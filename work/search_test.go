package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"candle/market"
)

// usList is Nasdaq's list of n made-up stocks and NVIDIA's.
func usList(n int) []byte {
	var b strings.Builder
	b.WriteString("Nasdaq Traded|Symbol|Security Name|Listing Exchange|Market Category|ETF|Round Lot Size|Test Issue|Financial Status|CQS Symbol|NASDAQ Symbol|NextShares\n")
	for i := range n {
		fmt.Fprintf(&b, "Y|Z%c%c%c|Made Up %d Inc. Common Stock|N| |N|100|N||Z|Z|N\n", 'A'+i/676%26, 'A'+i/26%26, 'A'+i%26, i)
	}
	b.WriteString("Y|NVDA|NVIDIA Corporation - Common Stock|Q|Q|N|100|N|N||NVDA|N\n")
	b.WriteString("Y|NVDL|GraniteShares 2x Long NVDA Daily ETF|Q|G|Y|100|N|N||NVDL|N\n")
	return []byte(b.String())
}

// Nasdaq's list is kept in the cache folder a week: fetched once, read from
// there after, fetched again when older, and the old one kept when Nasdaq
// does not answer. Without one, the search still has the popular stocks.
func TestUSListIsKept(t *testing.T) {
	dir := t.TempDir()
	var fetches int
	app := func(fail bool) *application {
		a := testApp()
		a.spawn = func(f func()) { f() }
		a.icons.dir = dir
		a.usFetch = func(context.Context) ([]byte, error) {
			fetches++
			if fail {
				return nil, errors.New("no answer")
			}
			return usList(1200), nil
		}
		return a
	}
	a := app(false)
	gen := a.searchGen
	a.loadUS()
	file := filepath.Join(dir, "us-symbols.txt")
	if _, err := os.Stat(file); err != nil || fetches != 1 || len(a.usList) < 1200 || a.searchGen == gen {
		t.Fatalf("first run: %d fetches, %d stocks, %v", fetches, len(a.usList), err)
	}
	if hits := a.findings("nvda", 5); len(hits) != 2 || hits[0].Symbol != "US:NVDA" || hits[1].Symbol != "US:NVDL" {
		t.Fatalf("nvda: %v", hits)
	}
	if a.after("US:NVDA") != " 英伟达" || a.after("US:NVDL") != " GraniteShares 2x Long NVDA Daily ETF" {
		t.Fatalf("names: %q %q", a.after("US:NVDA"), a.after("US:NVDL"))
	}
	b := app(true)
	b.loadUS()
	if fetches != 1 || len(b.usList) != len(a.usList) {
		t.Fatalf("kept list: %d fetches, %d stocks", fetches, len(b.usList))
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	os.Chtimes(file, old, old)
	c := app(true)
	c.loadUS()
	if fetches != 2 || len(c.usList) != len(a.usList) {
		t.Fatalf("a week old, Nasdaq silent: %d fetches, %d stocks", fetches, len(c.usList))
	}
	os.Remove(file)
	d := app(true)
	d.loadUS()
	d.loadUS() // not again so soon
	if fetches != 3 || d.usList != nil || d.usFailed.IsZero() {
		t.Fatalf("no list: %d fetches", fetches)
	}
	if hits := d.findings("英伟达", 5); len(hits) != 1 || hits[0].Symbol != "US:NVDA" {
		t.Fatalf("英伟达 without the list: %v", hits)
	}
	if &d.findings("nv", 50)[0] != &d.findings("nv", 50)[0] {
		t.Fatal("searched again for the same query")
	}
}

// Results show as the query is typed, before any price; the prices of the
// futures and stocks among them come a moment after, in one request, and
// Yahoo's search is not asked while Nasdaq's list is in.
func TestSearchShowsAtOnce(t *testing.T) {
	var searches, sparks atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/search") {
			searches.Add(1)
			fmt.Fprint(w, `{"quotes":[]}`)
			return
		}
		sparks.Add(1)
		var parts []string
		for _, s := range strings.Split(r.URL.Query().Get("symbols"), ",") {
			parts = append(parts, fmt.Sprintf(`%q:{"close":[215.38],"chartPreviousClose":217.92}`, s))
		}
		fmt.Fprint(w, "{"+strings.Join(parts, ",")+"}")
	}))
	defer srv.Close()
	queue := make(chan func(), 100)
	a := testApp()
	a.yahoo = market.Yahoo{HTTP: srv.Client(), Base: srv.URL}
	a.spawn = func(f func()) { f() }
	a.usFetch = func(context.Context) ([]byte, error) { return usList(1200), nil }
	a.loadUS()
	a.spawn = func(f func()) { go f() }
	a.post = func(f func()) { queue <- f }
	tt := newTester(a, true)
	a.query = "nvd"
	tt.Frame()
	tt.Frame()
	if !tt.HasText(" 英伟达") || tt.HasText("215.38") {
		t.Fatal("NVIDIA is not in the results at once, or its price came with it")
	}
	deadline := time.After(3 * time.Second)
	for a.quotes["US:NVDA"].Last == 0 {
		select {
		case f := <-queue:
			f()
		case <-deadline:
			t.Fatal("no price")
		}
	}
	tt.Frame()
	if !tt.HasText("215.38") || !tt.HasText("-1.17%") || sparks.Load() != 1 || searches.Load() != 0 {
		t.Fatalf("price shown %v, %d price requests, %d searches of Yahoo's", tt.HasText("215.38"), sparks.Load(), searches.Load())
	}
	tt.Frame()
	tt.Frame()
	if sparks.Load() != 1 {
		t.Fatal("asked for the prices again")
	}
	shot(t, tt, "search-nvd")
}
