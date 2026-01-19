package main

import (
	"arcane/internal/ui"
	"fmt"
	"os"
)

func main() {
	p := ui.NewProgram()
	finalModel, err := p.Run()
	if err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
	if m, ok := finalModel.(*ui.Model); ok {
		if m.DB != nil {
			_ = m.DB.Close()
		}
	}
}
