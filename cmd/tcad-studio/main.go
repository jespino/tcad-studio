// Command tcad-studio is TCAD Studio as a desktop app (Wails 3): a live 3D
// preview of the models of a folder on the left and a coding agent working
// in that folder on the right. The app is internal/studio, the same server
// "tcad studio" runs for a browser, shown in a native window.
//
//	tcad-studio [folder]        the desktop app
//	tcad-studio build x.tcad    any tcad command (the agent's terminal runs
//	                            it as "tcad", so it needs no separate install)
package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

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
	url, err := st.Start("127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	// The same session works in a browser, which helps when debugging the page.
	log.Printf("TCAD Studio at %s", url)

	wa := application.New(application.Options{
		Name:        "TCAD Studio",
		Description: "Design 3D parts with TCAD and an agent",
		// The window shows the studio's own server; Wails serves nothing.
		Assets:     application.AssetOptions{Handler: http.NotFoundHandler()},
		OnShutdown: st.Close,
		Mac:        application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
	})
	st.SetDialogs(dialogs{wa})
	// After SetDialogs: the folder's preview saves STLs with the native dialog.
	if len(os.Args) > 1 {
		if _, err := st.Open(os.Args[1]); err != nil {
			log.Fatal(err)
		}
	}
	wa.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "TCAD Studio",
		Width:            1600,
		Height:           960,
		BackgroundColour: application.NewRGB(233, 237, 242),
		URL:              url,
	})
	if err := wa.Run(); err != nil {
		log.Fatal(err)
	}
}

// dialogs adapts Wails' native dialogs to studio.Dialogs.
type dialogs struct{ wa *application.App }

func (d dialogs) ChooseDirectory(title, start string) (string, error) {
	dlg := d.wa.Dialog.OpenFile().
		SetTitle(title).
		CanChooseDirectories(true).
		CanChooseFiles(false).
		CanCreateDirectories(true)
	if start != "" {
		dlg.SetDirectory(start)
	}
	if w := d.wa.Window.Current(); w != nil {
		dlg.AttachToWindow(w)
	}
	return dlg.PromptForSingleSelection()
}

func (d dialogs) SaveFile(title, dir, name string) (string, error) {
	dlg := d.wa.Dialog.SaveFile().
		SetMessage(title).
		SetFilename(name).
		CanCreateDirectories(true).
		AddFilter("STL files", "*.stl")
	if dir != "" {
		dlg.SetDirectory(dir)
	}
	if w := d.wa.Window.Current(); w != nil {
		dlg.AttachToWindow(w)
	}
	return dlg.PromptForSingleSelection()
}
