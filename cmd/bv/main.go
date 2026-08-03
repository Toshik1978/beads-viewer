// Command bv is a read-only terminal browser for a .beads workspace.
package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
	"github.com/Toshik1978/beads-viewer/internal/watch"
)

// version is overwritten at release time via -ldflags.
var version = "dev"

func main() {
	os.Exit(run())
}

// run is the composition root: it resolves configuration, wires the watcher
// and the tea program, and turns every failure into an exit code rather than
// a panic, so a misconfigured start reads as one line on stderr.
func run() int {
	var flags config.Flags

	cmd := &cobra.Command{
		Use:           "bv",
		Short:         "A read-only terminal browser for a .beads workspace.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
		RunE: func(cmd *cobra.Command, _ []string) error {
			flags.HideClosedSet = cmd.Flags().Changed("hide-closed")

			return start(flags)
		},
	}

	cmd.Flags().StringVar(&flags.DBPath, "db", "",
		"path to a .beads directory (overrides BEADS_DIR)")
	cmd.Flags().StringVar(&flags.Theme, "theme", "", "colour scheme: auto, light or dark")
	cmd.Flags().StringVar(&flags.View, "view", "", "initial view: list, tree, board or deps")
	cmd.Flags().BoolVar(&flags.HideClosed, "hide-closed", false, "hide closed issues")

	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "bv:", err)

		return 1
	}

	return 0
}

// start wires the application once configuration is known.
func start(flags config.Flags) error {
	cfg, err := config.Load(flags)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log, closeLog, err := config.NewLogger(cfg.LogPath)
	if err != nil {
		return fmt.Errorf("create logger: %w", err)
	}
	defer closeLog()

	ws, err := beads.FindWorkspace(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("find workspace: %w", err)
	}

	snapshot, err := beads.LoadSnapshot(ws)
	if err != nil {
		return fmt.Errorf("load snapshot: %w", err)
	}

	watcher, err := watch.New(log, ws.IssuesPath, watch.DefaultDebounce)
	if err != nil {
		return fmt.Errorf("start watcher: %w", err)
	}
	defer func() { _ = watcher.Close() }()

	model, err := tui.NewModel(tui.Deps{
		Log:      log,
		Cfg:      cfg,
		Theme:    theme.New(cfg.Theme, theme.BackgroundUnknown),
		Snapshot: snapshot,
		Reload:   reloadFunc(ws),
	})
	if err != nil {
		return fmt.Errorf("build model: %w", err)
	}
	model.LoadTreeState(ws.Dir)
	// This defer covers q, in-TUI ctrl+c and an error return from Run below —
	// every path that unwinds this function normally. It does not cover an
	// external SIGINT/SIGTERM: Go never runs deferred functions on an
	// unhandled signal, and forgetting expansion state on a killed process is
	// accepted, not fixed, here.
	defer model.SaveTreeState(ws.Dir)

	program := tea.NewProgram(newProgramModel(model))
	go forward(watcher, program)

	if _, err := program.Run(); err != nil {
		return fmt.Errorf("run program: %w", err)
	}

	return nil
}

// reloadFunc builds the tea.Cmd the watcher goroutine triggers to re-read the
// workspace, reporting failure as a message rather than an error so a bad
// write surfaces in the status bar instead of exiting. It returns a func type
// rather than tea.Msg directly: ireturn flags a declared function returning an
// interface, but not one hidden behind the closure this returns.
func reloadFunc(ws beads.Workspace) func() tea.Msg {
	return func() tea.Msg {
		snapshot, err := beads.LoadSnapshot(ws)

		return tui.SnapshotMsg{Snapshot: snapshot, Err: err}
	}
}

// forward turns watcher events into program messages.
func forward(watcher *watch.Watcher, program *tea.Program) {
	for range watcher.Events() {
		program.Send(tui.ReloadRequestedMsg{})
	}
}
