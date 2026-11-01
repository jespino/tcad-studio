// Command tcad-studio is TCAD Studio as a desktop app (Wails 3, through
// agentstudio's desktop package): a live 3D preview of the models of a folder
// on the left and a coding agent working in that folder on the right. The
// backend is internal/studio, the UI the Vite app in frontend/ ("tcad studio"
// serves the same UI to a browser).
//
//	tcad-studio [folder]        the desktop app
//	tcad-studio build x.tcad    any tcad command (the agent's terminal runs
//	                            it as "tcad", so it needs no separate install)
package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jespino/agentstudio/desktop"
	tcad "github.com/jespino/tcad-studio"
	"github.com/jespino/tcad-studio/internal/cli"
	"github.com/jespino/tcad-studio/internal/studio"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func main() {
	// Run as the tcad command line: through the "tcad" link the studio
	// puts in the agent's PATH, or with a tcad command.
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if name == "tcad" || (len(os.Args) > 1 && slices.Contains(cli.Commands, os.Args[1])) {
		os.Exit(cli.Main(os.Args[1:]))
	}
	st := studio.New(studio.Options{})
	desktop.Run(st, desktop.Options{
		ID:          "tcad",
		Name:        "TCAD Studio",
		Description: "Design 3D parts with TCAD and an agent",
		Background:  &desktop.RGB{R: 233, G: 237, B: 242},
		Assets:      tcad.UI(),
		Routes:      []string{"/preview"},
		Open:        func(path string) error { _, err := st.Open(path); return err },
		Setup: func(*application.App) func() {
			// The viewer streams its updates: the webview loads it from a
			// loopback server rather than its own scheme.
			stop, err := st.ServePreviewHTTP()
			if err != nil {
				return nil
			}
			return stop
		},
	}, os.Args[1:])
}
