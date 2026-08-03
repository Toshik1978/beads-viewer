// Package config resolves user preferences from, in decreasing precedence,
// command-line flags, the environment, ~/.config/bv/config.yaml, and built-in
// defaults.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/goccy/go-yaml"
)

// ThemePreference selects the colour scheme, or defers to terminal detection.
type ThemePreference string

// The supported theme preferences.
const (
	ThemeAuto  ThemePreference = "auto"
	ThemeLight ThemePreference = "light"
	ThemeDark  ThemePreference = "dark"
)

// ViewKind names the view bv opens in.
type ViewKind string

// The supported views.
const (
	ViewList  ViewKind = "list"
	ViewTree  ViewKind = "tree"
	ViewBoard ViewKind = "board"
)

// Config is the resolved configuration.
type Config struct {
	Theme      ThemePreference
	View       ViewKind
	HideClosed bool
	// DBPath is an explicit .beads location; empty means discover it.
	DBPath string
	// LogPath enables file logging; empty discards logs, because writing to
	// stderr would corrupt the TUI.
	LogPath string
}

// Flags carries the parsed command line.
type Flags struct {
	Theme      string
	View       string
	DBPath     string
	HideClosed bool
	// HideClosedSet distinguishes --hide-closed=false from the flag being
	// absent. Without it, an unset boolean flag would silently override a
	// config file that enabled the setting.
	HideClosedSet bool
}

// fileConfig is the on-disk shape. Pointers distinguish "absent" from "set to
// the zero value", which is what makes precedence work.
type fileConfig struct {
	Theme      *string `yaml:"theme"`
	View       *string `yaml:"view"`
	HideClosed *bool   `yaml:"hide_closed"`
}

// Load resolves configuration across all four sources.
func Load(flags Flags) (Config, error) {
	// HideClosed defaults on: a workspace's closed issues are history, and
	// showing them by default buries the open work the viewer exists to show.
	// --hide-closed=false, hide_closed: false, BV_HIDE_CLOSED=false and the c
	// key are all still the way back, and Filter.IsZero() being false at this
	// default is what keeps the status bar announcing the narrowing.
	cfg := Config{Theme: ThemeAuto, View: ViewList, HideClosed: true}

	file, err := readFile()
	if err != nil {
		return Config{}, err
	}
	applyFile(&cfg, file)
	applyEnv(&cfg)

	applyFlags(&cfg, flags)

	if err := validate(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// FilePath returns the configuration file location, honouring XDG.
func FilePath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home directory: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}

	return filepath.Join(dir, "bv", "config.yaml"), nil
}

// readFile loads the config file, treating absence as empty.
//
// A missing file is normal — most users never write one. A malformed file is
// an error, because ignoring a typo in a file someone deliberately created
// hides the very setting they were trying to change.
func readFile() (fileConfig, error) {
	path, err := FilePath()
	if err != nil {
		return fileConfig{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fileConfig{}, nil
		}

		return fileConfig{}, fmt.Errorf("read %s: %w", path, err)
	}

	var file fileConfig
	if err := yaml.Unmarshal(data, &file); err != nil {
		return fileConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}

	return file, nil
}

func applyFile(cfg *Config, file fileConfig) {
	if file.Theme != nil {
		cfg.Theme = ThemePreference(*file.Theme)
	}
	if file.View != nil {
		cfg.View = ViewKind(*file.View)
	}
	if file.HideClosed != nil {
		cfg.HideClosed = *file.HideClosed
	}
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("BV_THEME"); v != "" {
		cfg.Theme = ThemePreference(v)
	}
	if v := os.Getenv("BV_VIEW"); v != "" {
		cfg.View = ViewKind(v)
	}
	if v := os.Getenv("BV_HIDE_CLOSED"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			cfg.HideClosed = parsed
		}
	}
	if v := os.Getenv("BEADS_DIR"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("BV_LOG"); v != "" {
		cfg.LogPath = v
	}
}

func applyFlags(cfg *Config, flags Flags) {
	if flags.Theme != "" {
		cfg.Theme = ThemePreference(flags.Theme)
	}
	if flags.View != "" {
		cfg.View = ViewKind(flags.View)
	}
	if flags.DBPath != "" {
		cfg.DBPath = flags.DBPath
	}
	if flags.HideClosedSet {
		cfg.HideClosed = flags.HideClosed
	}
}

func validate(cfg Config) error {
	switch cfg.Theme {
	case ThemeAuto, ThemeLight, ThemeDark:
	default:
		return fmt.Errorf("invalid theme %q: want auto, light or dark", cfg.Theme)
	}

	switch cfg.View {
	case ViewList, ViewTree, ViewBoard:
	default:
		return fmt.Errorf("invalid view %q: want list, tree or board", cfg.View)
	}

	return nil
}
