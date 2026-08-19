package tui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

type terminalTheme struct {
	accent   lipgloss.Style
	healthy  lipgloss.Style
	warning  lipgloss.Style
	danger   lipgloss.Style
	muted    lipgloss.Style
	selected lipgloss.Style
	border   lipgloss.Color
	focus    lipgloss.Color
}

func currentTheme() terminalTheme {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		return terminalTheme{}
	}
	return terminalTheme{
		accent:   lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true),
		healthy:  lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
		warning:  lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
		danger:   lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true),
		muted:    lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		selected: lipgloss.NewStyle().Background(lipgloss.Color("24")).Foreground(lipgloss.Color("15")),
		border:   lipgloss.Color("8"),
		focus:    lipgloss.Color("14"),
	}
}

func styleRisk(value string) string {
	theme := currentTheme()
	switch value {
	case "CRIT", "HIGH", "STALE":
		return theme.danger.Render(value)
	case "MED":
		return theme.warning.Render(value)
	case "INFO":
		return theme.healthy.Render(value)
	default:
		return value
	}
}

func styleSelected(value string, selected bool) string {
	if !selected {
		return value
	}
	return currentTheme().selected.Render(value)
}
