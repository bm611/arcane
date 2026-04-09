package styles

import "github.com/charmbracelet/lipgloss"

// Theme defines a complete color scheme for the application
type Theme struct {
	// Core colors
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Accent    lipgloss.Color

	// Background colors
	BgBase     lipgloss.Color
	BgSurface  lipgloss.Color
	BgElevated lipgloss.Color

	// Text colors
	TextPrimary_   lipgloss.Color
	TextSecondary_ lipgloss.Color
	TextMuted_     lipgloss.Color

	// Semantic colors
	Success_ lipgloss.Color
	Warning  lipgloss.Color
	Error_   lipgloss.Color
	Info     lipgloss.Color

	// UI element colors
	Border  lipgloss.Color
	Divider lipgloss.Color

	// Mode-specific
	ModeChat  lipgloss.Color
	ModeAgent lipgloss.Color
}

// DarkTheme — Noir Rose palette
var DarkTheme = Theme{
	Primary:   lipgloss.Color(Rose),
	Secondary: lipgloss.Color(Cyan),
	Accent:    lipgloss.Color(Violet),

	BgBase:     lipgloss.Color("#080C14"),
	BgSurface:  lipgloss.Color("#0D1117"),
	BgElevated: lipgloss.Color(BgDeep),

	TextPrimary_:   lipgloss.Color(TextPrimary),
	TextSecondary_: lipgloss.Color(TextSecondary),
	TextMuted_:     lipgloss.Color(TextMuted),

	Success_: lipgloss.Color(Success),
	Warning:  lipgloss.Color(Amber),
	Error_:   lipgloss.Color(ErrRed),
	Info:     lipgloss.Color(Cyan),

	Border:  lipgloss.Color(BorderDark),
	Divider: lipgloss.Color("#111827"),

	ModeChat:  lipgloss.Color(Rose),
	ModeAgent: lipgloss.Color(Pink),
}

// LightTheme is the light mode color scheme
var LightTheme = Theme{
	Primary:   lipgloss.Color("#E11D48"),
	Secondary: lipgloss.Color("#0891B2"),
	Accent:    lipgloss.Color("#7C3AED"),

	BgBase:     lipgloss.Color("#FAFAFA"),
	BgSurface:  lipgloss.Color("#FFFFFF"),
	BgElevated: lipgloss.Color("#F4F4F5"),

	TextPrimary_:   lipgloss.Color("#0F172A"),
	TextSecondary_: lipgloss.Color("#475569"),
	TextMuted_:     lipgloss.Color("#94A3B8"),

	Success_: lipgloss.Color("#10B981"),
	Warning:  lipgloss.Color("#F59E0B"),
	Error_:   lipgloss.Color("#EF4444"),
	Info:     lipgloss.Color("#0891B2"),

	Border:  lipgloss.Color("#E2E8F0"),
	Divider: lipgloss.Color("#F1F5F9"),

	ModeChat:  lipgloss.Color("#E11D48"),
	ModeAgent: lipgloss.Color("#7C3AED"),
}

// CurrentTheme holds the active theme (set at runtime based on terminal)
var CurrentTheme = DarkTheme

// Adaptive returns an AdaptiveColor that switches between light/dark
type Adaptive = lipgloss.AdaptiveColor

// Common adaptive colors for quick use
var (
	FgPrimary   = Adaptive{Light: string(LightTheme.Primary), Dark: string(DarkTheme.Primary)}
	FgSecondary = Adaptive{Light: string(LightTheme.Secondary), Dark: string(DarkTheme.Secondary)}
	FgMuted     = Adaptive{Light: string(LightTheme.TextMuted_), Dark: string(DarkTheme.TextMuted_)}
	FgError     = Adaptive{Light: string(LightTheme.Error_), Dark: string(DarkTheme.Error_)}
	FgSuccess   = Adaptive{Light: string(LightTheme.Success_), Dark: string(DarkTheme.Success_)}
	FgWarning   = Adaptive{Light: string(LightTheme.Warning), Dark: string(DarkTheme.Warning)}
	AccentColor = Adaptive{Light: string(LightTheme.Accent), Dark: string(DarkTheme.Accent)}

	BgSurfaceColor = Adaptive{Light: string(LightTheme.BgSurface), Dark: string(DarkTheme.BgSurface)}
	BgElevatedColor = Adaptive{Light: string(LightTheme.BgElevated), Dark: string(DarkTheme.BgElevated)}
	BorderColor     = Adaptive{Light: string(LightTheme.Border), Dark: string(DarkTheme.Border)}
)

// ProviderColorMap returns the color for each AI provider
var ProviderColorMap = map[string]lipgloss.Color{
	"Gemini":     lipgloss.Color("#A78BFA"),
	"Xai":        lipgloss.Color(Pink),
	"Deepseek":   lipgloss.Color(Cyan),
	"MiniMax":    lipgloss.Color("#60A5FA"),
	"Perplexity": lipgloss.Color(Amber),
	"Z.ai":       lipgloss.Color(Success),
	"OpenAI":     lipgloss.Color(Success),
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
