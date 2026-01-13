package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
)

var Definitions = []openai.ChatCompletionToolUnionParam{
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "read",
		Description: openai.String("Read file with line numbers (file path, not directory)"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]interface{}{
				"path":   map[string]interface{}{"type": "string"},
				"offset": map[string]interface{}{"type": "integer"},
				"limit":  map[string]interface{}{"type": "integer"},
			},
			"required": []string{"path"},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "write",
		Description: openai.String("Write content to file"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string"},
				"content": map[string]interface{}{"type": "string"},
			},
			"required": []string{"path", "content"},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "edit",
		Description: openai.String("Replace old with new in file (old must be unique unless all=true)"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string"},
				"old":  map[string]interface{}{"type": "string"},
				"new":  map[string]interface{}{"type": "string"},
				"all":  map[string]interface{}{"type": "boolean"},
			},
			"required": []string{"path", "old", "new"},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "glob",
		Description: openai.String("Find files by pattern, sorted by mtime"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]interface{}{
				"pat":  map[string]interface{}{"type": "string"},
				"path": map[string]interface{}{"type": "string"},
			},
			"required": []string{"pat"},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "grep",
		Description: openai.String("Search files for regex pattern"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]interface{}{
				"pat":  map[string]interface{}{"type": "string"},
				"path": map[string]interface{}{"type": "string"},
			},
			"required": []string{"pat"},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "bash",
		Description: openai.String("Run shell command"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]interface{}{
				"cmd": map[string]interface{}{"type": "string"},
			},
			"required": []string{"cmd"},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "ls",
		Description: openai.String("List files and directories in a path (defaults to current directory)"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string"},
			},
			"required": []string{},
		},
	}),
}

func ExecuteTool(name string, argsJSON string) (string, error) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", err
	}

	switch name {
	case "read":
		return ToolRead(args)
	case "write":
		return ToolWrite(args)
	case "edit":
		return ToolEdit(args)
	case "glob":
		return ToolGlob(args)
	case "grep":
		return ToolGrep(args)
	case "bash":
		return ToolBash(args)
	case "ls":
		return ToolLs(args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func GenerateToolSummary(name string, argsJSON string, result string) string {
	var args map[string]interface{}
	json.Unmarshal([]byte(argsJSON), &args)

	switch name {
	case "read":
		path, _ := args["path"].(string)
		lines := strings.Count(result, "\n")
		if lines == 0 && result != "" {
			lines = 1
		}
		return fmt.Sprintf("READ %s (%d lines)", filepath.Base(path), lines)
	case "write":
		path, _ := args["path"].(string)
		content, _ := args["content"].(string)
		lines := strings.Count(content, "\n") + 1
		return fmt.Sprintf("WRITE %s (%d lines)", filepath.Base(path), lines)
	case "edit":
		path, _ := args["path"].(string)
		if strings.Contains(result, "error") {
			return fmt.Sprintf("EDIT %s (failed)", filepath.Base(path))
		}
		return fmt.Sprintf("EDIT %s", filepath.Base(path))
	case "glob":
		pat, _ := args["pat"].(string)
		matches := strings.Count(result, "\n")
		if result == "none" {
			matches = 0
		} else if matches == 0 && result != "" {
			matches = 1
		}
		return fmt.Sprintf("GLOB %s (%d files)", pat, matches)
	case "grep":
		pat, _ := args["pat"].(string)
		matches := strings.Count(result, "\n")
		if result == "none" {
			matches = 0
		} else if matches == 0 && result != "" {
			matches = 1
		}
		return fmt.Sprintf("GREP \"%s\" (%d matches)", pat, matches)
	case "bash":
		cmd, _ := args["cmd"].(string)
		if len(cmd) > 30 {
			cmd = cmd[:27] + "..."
		}
		return fmt.Sprintf("BASH %s", cmd)
	case "ls":
		path, _ := args["path"].(string)
		if path == "" {
			path = "."
		}
		entries := strings.Count(result, "\n")
		if result == "(empty directory)" {
			entries = 0
		}
		return fmt.Sprintf("LS %s (%d entries)", path, entries)
	default:
		return fmt.Sprintf("%s called", strings.ToUpper(name))
	}
}

func ToolRead(args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)
	offset, _ := args["offset"].(float64)
	limit, _ := args["limit"].(float64)

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(data), "\n")
	start := int(offset)
	if start < 0 {
		start = 0
	}
	if start > len(lines) {
		start = len(lines)
	}

	end := len(lines)
	if limit > 0 {
		end = start + int(limit)
		if end > len(lines) {
			end = len(lines)
		}
	}

	selected := lines[start:end]
	var sb strings.Builder
	for i, line := range selected {
		sb.WriteString(fmt.Sprintf("%4d| %s\n", start+i+1, line))
	}
	return sb.String(), nil
}

func ToolWrite(args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)

	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		return "", err
	}
	return "ok", nil
}

func ToolEdit(args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)
	oldStr, _ := args["old"].(string)
	newStr, _ := args["new"].(string)
	all, _ := args["all"].(bool)

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	text := string(data)
	if !strings.Contains(text, oldStr) {
		return "error: old_string not found", nil
	}

	count := strings.Count(text, oldStr)
	if !all && count > 1 {
		return fmt.Sprintf("error: old_string appears %d times, must be unique (use all=true)", count), nil
	}

	var replacement string
	if all {
		replacement = strings.ReplaceAll(text, oldStr, newStr)
	} else {
		replacement = strings.Replace(text, oldStr, newStr, 1)
	}

	err = os.WriteFile(path, []byte(replacement), 0644)
	if err != nil {
		return "", err
	}
	return "ok", nil
}

func ToolGlob(args map[string]interface{}) (string, error) {
	pat, _ := args["pat"].(string)
	root, _ := args["path"].(string)
	if root == "" {
		root = "."
	}

	pattern := filepath.Join(root, pat)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}

	type fileInfo struct {
		path  string
		mtime time.Time
	}

	var infos []fileInfo
	for _, m := range matches {
		stat, err := os.Stat(m)
		if err == nil {
			infos = append(infos, fileInfo{path: m, mtime: stat.ModTime()})
		} else {
			infos = append(infos, fileInfo{path: m})
		}
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].mtime.After(infos[j].mtime)
	})

	var result []string
	for _, info := range infos {
		result = append(result, info.path)
	}

	if len(result) == 0 {
		return "none", nil
	}
	return strings.Join(result, "\n"), nil
}

func ToolGrep(args map[string]interface{}) (string, error) {
	pat, _ := args["pat"].(string)
	root, _ := args["path"].(string)
	if root == "" {
		root = "."
	}

	re, err := regexp.Compile(pat)
	if err != nil {
		return "", err
	}

	var hits []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if re.MatchString(line) {
				hits = append(hits, fmt.Sprintf("%s:%d:%s", path, i+1, strings.TrimSpace(line)))
				if len(hits) >= 50 {
					return io.EOF
				}
			}
		}
		return nil
	})

	if err != nil && err != io.EOF {
		return "", err
	}

	if len(hits) == 0 {
		return "none", nil
	}
	return strings.Join(hits, "\n"), nil
}

func ToolBash(args map[string]interface{}) (string, error) {
	cmdStr, _ := args["cmd"].(string)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))
	if result == "" {
		if err != nil {
			return fmt.Sprintf("error: %v", err), nil
		}
		return "(empty)", nil
	}
	return result, nil
}

func ToolLs(args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for _, entry := range entries {
		prefix := "[FILE]"
		name := entry.Name()
		if entry.IsDir() {
			prefix = "[DIR] "
			name += "/"
		}
		
		sb.WriteString(fmt.Sprintf("%s %s\n", prefix, name))
	}
	
	result := sb.String()
	if result == "" {
		return "(empty directory)", nil
	}
	return result, nil
}
