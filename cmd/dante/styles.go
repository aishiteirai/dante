package main

import "github.com/charmbracelet/lipgloss"

var (
	accent   = lipgloss.AdaptiveColor{Light: "57", Dark: "141"}
	subtle   = lipgloss.AdaptiveColor{Light: "245", Dark: "241"}
	faint    = lipgloss.AdaptiveColor{Light: "250", Dark: "238"}
	okColor  = lipgloss.AdaptiveColor{Light: "28", Dark: "78"}
	errColor = lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	warnCol  = lipgloss.AdaptiveColor{Light: "136", Dark: "221"}
	shaColor = lipgloss.AdaptiveColor{Light: "130", Dark: "179"}
	refColor = lipgloss.AdaptiveColor{Light: "30", Dark: "80"}
	hunkCol  = lipgloss.AdaptiveColor{Light: "31", Dark: "117"}

	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(accent)
	subtleStyle   = lipgloss.NewStyle().Foreground(subtle)
	boldStyle     = lipgloss.NewStyle().Bold(true)
	okStyle       = lipgloss.NewStyle().Foreground(okColor)
	errStyle      = lipgloss.NewStyle().Foreground(errColor)
	warnStyle     = lipgloss.NewStyle().Foreground(warnCol)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	helpStyle     = lipgloss.NewStyle().Foreground(subtle)
	labelStyle    = lipgloss.NewStyle().Bold(true).Foreground(accent)
	shaStyle      = lipgloss.NewStyle().Foreground(shaColor)
	refStyle      = lipgloss.NewStyle().Foreground(refColor).Bold(true)
	addStyle      = lipgloss.NewStyle().Foreground(okColor)
	delStyle      = lipgloss.NewStyle().Foreground(errColor)
	hunkStyle     = lipgloss.NewStyle().Foreground(hunkCol)
	cursorBg      = lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "254", Dark: "236"})

	paneStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(faint).Padding(0, 1)
	paneFocus = paneStyle.BorderForeground(accent)
	boxStyle  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(subtle).Padding(1, 2)
)

var statusStyle = map[string]lipgloss.Style{
	"M": warnStyle, "A": addStyle, "?": addStyle, "D": delStyle, "R": lipgloss.NewStyle().Foreground(refColor),
	"C": lipgloss.NewStyle().Foreground(refColor), "T": warnStyle, "U": errStyle,
}
