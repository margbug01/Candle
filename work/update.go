package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Candle updates itself with MyGo's signed updates. mygo build signs each
// platform's archive and manifest with the release key, and they go up with
// the GitHub release; work/mygo.json names the repository and holds the
// public key. The app reads its platform's manifest in the latest release,
// and downloads, checks and swaps in a newer version, then starts it. A build
// of go build, or a copy where it cannot write (still in the disk image), can
// only point to the releases page.

const releasesPage = "https://github.com/margbug01/Candle/releases"

type updateStage int

const (
	updateIdle updateStage = iota // not asked yet, or up to date
	updateChecking
	updateAvailable
	updateDownloading
	updateReady // the new version is in place, to start
)

// release is a newer version, and how to install it.
type release struct {
	Version, Notes string
	install        func(ctx context.Context, progress func(done, total int64)) error
}

// updates is where updating is at. Like the rest of application it belongs
// to the main thread: checks and downloads run with spawn and post back.
type updates struct {
	enabled  bool
	check    func(context.Context) (*release, error) // nil when up to date
	relaunch func()

	stage    updateStage
	found    *release
	progress float64 // of the download, 0–1
	err      string  // why the last check or download the user asked for failed
	detail   string  // and what it said
	checked  time.Time
	latest   bool // the last check found nothing newer
	autoPick choice[bool]
}

// myGoUpdates is the updater of a build of mygo build.
func myGoUpdates() updates {
	return updates{
		enabled: mygo.Updater.Enabled(),
		check: func(ctx context.Context) (*release, error) {
			u, err := mygo.Updater.Check(ctx)
			if err != nil || u == nil {
				return nil, err
			}
			return &release{Version: u.Version, Notes: u.Notes, install: u.Install}, nil
		},
		relaunch: mygo.App.Relaunch,
	}
}

// checkUpdate asks whether there is a newer version. manual is the user
// asking, who is told why it failed; a check of its own fails quietly.
func (a *application) checkUpdate(manual bool) {
	u := &a.upd
	if !u.enabled || u.stage == updateChecking || u.stage == updateDownloading || u.stage == updateReady {
		return
	}
	before, check := u.stage, u.check
	u.stage, u.err, u.detail = updateChecking, "", ""
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		found, err := check(ctx)
		a.post(func() {
			u.checked = time.Now()
			switch {
			case err != nil:
				u.stage = before // a version found before is still there
				if manual {
					u.err, u.detail = "检查更新失败："+updateError(err), err.Error()
				}
			case found == nil:
				u.stage, u.found, u.latest = updateIdle, nil, true
			default:
				u.stage, u.found, u.latest = updateAvailable, found, false
			}
		})
	})
}

// installUpdate downloads the update, checks its signature and puts it in
// place of this copy on the disk, then starts it.
func (a *application) installUpdate() {
	u := &a.upd
	found := u.found
	if u.stage != updateAvailable || found == nil {
		return
	}
	u.stage, u.progress, u.err, u.detail = updateDownloading, 0, "", ""
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		shown := -1
		err := found.install(ctx, func(done, total int64) {
			if total <= 0 {
				return
			}
			if pct := int(done * 100 / total); pct != shown { // a frame a percent, not a chunk
				shown = pct
				a.post(func() {
					if u.stage == updateDownloading {
						u.progress = float64(pct) / 100
					}
				})
			}
		})
		a.post(func() {
			if err != nil {
				u.stage, u.err, u.detail = updateAvailable, "更新没装上："+updateError(err), err.Error()
				return
			}
			u.stage = updateReady
			a.restartForUpdate()
		})
	})
}

// restartForUpdate starts the new version. What the settings dialog shows
// and 确认 did not keep goes, as it would with 取消; the rest is saved as it
// changes.
func (a *application) restartForUpdate() {
	if a.editing {
		a.closeSettings(false)
	}
	a.upd.relaunch()
}

// watchUpdates checks a while after opening and every 12 hours after,
// unless the settings say not to.
func (a *application) watchUpdates() {
	if !a.upd.enabled {
		return
	}
	a.spawn(func() {
		time.Sleep(10 * time.Second)
		for {
			a.post(func() {
				if a.set.AutoUpdate {
					a.checkUpdate(false)
				}
			})
			time.Sleep(12 * time.Hour)
		}
	})
}

// updateError says why, for people.
func updateError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "连接 GitHub 超时"
	case strings.Contains(err.Error(), "404"):
		return "GitHub 上的最新版本还没有更新包"
	}
	return "连不上 GitHub，看看网络或系统代理"
}

// noteLines puts the Markdown release notes in a few lines of plain text:
// no headings or emphasis, items as bullets, the rest cut.
func noteLines(md string, max int) []string {
	var out []string
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, bullet := range []string{"- ", "* "} {
			if rest, ok := strings.CutPrefix(line, bullet); ok {
				line = "· " + rest
			}
		}
		out = append(out, strings.NewReplacer("**", "", "`", "").Replace(line))
	}
	if max > 0 && len(out) > max {
		out = append(out[:max-1], "…")
	}
	return out
}

// appVersion is the version of mygo.json, which go build leaves out.
func appVersion() string {
	if v := mygo.App.Version(); v != "" {
		return v
	}
	return "开发版"
}

// openAbout shows the settings' 关于 page, where updating is.
func (a *application) openAbout() {
	if a.indList || a.indDialog {
		return
	}
	if !a.dialog {
		a.openSettings()
	}
	for i, pg := range settingsPages {
		if pg.label == "关于" {
			a.page = i
		}
	}
}

// aboutPage is the version and updating.
func (a *application) aboutPage(c *ui.Context, t *ui.Theme, k palette) {
	u := &a.upd
	section(c, t, "Candle")
	infoRow(c, t, "版本 "+appVersion(), "照着 TradingView 做的 Binance K 线图。和 TradingView、Binance 没有关系，也不构成任何投资建议", func() {
		ui.Link(c, "GitHub", "https://github.com/margbug01/Candle")
	})
	section(c, t, "更新")
	switch {
	case !u.enabled:
		infoRow(c, t, "这一份不能自己更新", "自己编译的，或者放在没有写入权限的地方（比如还在 dmg 里）。新版本到 GitHub 下载", func() {
			ui.Link(c, "打开下载页", releasesPage)
		})
	case u.stage == updateAvailable:
		infoRow(c, t, "有新版本 "+u.found.Version, "当前 "+appVersion()+"，更新时 Candle 会重新打开一下", func() {
			if ui.PrimaryButton(c.Key("install-update"), "更新并重启").MinWidth(96).Clicked() {
				a.installUpdate()
			}
		})
		if lines := noteLines(u.found.Notes, 8); len(lines) > 0 {
			ui.Column(c).FillWidth().Gap(4).Padding(0, 0, 8, 0).Children(func() {
				for _, line := range lines {
					ui.Text(c, line).FontSize(12).TextColor(t.TextMuted)
				}
			})
		}
	case u.stage == updateDownloading:
		infoRow(c, t, "正在下载 "+u.found.Version, fmt.Sprintf("%d%%，装好会自己重新打开", int(u.progress*100)), func() {
			ui.Progress(c, u.progress).Width(160)
		})
	case u.stage == updateReady:
		infoRow(c, t, u.found.Version+" 已经装好", "重新打开 Candle 后生效", func() {
			if ui.PrimaryButton(c.Key("restart-update"), "立即重启").MinWidth(96).Clicked() {
				a.restartForUpdate()
			}
		})
	default: // not asked, up to date, asking
		title, desc := "检查新版本", "还没检查过"
		if u.latest {
			now := time.Now()
			at := u.checked.Format("15:04")
			if y, m, d := u.checked.Date(); y != now.Year() || m != now.Month() || d != now.Day() {
				at = u.checked.Format("01-02 15:04")
			}
			title, desc = "已是最新版本", at+" 检查过"
		}
		infoRow(c, t, title, desc, func() {
			label := "检查更新"
			if u.stage == updateChecking {
				label = "检查中…"
			}
			if ui.Button(c.Key("check-update"), label).Disabled(u.stage == updateChecking).Clicked() {
				a.checkUpdate(true)
			}
		})
	}
	if u.err != "" {
		ui.Text(c, u.err).FontSize(12).TextColor(k.down).Padding(0, 0, 8, 0).Tooltip(u.detail)
	}
	if u.enabled {
		// Kept at once, whatever 确认 or 取消 do to the chart's settings.
		p, on, changed := u.autoPick.bind(a.set.AutoUpdate)
		checkRow(c, p, "自动检查更新", func() {
			ui.Text(c, "打开时和之后每 12 小时问一次 GitHub，只提示，不自己装").FontSize(12).TextColor(t.TextMuted)
		})
		if changed {
			a.set.AutoUpdate = on
			a.save()
			if on {
				a.checkUpdate(false)
			}
		}
	}
}

// infoRow is a line of what is, with what it says below it and the controls
// on the right.
func infoRow(c *ui.Context, t *ui.Theme, title, desc string, controls func()) {
	ui.Row(c).FillWidth().MinHeight(52).Gap(16).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, title).FontSize(13)
			if desc != "" {
				ui.Text(c, desc).FontSize(12).TextColor(t.TextMuted)
			}
		})
		if controls != nil {
			ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Children(controls)
		}
	})
}

// updateBadge sits beside the gear when there is a new version, and opens
// the 关于 page.
func (a *application) updateBadge(c *ui.Context, t *ui.Theme, k palette) {
	u := &a.upd
	if u.found == nil || u.stage != updateAvailable && u.stage != updateDownloading && u.stage != updateReady {
		return
	}
	text := "新版本 " + u.found.Version
	b := ui.ButtonBase(c.Key("update-badge")).Height(26).Padding(0, 10).Shrink(0).Center().Radius(13).Cursor(ui.CursorPointer).
		Label(text).Tooltip("打开设置 › 关于，更新 Candle").Background(k.chosen)
	if b.Hovered() {
		b.Background(k.hover)
	}
	if b.Children(func() { ui.Text(c, text).FontSize(12).FontWeight(500).TextColor(k.accent).SingleLine() }).Clicked() {
		a.openAbout()
	}
}
