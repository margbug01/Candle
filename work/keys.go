package main

import (
	"runtime"

	"github.com/egoist/mygo/ui"
)

// Shortcuts take ui.Cmd: Command on macOS, Ctrl elsewhere. Redo answers to
// both Cmd+Y and Cmd+Shift+Z everywhere; the menu shows the one the platform
// uses. cmdKey is how a tip writes the modifier.
var (
	redoMods, redoKey = ui.Cmd, ui.KeyY
	cmdKey            = "Ctrl+"
)

func init() {
	if runtime.GOOS == "darwin" {
		redoMods, redoKey = ui.Cmd|ui.Shift, ui.KeyZ
		cmdKey = "⌘"
	}
}
