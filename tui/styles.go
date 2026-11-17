package main

import "github.com/charmbracelet/lipgloss"

var severities = []string{"critica", "alta", "media", "baixa", "info"}

var sevColor = map[string]lipgloss.AdaptiveColor{
	"critica": {Light: "160", Dark: "203"},
	"alta":    {Light: "166", Dark: "215"},
	"media":   {Light: "136", Dark: "221"},
	"baixa":   {Light: "25", Dark: "75"},
	"info":    {Light: "243", Dark: "246"},
}

var (
	accent   = lipgloss.AdaptiveColor{Light: "57", Dark: "141"}
	subtle   = lipgloss.AdaptiveColor{Light: "245", Dark: "241"}
	okColor  = lipgloss.AdaptiveColor{Light: "28", Dark: "78"}
	errColor = lipgloss.AdaptiveColor{Light: "160", Dark: "203"}

	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(accent)
	subtleStyle   = lipgloss.NewStyle().Foreground(subtle)
	boldStyle     = lipgloss.NewStyle().Bold(true)
	okStyle       = lipgloss.NewStyle().Foreground(okColor)
	errStyle      = lipgloss.NewStyle().Foreground(errColor)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	helpStyle     = lipgloss.NewStyle().Foreground(subtle)
	panelStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(subtle).Padding(0, 1)
	labelStyle    = lipgloss.NewStyle().Bold(true).Foreground(accent)
)

func sevStyle(s string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(sevColor[s])
}

func sevDot(s string) string { return sevStyle(s).Render("●") }

func sevIndex(s string) int {
	for i, v := range severities {
		if v == s {
			return i
		}
	}
	return len(severities)
}

// Contagem por severidade, ex.: "● 1 critica  ● 2 alta".
func sevSummary(fs []Finding) string {
	count := map[string]int{}
	for _, f := range fs {
		count[f.Severidade]++
	}
	out := ""
	for _, s := range severities {
		if count[s] > 0 {
			if out != "" {
				out += "  "
			}
			out += sevStyle(s).Render("● " + itoa(count[s]) + " " + s)
		}
	}
	return out
}

func sevCompact(fs []Finding) string {
	count := map[string]int{}
	for _, f := range fs {
		count[f.Severidade]++
	}
	out := ""
	for _, s := range severities {
		if count[s] > 0 {
			out += sevStyle(s).Render("●"+itoa(count[s])) + " "
		}
	}
	return out
}
