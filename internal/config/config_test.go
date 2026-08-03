package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/config"
)

func TestConfig(t *testing.T) {
	suite.Run(t, new(configTestSuite))
}

type configTestSuite struct {
	suite.Suite
}

// isolate points HOME and XDG_CONFIG_HOME at a temp dir and clears every
// variable Load reads, so a developer's real environment cannot change the
// result of a test run.
func (s *configTestSuite) isolate() string {
	home := s.T().TempDir()
	s.T().Setenv("HOME", home)
	s.T().Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	for _, key := range []string{"BV_THEME", "BV_VIEW", "BV_HIDE_CLOSED", "BV_LOG", "BEADS_DIR"} {
		s.T().Setenv(key, "")
	}

	return home
}

func (s *configTestSuite) writeConfig(body string) {
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "bv")
	s.Require().NoError(os.MkdirAll(dir, 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600))
}

func (s *configTestSuite) TestDefaults() {
	s.isolate()

	cfg, err := config.Load(config.Flags{})
	s.Require().NoError(err)
	s.Equal(config.ThemeAuto, cfg.Theme)
	s.Equal(config.ViewList, cfg.View)
	s.True(cfg.HideClosed, "closed issues are noise until asked for; c and "+
		"--hide-closed=false are the ways back")
}

func (s *configTestSuite) TestHideClosedDefaultIsOverridableByEveryLayer() {
	for _, tc := range []struct {
		name  string
		setup func()
		flags config.Flags
		want  bool
	}{
		{
			name:  "file turns it off",
			setup: func() { s.writeConfig("hide_closed: false\n") },
			flags: config.Flags{},
			want:  false,
		},
		{
			name:  "environment turns it off",
			setup: func() { s.T().Setenv("BV_HIDE_CLOSED", "false") },
			flags: config.Flags{},
			want:  false,
		},
		{
			name:  "flag turns it off",
			setup: func() {},
			flags: config.Flags{HideClosed: false, HideClosedSet: true},
			want:  false,
		},
		{
			name:  "an absent flag does not override the default",
			setup: func() {},
			flags: config.Flags{HideClosed: false},
			want:  true,
		},
		{
			name:  "an absent flag does not override a file that turned it off",
			setup: func() { s.writeConfig("hide_closed: false\n") },
			flags: config.Flags{HideClosed: true}, // Deliberately disagree with file: if applyFlags drops the HideClosedSet guard and assigns unconditionally, it would wrongly change the result to true.
			want:  false,
		},
	} {
		s.Run(tc.name, func() {
			s.isolate()
			tc.setup()

			cfg, err := config.Load(tc.flags)
			s.Require().NoError(err)
			s.Equal(tc.want, cfg.HideClosed)
		})
	}
}

func (s *configTestSuite) TestMissingFileIsNotAnError() {
	s.isolate()

	_, err := config.Load(config.Flags{})
	s.NoError(err, "most users never create a config file")
}

func (s *configTestSuite) TestFileValues() {
	s.isolate()
	s.writeConfig("theme: dark\nview: tree\nhide_closed: true\n")

	cfg, err := config.Load(config.Flags{})
	s.Require().NoError(err)
	s.Equal(config.ThemeDark, cfg.Theme)
	s.Equal(config.ViewTree, cfg.View)
	s.True(cfg.HideClosed)
}

func (s *configTestSuite) TestMalformedFileIsAnError() {
	s.isolate()
	s.writeConfig("theme: [unclosed\n")

	_, err := config.Load(config.Flags{})
	s.Require().Error(err, "a typo in a file the user wrote must not be ignored")
	s.Contains(err.Error(), "config.yaml")
}

func (s *configTestSuite) TestEnvironmentBeatsFile() {
	s.isolate()
	s.writeConfig("theme: dark\n")
	s.T().Setenv("BV_THEME", "light")

	cfg, err := config.Load(config.Flags{})
	s.Require().NoError(err)
	s.Equal(config.ThemeLight, cfg.Theme)
}

func (s *configTestSuite) TestFlagBeatsEnvironment() {
	s.isolate()
	s.writeConfig("theme: dark\n")
	s.T().Setenv("BV_THEME", "light")

	cfg, err := config.Load(config.Flags{Theme: "dark"})
	s.Require().NoError(err)
	s.Equal(config.ThemeDark, cfg.Theme)
}

// TestFlagWinsAcrossFullChain sets a distinct value at every layer — default
// (auto), file (dark), environment (light) — and a flag value that matches
// neither the file nor the environment. If the flag were not the last word,
// the result would be light (environment beating file); it must be auto.
func (s *configTestSuite) TestFlagWinsAcrossFullChain() {
	s.isolate()
	s.writeConfig("theme: dark\n")
	s.T().Setenv("BV_THEME", "light")

	cfg, err := config.Load(config.Flags{Theme: "auto"})
	s.Require().NoError(err)
	s.Equal(config.ThemeAuto, cfg.Theme, "flag must win over environment, file and default")
}

func (s *configTestSuite) TestHideClosedFlagCanOverrideFileToFalse() {
	// A bare bool cannot tell --hide-closed=false from the flag being absent,
	// which is why Flags carries HideClosedSet.
	s.isolate()
	s.writeConfig("hide_closed: true\n")

	cfg, err := config.Load(config.Flags{HideClosed: false, HideClosedSet: true})
	s.Require().NoError(err)
	s.False(cfg.HideClosed)

	cfg, err = config.Load(config.Flags{HideClosed: false, HideClosedSet: false})
	s.Require().NoError(err)
	s.True(cfg.HideClosed, "an unset flag must not override the file")
}

func (s *configTestSuite) TestDepsIsAValidView() {
	s.isolate()
	s.T().Setenv("BV_VIEW", "deps")

	cfg, err := config.Load(config.Flags{})
	s.Require().NoError(err)
	s.Equal(config.ViewDeps, cfg.View)
}

func (s *configTestSuite) TestInvalidValuesAreRejected() {
	s.isolate()

	_, err := config.Load(config.Flags{Theme: "purple"})
	s.Require().Error(err)
	s.Contains(err.Error(), "purple")

	_, err = config.Load(config.Flags{View: "gantt"})
	s.Require().Error(err)
	s.Contains(err.Error(), "gantt")
}

func (s *configTestSuite) TestNewLoggerDiscardsByDefault() {
	log, closeLog, err := config.NewLogger("")
	s.Require().NoError(err)
	defer closeLog()

	s.NotNil(log)
}

func (s *configTestSuite) TestNewLoggerWritesToFile() {
	path := filepath.Join(s.T().TempDir(), "bv.log")

	log, closeLog, err := config.NewLogger(path)
	s.Require().NoError(err)
	defer closeLog()

	log.Info("hello")

	data, err := os.ReadFile(path)
	s.Require().NoError(err)
	s.Contains(string(data), "hello")
}

func (s *configTestSuite) TestNewLoggerRejectsUnwritablePath() {
	_, _, err := config.NewLogger(filepath.Join(s.T().TempDir(), "missing-dir", "bv.log"))
	s.Require().Error(err)
}

func (s *configTestSuite) TestBeadsDirAndLogPathComeFromEnvironment() {
	s.isolate()
	s.T().Setenv("BEADS_DIR", "/tmp/somewhere/.beads")
	s.T().Setenv("BV_LOG", "/tmp/bv.log")

	cfg, err := config.Load(config.Flags{})
	s.Require().NoError(err)
	s.Equal("/tmp/somewhere/.beads", cfg.DBPath)
	s.Equal("/tmp/bv.log", cfg.LogPath)

	cfg, err = config.Load(config.Flags{DBPath: "/explicit/.beads"})
	s.Require().NoError(err)
	s.Equal("/explicit/.beads", cfg.DBPath)
}
