// Command candle is a candlestick chart of Binance's spot markets.
package main

import (
	"net/http"
	"os"
	"time"
	_ "time/tzdata" // the zones the settings offer, on machines without Go

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"candle/market"
)

func main() {
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	file := settingsPath()
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = market.SystemProxy
	bin := market.Binance{HTTP: &http.Client{Transport: tr, Timeout: 30 * time.Second}}
	var win *mygo.Window
	set := loadSettings(file)
	applyTheme(set.Chart.Theme)
	a := newApplication(set, file, bin, func(fn func()) { win.Update(fn) })
	a.setMenu()
	mygo.App.OnSecondInstance(func([]string, string) {
		if win != nil {
			win.Show()
			win.Restore()
			win.Focus()
		}
	})
	mygo.App.WhenReady(func() {
		win = mygo.NewWindow(mygo.WindowOptions{
			Title: "Candle", Width: 1320, Height: 820, MinWidth: 960, MinHeight: 560, StateKey: "main",
			TitleBarStyle: mygo.TitleBarHidden, TitleBarHeight: titleBarHeight,
			Content: ui.View(a.render),
		})
		a.start()
	})
	if err := mygo.App.Run(); err != nil {
		os.Exit(1)
	}
}
