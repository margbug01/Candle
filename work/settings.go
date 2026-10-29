package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
)

// settings are kept between runs in %APPDATA%\Candle\settings.json, and on
// macOS in ~/Library/Application Support/Candle/settings.json.
type settings struct {
	// Layout is which of TradingView's layouts shows the charts, "" for one
	// chart. Charts are the layout's charts, in TradingView's order, those a
	// layout with fewer hides too; Active is the one the toolbar acts on.
	Layout string `json:",omitempty"`
	Charts []chartSettings
	Active int
	Sync   syncSettings
	// Splits are the shares of the parts of each layout as dragged, by
	// layout and its splits' node: nil for alike.
	Splits     map[string][][]float64 `json:",omitempty"`
	Watch      []string
	Chart      style
	Drawings   map[string][]drawing // by market
	Magnet     bool
	AutoUpdate bool // check for a new version now and then
	// FuturesRTH shows futures' intraday bars in the regular trading hours
	// only (RTH), not the whole electronic session (ETH).
	FuturesRTH bool `json:",omitempty"`
	// Sections are the watchlist's sections in the order they show, by key
	// (crypto, futures, us); Folded those folded away.
	Sections []string `json:",omitempty"`
	Folded   []string `json:",omitempty"`
}

// chartSettings are a chart's: its market, its bars and its indicators.
type chartSettings struct {
	Symbol     string
	Interval   string
	Indicators []indicator
}

// savedSettings are what the file holds: the settings, and the active
// chart as versions before the layouts kept the one chart.
type savedSettings struct {
	settings
	Symbol     string
	Interval   string
	Indicators []indicator
}

func defaultSettings() settings {
	return settings{
		Charts:     []chartSettings{{Symbol: "BTCUSDT", Interval: "1h"}},
		Sync:       syncSettings{Crosshair: true},
		Watch:      []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT", "XRPUSDT", "DOGEUSDT", "SUIUSDT", "ADAUSDT"},
		Chart:      defaultStyle(),
		Magnet:     true,
		AutoUpdate: true,
	}
}

func settingsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "Candle", "settings.json")
}

// loadSettings reads the file; what is missing or damaged keeps its default.
// A file from before the layouts has the one chart.
func loadSettings(file string) settings {
	saved := savedSettings{settings: defaultSettings()}
	saved.Charts = nil
	if data, err := os.ReadFile(file); err == nil {
		json.Unmarshal(data, &saved)
	}
	s := saved.settings
	if len(s.Charts) == 0 {
		s.Charts = []chartSettings{{Symbol: saved.Symbol, Interval: saved.Interval, Indicators: saved.Indicators}}
	}
	s.Charts = s.Charts[:min(len(s.Charts), maxCells)]
	for i := range s.Charts {
		s.Charts[i].fix()
	}
	s.Active = min(max(s.Active, 0), len(s.Charts)-1)
	s.Chart.fix()
	for sym, ds := range s.Drawings { // what a damaged file holds that cannot be drawn
		s.Drawings[sym] = slices.DeleteFunc(ds, func(d drawing) bool {
			t, ok := toolOf(d.Kind)
			return !ok || len(d.Pts) != t.points
		})
	}
	return s
}

// fix gives a chart missing its market or its bars the default ones.
func (cs *chartSettings) fix() {
	if _, ok := intervalOf(cs.Interval); !ok {
		cs.Interval = "1h"
	}
	if cs.Symbol == "" {
		cs.Symbol = "BTCUSDT"
	}
	cs.Indicators = fixIndicators(cs.Indicators)
}

// saveSettings writes beside the file and renames over it, so a crash halfway
// leaves the old one. An empty name saves nothing, for the tests.
func saveSettings(file string, s settings) error {
	if file == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	saved := savedSettings{settings: s}
	if len(s.Charts) > 0 {
		cs := s.Charts[min(max(s.Active, 0), len(s.Charts)-1)]
		saved.Symbol, saved.Interval, saved.Indicators = cs.Symbol, cs.Interval, cs.Indicators
	}
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}
