package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// updateApp is testApp with an updater that answers found, or fails with
// fail, without the network, and runs its background work at once.
func updateApp(found *release, fail error) (*application, *int) {
	a := testApp()
	a.spawn = func(f func()) { f() }
	relaunched := new(int)
	a.upd = updates{
		enabled:  true,
		check:    func(context.Context) (*release, error) { return found, fail },
		relaunch: func() { *relaunched++ },
	}
	return a, relaunched
}

// A new version shows beside the gear; that opens 关于, where 更新并重启
// installs it, showing how far it got, and starts it.
func TestUpdateInstalls(t *testing.T) {
	var reports []float64
	found := &release{Version: "9.9.0", Notes: "## 9.9.0\n\n- **更快**的 K 线\n- 新指标"}
	a, relaunched := updateApp(found, nil)
	found.install = func(_ context.Context, progress func(done, total int64)) error {
		for _, n := range []int64{0, 10, 11, 50, 100} {
			progress(n, 100)
			reports = append(reports, a.upd.progress)
		}
		return nil
	}
	tt := newTester(a, false)
	if err := tt.Click("新版本 9.9.0"); err == nil {
		t.Fatal("a new version shows before any check")
	}
	a.checkUpdate(false)
	tt.Frame()
	click(t, tt, "新版本 9.9.0")
	if !a.dialog || settingsPages[a.page].label != "关于" {
		t.Fatalf("the badge opened page %d, dialog %v", a.page, a.dialog)
	}
	for _, s := range []string{"有新版本 9.9.0", "· 更快的 K 线", "· 新指标"} {
		if !tt.HasText(s) {
			t.Fatalf("关于 shows no %q", s)
		}
	}
	shot(t, tt, "update-available")
	click(t, tt, "更新并重启")
	if want := []float64{0, 0.1, 0.11, 0.5, 1}; !reflect.DeepEqual(reports, want) {
		t.Fatalf("progress %v, want %v", reports, want)
	}
	if a.upd.stage != updateReady || *relaunched != 1 {
		t.Fatalf("stage %v, relaunched %d times", a.upd.stage, *relaunched)
	}
	if a.dialog || a.editing {
		t.Fatal("the settings stayed open over the restart")
	}
}

// A failed download says so and offers the update again.
func TestUpdateInstallFails(t *testing.T) {
	found := &release{Version: "9.9.0"}
	found.install = func(context.Context, func(int64, int64)) error { return errors.New("signature mismatch") }
	a, relaunched := updateApp(found, nil)
	tt := newTester(a, false)
	a.checkUpdate(false)
	a.openAbout()
	tt.Frame()
	click(t, tt, "更新并重启")
	if a.upd.stage != updateAvailable || *relaunched != 0 {
		t.Fatalf("stage %v, relaunched %d times", a.upd.stage, *relaunched)
	}
	if !tt.HasText("更新没装上：连不上 GitHub，看看网络或系统代理") {
		t.Fatal("no " + "更新没装上：连不上 GitHub，看看网络或系统代理")
	}
}

// Asking finds it up to date, or says why it could not tell; a check of
// its own that fails says nothing.
func TestUpdateCheck(t *testing.T) {
	a, _ := updateApp(nil, nil)
	tt := newTester(a, false)
	a.openAbout()
	tt.Frame()
	click(t, tt, "检查更新")
	if !tt.HasText("已是最新版本") {
		t.Fatal("no 已是最新版本")
	}
	if !a.upd.latest || a.upd.checked.IsZero() {
		t.Fatal("not up to date after checking")
	}
	shot(t, tt, "update-latest")

	a, _ = updateApp(nil, context.DeadlineExceeded)
	tt = newTester(a, false)
	a.checkUpdate(false)
	if a.upd.err != "" {
		t.Fatalf("a check of its own told: %q", a.upd.err)
	}
	a.openAbout()
	tt.Frame()
	click(t, tt, "检查更新")
	if !tt.HasText("检查更新失败：连接 GitHub 超时") {
		t.Fatal("no " + "检查更新失败：连接 GitHub 超时")
	}
}

// A copy that cannot update itself points to the releases, and checks nothing.
func TestUpdateDisabled(t *testing.T) {
	a, _ := updateApp(nil, nil)
	a.upd.enabled = false
	tt := newTester(a, false)
	a.checkUpdate(true)
	a.openAbout()
	tt.Frame()
	if a.upd.stage != updateIdle || !a.upd.checked.IsZero() {
		t.Fatal("a copy that cannot update checked")
	}
	for _, s := range []string{"这一份不能自己更新", "打开下载页"} {
		if !tt.HasText(s) {
			t.Fatalf("no %q", s)
		}
	}
	if tt.HasText("自动检查更新") {
		t.Fatal("自动检查更新 shows where nothing updates")
	}
}

// 自动检查更新 is saved at once, and saving while the dialog is open keeps
// the chart's settings as 确认 last left them.
func TestAutoUpdateSavedAtOnce(t *testing.T) {
	a, _ := updateApp(nil, nil)
	a.file = filepath.Join(t.TempDir(), "settings.json")
	tt := newTester(a, false)
	click(t, tt, "图表设置")
	grid := a.set.Chart.VertGrid
	a.set.Chart.VertGrid = !grid // as a page of the dialog would
	a.openAbout()
	tt.Frame()
	click(t, tt, "自动检查更新")
	saved := loadSettings(a.file)
	if saved.AutoUpdate || saved.Chart.VertGrid != grid {
		t.Fatalf("saved AutoUpdate %v, grid %v (was %v)", saved.AutoUpdate, saved.Chart.VertGrid, grid)
	}
	click(t, tt, "取消")
	if a.set.AutoUpdate || a.set.Chart.VertGrid != grid {
		t.Fatalf("after 取消: AutoUpdate %v, grid %v", a.set.AutoUpdate, a.set.Chart.VertGrid)
	}
	if _, err := os.Stat(a.file); err != nil {
		t.Fatal(err)
	}
}

func TestNoteLines(t *testing.T) {
	got := noteLines("## 0.2.0\n\n- 新增 **macOS** 版\n* `⌘,` 打开设置\n\n一段话\n- 四\n- 五", 4)
	want := []string{"· 新增 macOS 版", "· ⌘, 打开设置", "一段话", "…"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
