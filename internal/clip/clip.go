// Package clip copies text to the system clipboard by shelling out to whatever
// clipboard tool is available, so it needs no CGO and no external Go dependency.
package clip

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Copy writes text to the system clipboard. It returns an error if no supported
// clipboard utility is found or the command fails.
func Copy(text string) error {
	name, args := command()
	if name == "" {
		return fmt.Errorf("no clipboard tool found (install wl-clipboard, xclip, or xsel)")
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// command picks the first available clipboard utility for the platform.
func command() (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		if has("pbcopy") {
			return "pbcopy", nil
		}
	case "windows":
		return "clip", nil
	}
	switch {
	case has("wl-copy"): // Wayland
		return "wl-copy", nil
	case has("xclip"): // X11
		return "xclip", []string{"-selection", "clipboard"}
	case has("xsel"): // X11
		return "xsel", []string{"--clipboard", "--input"}
	case has("clip.exe"): // WSL
		return "clip.exe", nil
	}
	return "", nil
}

func has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
