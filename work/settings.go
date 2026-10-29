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
	Symbol     string
	Interval   string
	Watch      []string
	Chart      style
	Drawings   map[string][]drawing // by market
	Magnet     bool
	Indicators []indicator // for every market
	AutoUpdate bool        // check for a new version now and then
	// FuturesRTH shows futures' intraday bars in the regular trading hours
	// only (RTH), not the whole electronic session (ETH).
	FuturesRTH bool `json:",omitempty"`
	// Sections are the watchlist's sections in the order they show, by key
	// (crypto, futures, us); Folded those folded away.
	Sections []string `json:",omitempty"`
	Folded   []string `json:",omitempty"`
	// FavTools are the drawing tools on the bar of favorites, which floats
	// over the chart, its top left FavX and FavY of the way across and down
	// it; FavBar shows it.
	FavTools   []string
	FavBar     bool
	FavX, FavY float64
}

func defaultSettings() settings {
	return settings{
		Symbol:     "BTCUSDT",
		Interval:   "1h",
		Watch:      []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT", "XRPUSDT", "DOGEUSDT", "SUIUSDT", "ADAUSDT"},
		Chart:      defaultStyle(),
		Magnet:     true,
		AutoUpdate: true,
		FavTools:   []string{"trend", "hline", "rect", "fib", "long", "short"},
		FavBar:     true,
		FavX:       1,
		FavY:       0.01,
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
func loadSettings(file string) settings {
	s := defaultSettings()
	if data, err := os.ReadFile(file); err == nil {
		json.Unmarshal(data, &s)
	}
	if _, ok := intervalOf(s.Interval); !ok {
		s.Interval = "1h"
	}
	s.Chart.fix()
	for sym, ds := range s.Drawings { // what a damaged file holds that cannot be drawn
		s.Drawings[sym] = slices.DeleteFunc(ds, func(d drawing) bool {
			t, ok := toolOf(d.Kind)
			return !ok || len(d.Pts) != t.points
		})
	}
	s.Indicators = fixIndicators(s.Indicators)
	seen := map[string]bool{}
	s.FavTools = slices.DeleteFunc(s.FavTools, func(k string) bool {
		_, ok := toolOf(k)
		drop := !ok || seen[k]
		seen[k] = true
		return drop
	})
	s.FavX, s.FavY = min(max(s.FavX, 0), 1), min(max(s.FavY, 0), 1)
	if s.Symbol == "" {
		s.Symbol = "BTCUSDT"
	}
	return s
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
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}
