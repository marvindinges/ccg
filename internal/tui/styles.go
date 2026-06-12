package tui

import (
	"image/color"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

type styles struct {
	primary   color.Color   // configurable accent (badges, borders, spinner tail)
	secondary color.Color   // configurable accent (keys, selectors, spinner head)
	shimmer   []color.Color // loading-sweep colors, derived from the accents

	logo       lipgloss.Style // "ccg" badge
	stage      lipgloss.Style // current-step badge
	subtle     lipgloss.Style
	errBox     lipgloss.Style
	warnBox    lipgloss.Style
	success    lipgloss.Style
	spin       lipgloss.Style
	preview    lipgloss.Style
	previewT   lipgloss.Style
	modalTitle lipgloss.Style
}

// Fixed semantic colors (not user-configurable). White is the terminal's bright
// white so badge text stays readable on any accent background.
var (
	colWhite = lipgloss.Color("15")
	colGreen = lipgloss.Color("42")
	colRed   = lipgloss.Color("203")
	colYell  = lipgloss.Color("221")
	colDim   = lipgloss.Color("240")

	// Footer bar (keybinding hints). A solid background keeps the hints readable
	// on transparent terminals; brighter text than the dim default for contrast.
	colFooterBg  = lipgloss.Color("236")
	colFooterFg  = lipgloss.Color("252")
	colFooterDim = lipgloss.Color("245")
)

// ansiNames maps terminal color names to ANSI palette indices, so a config of
// "bright-blue" follows the user's terminal theme.
var ansiNames = map[string]string{
	"black": "0", "red": "1", "green": "2", "yellow": "3",
	"blue": "4", "magenta": "5", "purple": "5", "cyan": "6", "white": "7",
	"gray": "8", "grey": "8", "bright-black": "8",
	"bright-red": "9", "bright-green": "10", "bright-yellow": "11",
	"bright-blue": "12", "bright-magenta": "13", "bright-purple": "13",
	"bright-cyan": "14", "bright-white": "15",
}

// parseColor turns a config color spec into a color: a terminal color name
// ("bright-blue"), an ANSI 256 index ("141"), or a hex value ("#a06bff").
func parseColor(spec string) color.Color {
	s := strings.ToLower(strings.TrimSpace(spec))
	if idx, ok := ansiNames[s]; ok {
		return lipgloss.Color(idx)
	}
	return lipgloss.Color(spec)
}

func newStyles(primary, secondary color.Color) styles {
	badge := func(bg color.Color) lipgloss.Style {
		return lipgloss.NewStyle().Bold(true).Foreground(colWhite).Background(bg).Padding(0, 1)
	}
	return styles{
		primary:   primary,
		secondary: secondary,
		// Sweep: bright head → secondary → primary, then dim tail.
		shimmer: []color.Color{colWhite, secondary, primary},

		logo:   badge(primary),
		stage:  badge(secondary),
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
		spin:    lipgloss.NewStyle().Foreground(secondary),
		preview: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primary).
			Padding(0, 1),
		previewT:   lipgloss.NewStyle().Foreground(primary).Bold(true),
		modalTitle: lipgloss.NewStyle().Foreground(colRed).Bold(true),
	}
}

// brailleFrames drives the spinner glyph.
var brailleFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// loading renders an animated loader: a cycling braille spinner followed by the
// label with a bright highlight that sweeps across it (a shimmer/wave effect).
// frame is a monotonically increasing tick counter.
func (s styles) loading(frame int, label string) string {
	sp := lipgloss.NewStyle().Foreground(s.secondary).Bold(true).
		Render(string(brailleFrames[frame%len(brailleFrames)]))

	runes := []rune(label)
	// The bright head travels across the label and a trailing gap, then loops.
	span := len(runes) + len(s.shimmer) + 4
	head := frame % span

	dim := lipgloss.NewStyle().Foreground(colDim)
	var b strings.Builder
	for i, r := range runes {
		d := head - i // distance behind the moving head
		if d >= 0 && d < len(s.shimmer) {
			st := lipgloss.NewStyle().Foreground(s.shimmer[d])
			if d == 0 {
				st = st.Bold(true)
			}
			b.WriteString(st.Render(string(r)))
		} else {
			b.WriteString(dim.Render(string(r)))
		}
	}
	dots := strings.Repeat(".", frame%4)
	return sp + " " + b.String() + dim.Render(dots)
}

// key renders one "[t] type" keybinding chip with the footer background, so the
// whole footer reads as a solid bar even on a transparent terminal.
func (s styles) key(k, label string) string {
	keySt := lipgloss.NewStyle().Foreground(s.secondary).Bold(true).Background(colFooterBg)
	brk := lipgloss.NewStyle().Foreground(colFooterDim).Background(colFooterBg)
	lbl := lipgloss.NewStyle().Foreground(colFooterFg).Background(colFooterBg)
	return brk.Render("[") + keySt.Render(k) + brk.Render("] ") + lbl.Render(label)
}

// footerBar joins keybinding chips into a single background bar (with padding),
// so the separators and edges share the chips' background.
func (s styles) footerBar(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	sep := lipgloss.NewStyle().Background(colFooterBg).Render("  ")
	line := strings.Join(parts, sep)
	return lipgloss.NewStyle().Background(colFooterBg).Padding(0, 1).Render(line)
}

// hints renders a form's active keybindings as a footer bar in the same
// "[KEY] text" style as the review hub. Bindings without help text or that are
// disabled are skipped; duplicates are de-duplicated.
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
	return s.footerBar(parts)
}

// modal renders body inside a bordered panel centered over a width×height area —
// a simple overlay used for AI errors so long messages wrap and stay readable.
func (s styles) modal(width, height int, body string) string {
	boxWidth := 64
	if width > 0 && width-8 < boxWidth {
		boxWidth = width - 8
	}
	if boxWidth < 24 {
		boxWidth = 24
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colRed).
		Padding(1, 2).
		Width(boxWidth).
		Render(body)
	if width <= 0 || height <= 0 {
		return box
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
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

// huhTheme is huh's Charm theme recolored so the focused field's accents use
// the configured primary/secondary colors, matching the rest of the TUI.
func (s styles) huhTheme() huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		st := huh.ThemeCharm(isDark)
		f := &st.Focused
		f.Base = f.Base.BorderForeground(s.primary)
		f.Title = f.Title.Foreground(s.primary).Bold(true)
		f.NoteTitle = f.NoteTitle.Foreground(s.primary)
		f.SelectSelector = f.SelectSelector.Foreground(s.secondary)
		f.MultiSelectSelector = f.MultiSelectSelector.Foreground(s.secondary)
		f.SelectedOption = f.SelectedOption.Foreground(s.secondary)
		f.SelectedPrefix = f.SelectedPrefix.Foreground(s.secondary)
		f.NextIndicator = f.NextIndicator.Foreground(s.secondary)
		f.PrevIndicator = f.PrevIndicator.Foreground(s.secondary)
		f.FocusedButton = f.FocusedButton.Background(s.primary).Foreground(colWhite)
		return st
	})
}
