package config

import (
	"os"
	"path/filepath"
)

// GlobalConfigPath returns the path to the global config file:
// $XDG_CONFIG_HOME/ccg/config.yaml, falling back to ~/.config/ccg/config.yaml.
// os.UserConfigDir already implements this resolution and is correct on WSL.
func GlobalConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ccg", "config.yaml"), nil
}

// GlobalConfigDir returns the directory containing the global config file.
func GlobalConfigDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ccg"), nil
}
