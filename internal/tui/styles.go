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

	// Panel editor row styles
	editorFocused lipgloss.Style // focused field row (bold, secondary color)
	editorNormal  lipgloss.Style // unfocused field row
}

// Fixed semantic colors (not user-configurable). White is the terminal's bright
// white so badge text stays readable on any accent background.
var (
	colWhite = lipgloss.Color("15")
	colGreen = lipgloss.Color("42")
	colRed   = lipgloss.Color("203")
	colYell  = lipgloss.Color("221")
	// Inactive/dim elements (unfocused panel borders, placeholders, hint
	// brackets). A bit brighter than the old 240 so inactive panels read clearly.
	colDim = lipgloss.Color("245")

	// Footer hint label color, brighter than dim for readability.
	colFooterFg = lipgloss.Color("252")
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

		editorFocused: lipgloss.NewStyle().Foreground(secondary).Bold(true),
		editorNormal:  lipgloss.NewStyle().Foreground(colFooterFg),
	}
}

// headerLenColor returns the color for a header of length n against the limit
// max: green when comfortably short (≤50), yellow once it grows past 50, red
// once it exceeds the configured maximum.
func headerLenColor(n, max int) color.Color {
	switch {
	case n > max:
		return colRed
	case n > 50:
		return colYell
	default:
		return colGreen
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

// key renders one "[t] type" keybinding chip as plain text (no background bar),
// to match the bordered, borderless-footer panel styling: dim brackets, an
// accent key, and a readable label.
func (s styles) key(k, label string) string {
	keySt := lipgloss.NewStyle().Foreground(s.secondary).Bold(true)
	brk := lipgloss.NewStyle().Foreground(colDim)
	lbl := lipgloss.NewStyle().Foreground(colFooterFg)
	return brk.Render("[") + keySt.Render(k) + brk.Render("] ") + lbl.Render(label)
}

// footerBar joins keybinding chips into one borderless line with a small indent.
func (s styles) footerBar(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, "  ")
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

// popup renders body inside a rounded, padded box with the given border color —
// the bordered surface for the edit/review/push/error popups, which are then
// composited over the panel layout by the caller.
func (s styles) popup(border color.Color, body string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(1, 2).
		Render(body)
}

// panelBox renders a titled panel with box-drawing border characters.
// Active panels use the primary accent color; inactive panels use the dim color.
// The title appears in the top border: ┌─ Title ──────────┐
func (s styles) panelBox(title, content string, outerW, outerH int, active bool) string {
	if outerW < 4 || outerH < 3 {
		return ""
	}

	var borderColor color.Color = colDim
	if active {
		borderColor = s.primary
	}
	borderSt := lipgloss.NewStyle().Foreground(borderColor)
	titleSt := lipgloss.NewStyle().Foreground(borderColor)
	if active {
		titleSt = titleSt.Bold(true)
	}

	innerW := outerW - 2 // subtract left + right border chars
	innerH := outerH - 2 // subtract top + bottom border rows

	// Clamp content to innerH lines, padding short content with empty lines.
	lines := strings.Split(content, "\n")
	for len(lines) < innerH {
		lines = append(lines, "")
	}
	lines = lines[:innerH]

	var out strings.Builder

	// Top border: ╭─ Title ──────────────╮
	titlePart := "─ " + title + " "
	titleRunes := []rune(titlePart)
	fill := innerW - len(titleRunes)
	if fill < 0 {
		fill = 0
		titleRunes = titleRunes[:innerW]
		titlePart = string(titleRunes)
	}
	out.WriteString(borderSt.Render("╭"))
	out.WriteString(titleSt.Render(titlePart))
	out.WriteString(borderSt.Render(strings.Repeat("─", fill) + "╮"))
	out.WriteString("\n")

	// Content rows
	lb := borderSt.Render("│")
	rb := borderSt.Render("│")
	for _, line := range lines {
		visW := lipgloss.Width(line)
		if visW > innerW {
			line = lipgloss.NewStyle().MaxWidth(innerW).Render(line)
			visW = innerW
		}
		out.WriteString(lb + line + strings.Repeat(" ", innerW-visW) + rb + "\n")
	}

	// Bottom border: ╰──────────────────────╯
	out.WriteString(borderSt.Render("╰" + strings.Repeat("─", innerW) + "╯"))

	return out.String()
}

// footerBarSplit renders a borderless footer line with a global section and a
// panel-specific section separated by a dim vertical bar. Either may be empty.
func (s styles) footerBarSplit(global, panel []string) string {
	divider := lipgloss.NewStyle().Foreground(colDim).Render("│")

	all := make([]string, 0, len(global)+1+len(panel))
	all = append(all, global...)
	if len(panel) > 0 {
		all = append(all, divider)
		all = append(all, panel...)
	}
	if len(all) == 0 {
		return ""
	}
	return " " + strings.Join(all, "  ")
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
