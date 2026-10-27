package main

import (
	"runtime"

	"github.com/egoist/mygo"
)

// setMenu gives Candle a menu bar on macOS, in Chinese, with the settings in
// the app menu. Windows has none: the title bar is the app's own.
//
// It has no Edit menu. Its Undo, Copy and Paste would take Cmd+Z, Cmd+C and
// Cmd+V before the window sees them and hand them only to a text input with
// the focus, so the drawings' undo, copy and paste would never get them.
// Without it the keys reach the window as they do on Windows: a text input
// with the focus takes them first, the drawings the rest.
func (a *application) setMenu() {
	if runtime.GOOS != "darwin" {
		return
	}
	settings := func(*mygo.MenuItem, *mygo.Window) {
		a.post(func() {
			if !a.dialog && !a.indList && !a.indDialog {
				a.openSettings()
			}
		})
	}
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Candle", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout, Label: "关于 Candle"},
			mygo.Separator(),
			{Label: "设置…", Accelerator: "CmdOrCtrl+,", Click: settings},
			mygo.Separator(),
			{Role: mygo.RoleServices, Label: "服务"},
			mygo.Separator(),
			{Role: mygo.RoleHide, Label: "隐藏 Candle"},
			{Role: mygo.RoleHideOthers, Label: "隐藏其他"},
			{Role: mygo.RoleUnhide, Label: "全部显示"},
			mygo.Separator(),
			{Role: mygo.RoleQuit, Label: "退出 Candle"},
		}},
		{Label: "显示", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleToggleFullScreen, Label: "进入全屏幕"},
		}},
		{Label: "窗口", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleMinimize, Label: "最小化"},
			{Role: mygo.RoleZoom, Label: "缩放"},
			mygo.Separator(),
			{Role: mygo.RoleClose, Label: "关闭"},
			{Role: mygo.RoleFront, Label: "前置全部窗口"},
		}},
	}))
}
