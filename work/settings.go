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
}

func defaultSettings() settings {
	return settings{
		Symbol:     "BTCUSDT",
		Interval:   "1h",
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
