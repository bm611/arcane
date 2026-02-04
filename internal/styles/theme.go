package styles

import "github.com/charmbracelet/lipgloss"

// Theme defines a complete color scheme for the application
type Theme struct {
	// Core colors
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Accent    lipgloss.Color

	// Background colors
	BgBase    lipgloss.Color
	BgSurface lipgloss.Color
	BgElevated lipgloss.Color

	// Text colors
	TextPrimary   lipgloss.Color
	TextSecondary lipgloss.Color
	TextMuted     lipgloss.Color

	// Semantic colors
	Success lipgloss.Color
	Warning lipgloss.Color
	Error   lipgloss.Color
	Info    lipgloss.Color

	// UI element colors
	Border lipgloss.Color
	Divider lipgloss.Color

	// Mode-specific
	ModeChat  lipgloss.Color
	ModeAgent lipgloss.Color
}

// DarkTheme is the dark mode color scheme
var DarkTheme = Theme{
	Primary:      lipgloss.Color("#818CF8"),    // Indigo 400
	Secondary:    lipgloss.Color("#22D3EE"),    // Cyan 400
	Accent:       lipgloss.Color("#F472B6"),    // Pink 400

	BgBase:       lipgloss.Color("#0B0B0F"),    // Deep black
	BgSurface:    lipgloss.Color("#141419"),    // Slightly lighter
	BgElevated:   lipgloss.Color("#1E1E2A"),    // Elevated surfaces

	TextPrimary:   lipgloss.Color("#F1F5F9"),   // Slate 100
	TextSecondary: lipgloss.Color("#94A3B8"),   // Slate 400
	TextMuted:     lipgloss.Color("#64748B"),   // Slate 500

	Success: lipgloss.Color("#34D399"), // Emerald 400
	Warning: lipgloss.Color("#FBBF24"), // Amber 400
	Error:   lipgloss.Color("#FB7185"), // Rose 400
	Info:    lipgloss.Color("#60A5FA"), // Blue 400

	Border:  lipgloss.Color("#27272A"),  // Zinc 800
	Divider: lipgloss.Color("#1F2937"),  // Gray 800

	ModeChat:  lipgloss.Color("#818CF8"), // Indigo
	ModeAgent: lipgloss.Color("#A78BFA"), // Purple
}

// LightTheme is the light mode color scheme
var LightTheme = Theme{
	Primary:      lipgloss.Color("#4F46E5"),    // Indigo 600
	Secondary:    lipgloss.Color("#0891B2"),    // Cyan 600
	Accent:       lipgloss.Color("#DB2777"),    // Pink 600

	BgBase:       lipgloss.Color("#FAFAFA"),    // Near white
	BgSurface:    lipgloss.Color("#FFFFFF"),    // White
	BgElevated:   lipgloss.Color("#F4F4F5"),    // Zinc 100

	TextPrimary:   lipgloss.Color("#18181B"),   // Zinc 900
	TextSecondary: lipgloss.Color("#52525B"),   // Zinc 600
	TextMuted:     lipgloss.Color("#A1A1AA"),   // Zinc 400

	Success: lipgloss.Color("#10B981"), // Emerald 500
	Warning: lipgloss.Color("#F59E0B"), // Amber 500
	Error:   lipgloss.Color("#EF4444"), // Red 500
	Info:    lipgloss.Color("#3B82F6"), // Blue 500

	Border:  lipgloss.Color("#E4E4E7"),  // Zinc 200
	Divider: lipgloss.Color("#F4F4F5"),  // Zinc 100

	ModeChat:  lipgloss.Color("#4F46E5"), // Indigo
	ModeAgent: lipgloss.Color("#7C3AED"), // Purple
}

// CurrentTheme holds the active theme (set at runtime based on terminal)
var CurrentTheme = DarkTheme

// Adaptive returns an AdaptiveColor that switches between light/dark
type Adaptive = lipgloss.AdaptiveColor

// Common adaptive colors for quick use
var (
	FgPrimary   = Adaptive{Light: string(LightTheme.Primary), Dark: string(DarkTheme.Primary)}
	FgSecondary = Adaptive{Light: string(LightTheme.Secondary), Dark: string(DarkTheme.Secondary)}
	FgMuted     = Adaptive{Light: string(LightTheme.TextMuted), Dark: string(DarkTheme.TextMuted)}
	FgError     = Adaptive{Light: string(LightTheme.Error), Dark: string(DarkTheme.Error)}
	FgSuccess   = Adaptive{Light: string(LightTheme.Success), Dark: string(DarkTheme.Success)}
	FgWarning   = Adaptive{Light: string(LightTheme.Warning), Dark: string(DarkTheme.Warning)}
	Accent      = Adaptive{Light: string(LightTheme.Accent), Dark: string(DarkTheme.Accent)}

	BgSurface   = Adaptive{Light: string(LightTheme.BgSurface), Dark: string(DarkTheme.BgSurface)}
	BgElevated  = Adaptive{Light: string(LightTheme.BgElevated), Dark: string(DarkTheme.BgElevated)}
	BorderColor = Adaptive{Light: string(LightTheme.Border), Dark: string(DarkTheme.Border)}
)

// ProviderColorMap returns the color for each AI provider
var ProviderColorMap = map[string]lipgloss.Color{
	"Gemini":     lipgloss.Color("#A78BFA"), // Purple
	"Xai":        lipgloss.Color("#F472B6"), // Pink
	"Deepseek":   lipgloss.Color("#22D3EE"), // Cyan
	"MiniMax":    lipgloss.Color("#60A5FA"), // Blue
	"Perplexity": lipgloss.Color("#FBBF24"), // Amber
	"Z.ai":       lipgloss.Color("#34D399"), // Emerald
	"OpenAI":     lipgloss.Color("#10B981"), // Green
}

// GetProviderColor returns the color for a provider
func GetProviderColor(provider string) lipgloss.Color {
	if c, ok := ProviderColorMap[provider]; ok {
		return c
	}
	return CurrentTheme.Primary
}

// InitTheme sets the current theme based on terminal background
func InitTheme() {
	if lipgloss.HasDarkBackground() {
		CurrentTheme = DarkTheme
	} else {
		CurrentTheme = LightTheme
	}
}
