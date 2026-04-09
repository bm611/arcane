package styles

import "github.com/charmbracelet/lipgloss"

var (
	ContentWidth = 54
)

// Noir Rose palette
const (
	Rose   = "#F43F5E"
	Violet = "#8B5CF6"
	Cyan   = "#06B6D4"
	Amber  = "#F59E0B"
	Pink   = "#EC4899"

	TextPrimary   = "#F1F5F9"
	TextSecondary = "#94A3B8"
	TextMuted     = "#64748B"
	TextDim       = "#475569"

	BorderDark = "#1E293B"
	BgDeep     = "#0F172A"

	ErrRed   = "#EF4444"
	ErrAmber = "#F59E0B"
	Success  = "#10B981"
)

var (
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(Rose)).
			Italic(true).
			Padding(0, 1)

	InfoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(TextMuted)).
			Render

	UserLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#0E1525")).
			Background(lipgloss.Color(Cyan)).
			Bold(true).
			Padding(0, 1).
			MarginRight(1)

	UserMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#1E293B", Dark: TextPrimary}).
			PaddingLeft(2).
			BorderLeft(true).
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(lipgloss.Color(Cyan))

	AiLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F1F5F9")).
			Background(lipgloss.Color(Violet)).
			Bold(true).
			Padding(0, 1).
			MarginRight(1)

	AiMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#1E293B", Dark: TextPrimary}).
			PaddingTop(1).
			BorderLeft(true).
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(lipgloss.Color(Violet))

	ErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ErrRed)).
			Bold(true)

	ToolActionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(TextDim)).
			PaddingLeft(2)

	ToolIconStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Amber)).
			Bold(true)

	ToolNameStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FCD34D")).
			Bold(true)

	ToolDetailStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(TextMuted))

	InputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(Rose)).
			Padding(0, 1)

	WelcomeArtStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#1E293B", Dark: Rose}).
			Bold(true)

	WelcomeSubtitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(Violet)).
				Italic(true)

	ModalStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(Rose)).
			Padding(1, 2)

	ModalTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(Rose)).
			Width(ContentWidth).
			MarginBottom(1)

	ModalItemStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Width(ContentWidth)

	ModalHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				PaddingLeft(1).
				Width(ContentWidth)

	ModalSelectedStyle = lipgloss.NewStyle().
				Padding(0, 1).
				Width(ContentWidth).
				Background(lipgloss.Color("#312E81")).
				Foreground(lipgloss.Color(TextPrimary))

	ModelNameStyle = lipgloss.NewStyle().
			Bold(true).
			MarginRight(1).
			Foreground(lipgloss.AdaptiveColor{Light: "#1E293B", Dark: TextPrimary})

	ProviderStyle = lipgloss.NewStyle().
			Italic(true).
			MarginRight(1)

	DescStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(TextMuted)).
			Width(50)

	HintColor = lipgloss.Color(TextMuted)

	InputTokenStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(Cyan))
	OutputTokenStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(Violet))
)

var ProviderColors = map[string]string{
	"Gemini":     "#A78BFA",
	"Xai":        Pink,
	"Deepseek":   Cyan,
	"MiniMax":    "#60A5FA",
	"Perplexity": Amber,
	"Z.ai":       Success,
	"OpenAI":     Success,
}
