package styles

import "github.com/charmbracelet/lipgloss"

var (
	ContentWidth = 54
)

var (
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#B39DDB")).
			Padding(0, 1)

	InfoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#545454")).
			Render

	UserLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#90CAF9")).
			Bold(true).
			Padding(0, 1).
			MarginRight(1)

	UserMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#E0E0E0"}).
			PaddingLeft(2).
			BorderLeft(true).
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(lipgloss.Color("#90CAF9"))

	AiLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#B39DDB")).
			Bold(true).
			Padding(0, 1).
			MarginRight(1)

	AiMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#E0E0E0"}).
			PaddingTop(1).
			BorderLeft(true).
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(lipgloss.Color("#B39DDB"))

	ErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EF9A9A")).
			Bold(true)

	ToolActionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			PaddingLeft(2)

	ToolIconStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#CE93D8")).
			Bold(true)

	ToolNameStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFCC80")).
			Bold(true)

	ToolDetailStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#545454"))

	InputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#B39DDB")).
			Padding(0, 1)

	WelcomeArtStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#000000", Dark: "#FFFFFF"}).
			Bold(true)

	WelcomeSubtitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#545454")).
				Italic(true)

	ModalStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#B39DDB")).
			Padding(1, 2)

	ModalTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#B39DDB")).
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
				Background(lipgloss.Color("#5C5C7A")).
				Foreground(lipgloss.Color("#FFFFFF"))

	ModelNameStyle = lipgloss.NewStyle().
			Bold(true).
			MarginRight(1).
			Foreground(lipgloss.AdaptiveColor{Light: "#1a1a2e", Dark: "#FFFFFF"})

	ProviderStyle = lipgloss.NewStyle().
			Italic(true).
			MarginRight(1)

	DescStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Width(50)

	HintColor = lipgloss.Color("#545454")

	InputTokenStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#90CAF9"))
	OutputTokenStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#B39DDB"))
)

var ProviderColors = map[string]string{
	"Gemini":     "#CE93D8",
	"Xai":        "#FFCC80",
	"Deepseek":   "#80CBC4",
	"MiniMax":    "#81D4FA",
	"Perplexity": "#EF9A9A",
	"Z.ai":       "#A5D6A7",
	"OpenAI":     "#A5D6A7",
}
