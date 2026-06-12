package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

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

// brailleFrames drives the spinner glyph.
var brailleFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// shimmerColors are the bright-to-trailing colors swept across the label.
var shimmerColors = []color.Color{
	lipgloss.Color("231"), // brightest (head)
	lipgloss.Color("225"),
	lipgloss.Color("213"),
	lipgloss.Color("212"),
	lipgloss.Color("141"),
	lipgloss.Color("98"),
}

// loading renders an animated loader: a cycling braille spinner followed by the
// label with a bright highlight that sweeps across it (a shimmer/wave effect).
// frame is a monotonically increasing tick counter.
func (s styles) loading(frame int, label string) string {
	sp := lipgloss.NewStyle().Foreground(colPink).Bold(true).
		Render(string(brailleFrames[frame%len(brailleFrames)]))

	runes := []rune(label)
	// The bright head travels across the label and a trailing gap, then loops.
	span := len(runes) + len(shimmerColors) + 4
	head := frame % span

	dim := lipgloss.NewStyle().Foreground(colDim)
	var b strings.Builder
	for i, r := range runes {
		d := head - i // distance behind the moving head
		if d >= 0 && d < len(shimmerColors) {
			st := lipgloss.NewStyle().Foreground(shimmerColors[d])
			if d == 0 {
				st = st.Bold(true)
			}
			b.WriteString(st.Render(string(r)))
		} else {
			b.WriteString(dim.Render(string(r)))
		}
	}
	// Animated trailing dots.
	dots := strings.Repeat(".", frame%4)
	return sp + " " + b.String() + dim.Render(dots)
}

// key renders a keybinding hint like "[t] type" with the key emphasized.
func (s styles) key(k, label string) string {
	cap := lipgloss.NewStyle().Foreground(colPink).Bold(true).Render(k)
	return s.subtle.Render("[") + cap + s.subtle.Render("] ") + s.subtle.Render(label)
}

// header renders the branded title line plus a contextual step label.
func (s styles) header(stepLabel string) string {
	line := s.logo.Render("ccg") + "  " + s.tagline.Render("conventional commits")
	if stepLabel != "" {
		line += "  " + s.subtle.Render("·") + "  " + s.step.Render(stepLabel)
	}
	return line
}
