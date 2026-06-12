package tui

import (
	"image/color"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

type styles struct {
	logo     lipgloss.Style // "ccg" badge
	stage    lipgloss.Style // current-step badge
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
	colDim   = lipgloss.Color("240")
)

func newStyles() styles {
	return styles{
		logo: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("231")).
			Background(colMauve).
			Padding(0, 1),
		stage: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("231")).
			Background(colPink).
			Padding(0, 1),
		subtle: lipgloss.NewStyle().Foreground(colDim),
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

// hints renders a form's active keybindings in the same "[KEY] text" style as
// the review hub, so help looks identical across every stage. Bindings without
// help text or that are disabled are skipped; duplicates are de-duplicated.
func (s styles) hints(bindings []key.Binding) string {
	seen := map[string]bool{}
	var parts []string
	for _, b := range bindings {
		if !b.Enabled() {
			continue
		}
		h := b.Help()
		if h.Key == "" || seen[h.Key] {
			continue
		}
		seen[h.Key] = true
		parts = append(parts, s.key(h.Key, h.Desc))
	}
	return strings.Join(parts, "  ")
}

// header renders two badges: the "ccg" mark and the current step, styled
// alike (bold light text on a colored background) but in different colors.
func (s styles) header(stepLabel string) string {
	out := s.logo.Render("ccg")
	if stepLabel != "" {
		out += " " + s.stage.Render(stepLabel)
	}
	return out
}

// ccgTheme is huh's Charm theme with the form's keybinding help recolored to
// match the review hub's hints (pink keys, dim labels).
func ccgTheme(isDark bool) *huh.Styles {
	s := huh.ThemeCharm(isDark)
	keyStyle := lipgloss.NewStyle().Foreground(colPink).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(colDim)
	s.Help.ShortKey = s.Help.ShortKey.Foreground(colPink).Bold(true)
	s.Help.ShortDesc = s.Help.ShortDesc.Foreground(colDim)
	s.Help.ShortSeparator = s.Help.ShortSeparator.Foreground(colDim)
	s.Help.FullKey = keyStyle
	s.Help.FullDesc = descStyle
	s.Help.FullSeparator = descStyle
	s.Help.Ellipsis = descStyle
	return s
}
