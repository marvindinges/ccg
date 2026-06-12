package tui

import "charm.land/lipgloss/v2"

type styles struct {
	logo     lipgloss.Style
	tagline  lipgloss.Style
	step     lipgloss.Style
	subtle   lipgloss.Style
	errBox   lipgloss.Style
	warnBox  lipgloss.Style
	success  lipgloss.Style
	spin     lipgloss.Style
	preview  lipgloss.Style
	previewT lipgloss.Style
}

// Palette (ANSI 256 — works on the typical WSL/Windows terminal).
var (
	colMauve = lipgloss.Color("141") // soft purple
	colPink  = lipgloss.Color("212")
	colGreen = lipgloss.Color("42")
	colRed   = lipgloss.Color("203")
	colYell  = lipgloss.Color("221")
	colGray  = lipgloss.Color("245")
	colDim   = lipgloss.Color("240")
)

func newStyles() styles {
	return styles{
		logo: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("231")).
			Background(colMauve).
			Padding(0, 1),
		tagline: lipgloss.NewStyle().Foreground(colGray).Italic(true),
		step:    lipgloss.NewStyle().Bold(true).Foreground(colPink),
		subtle:  lipgloss.NewStyle().Foreground(colDim),
		errBox: lipgloss.NewStyle().
			Foreground(colRed).Bold(true).
			Border(lipgloss.RoundedBorder(), false, false, false, true).
			BorderForeground(colRed).PaddingLeft(1),
		warnBox: lipgloss.NewStyle().
			Foreground(colYell).
			Border(lipgloss.RoundedBorder(), false, false, false, true).
			BorderForeground(colYell).PaddingLeft(1),
		success: lipgloss.NewStyle().Foreground(colGreen).Bold(true),
		spin:    lipgloss.NewStyle().Foreground(colMauve),
		preview: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colMauve).
			Padding(0, 1),
		previewT: lipgloss.NewStyle().Foreground(colMauve).Bold(true),
	}
}

// header renders the branded title line plus a contextual step label.
func (s styles) header(stepLabel string) string {
	line := s.logo.Render("ccg") + "  " + s.tagline.Render("conventional commits")
	if stepLabel != "" {
		line += "  " + s.subtle.Render("·") + "  " + s.step.Render(stepLabel)
	}
	return line
}
