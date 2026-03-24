package theme

import (
	"github.com/arturgomes/tnt/internal/config"
	"github.com/charmbracelet/lipgloss"
)

type Theme struct {
	BG     lipgloss.Color
	FG     lipgloss.Color
	Gray   lipgloss.Color
	Blue   lipgloss.Color
	Cyan   lipgloss.Color
	Green  lipgloss.Color
	Orange lipgloss.Color
	Purple lipgloss.Color
	Red    lipgloss.Color
	Yellow lipgloss.Color
	Dark   lipgloss.Color
	Border lipgloss.Color

	Title       lipgloss.Style
	Subtitle    lipgloss.Style
	Muted       lipgloss.Style
	Success     lipgloss.Style
	Warning     lipgloss.Style
	Error       lipgloss.Style
	Info        lipgloss.Style
	Accent      lipgloss.Style
	Selected    lipgloss.Style
	BorderStyle lipgloss.Style
	Panel       lipgloss.Style
}

func New(cfg config.ThemeConfig) *Theme {
	t := &Theme{
		BG:     lipgloss.Color(cfg.BG),
		FG:     lipgloss.Color(cfg.FG),
		Gray:   lipgloss.Color(cfg.Gray),
		Blue:   lipgloss.Color(cfg.Blue),
		Cyan:   lipgloss.Color(cfg.Cyan),
		Green:  lipgloss.Color(cfg.Green),
		Orange: lipgloss.Color(cfg.Orange),
		Purple: lipgloss.Color(cfg.Purple),
		Red:    lipgloss.Color(cfg.Red),
		Yellow: lipgloss.Color(cfg.Yellow),
		Dark:   lipgloss.Color(cfg.Dark),
		Border: lipgloss.Color(cfg.Border),
	}

	t.Title = lipgloss.NewStyle().Bold(true).Foreground(t.Blue)
	t.Subtitle = lipgloss.NewStyle().Foreground(t.Purple)
	t.Muted = lipgloss.NewStyle().Foreground(t.Gray)
	t.Success = lipgloss.NewStyle().Foreground(t.Green)
	t.Warning = lipgloss.NewStyle().Foreground(t.Yellow)
	t.Error = lipgloss.NewStyle().Foreground(t.Red)
	t.Info = lipgloss.NewStyle().Foreground(t.Cyan)
	t.Accent = lipgloss.NewStyle().Foreground(t.Orange)
	t.Selected = lipgloss.NewStyle().Foreground(t.Blue).Bold(true)
	t.BorderStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Border)
	t.Panel = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Gray).
		Padding(0, 1)

	return t
}
