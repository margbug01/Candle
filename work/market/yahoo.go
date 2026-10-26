package market

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
)

const yahooChart = "https://query1.finance.yahoo.com/v8/finance/chart/"

// Yahoo reads the chart endpoint behind Yahoo Finance's own pages. It needs no
// key, is not a published API, and can change or throttle without notice.
type Yahoo struct{ HTTP *http.Client }

// Chart is one symbol's bars and what is needed to label them.
type Chart struct {
	Bars     []Bar
	Timezone string // the exchange's IANA zone, e.g. America/New_York
	Skipped  int    // rows without prices or outside the regular session
	Splits   int    // splits inside the range; prices are already adjusted for them
}

// Chart returns the bars between from and to (Unix seconds). Intraday intervals
// only reach back so far: 1m about a week, 5m to 30m 60 days, 1h 730 days.
func (y Yahoo) Chart(ctx context.Context, symbol, interval string, from, to int64) (Chart, error) {
	q := url.Values{
		"interval": {interval},
		"period1":  {strconv.FormatInt(from, 10)},
		"period2":  {strconv.FormatInt(to, 10)},
		"events":   {"split"},
	}
	body, err := get(ctx, y.HTTP, yahooChart+url.PathEscape(symbol)+"?"+q.Encode())
	if err != nil {
		return Chart{}, fmt.Errorf("yahoo %s: %w", symbol, err)
	}
	c, err := parseChart(body, interval)
	if err != nil {
		return Chart{}, fmt.Errorf("yahoo %s: %w", symbol, err)
	}
	return c, nil
}

type chartResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Timezone    string `json:"exchangeTimezoneName"`
				Granularity string `json:"dataGranularity"`
				// Intraday only: one list of {start, end} per day.
				Sessions json.RawMessage `json:"tradingPeriods"`
			} `json:"meta"`
			Timestamp []int64 `json:"timestamp"`
			Events    struct {
				Splits map[string]json.RawMessage `json:"splits"`
			} `json:"events"`
			Indicators struct {
				Quote []struct {
					Open   []*float64 `json:"open"`
					High   []*float64 `json:"high"`
					Low    []*float64 `json:"low"`
					Close  []*float64 `json:"close"`
					Volume []*float64 `json:"volume"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

func parseChart(body []byte, interval string) (Chart, error) {
	var r chartResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return Chart{}, err
	}
	if e := r.Chart.Error; e != nil {
		return Chart{}, fmt.Errorf("%s: %s", e.Code, e.Description)
	}
	if len(r.Chart.Result) != 1 || len(r.Chart.Result[0].Indicators.Quote) != 1 {
		return Chart{}, fmt.Errorf("unexpected chart shape: %.200s", body)
	}
	res := r.Chart.Result[0]
	// Yahoo quietly answers with coarser bars than asked for (range=max turns 1d
	// into 3mo); drawing those as the asked size would be wrong.
	if res.Meta.Granularity != interval {
		return Chart{}, fmt.Errorf("asked for %s bars, got %s", interval, res.Meta.Granularity)
	}
	q := res.Indicators.Quote[0]
	sessions := regularSessions(res.Meta.Sessions)
	c := Chart{Timezone: res.Meta.Timezone, Splits: len(res.Events.Splits), Bars: make([]Bar, 0, len(res.Timestamp))}
	j := 0
	for i, ts := range res.Timestamp {
		o, h, l, cl := at(q.Open, i), at(q.High, i), at(q.Low, i), at(q.Close, i)
		if o == nil || h == nil || l == nil || cl == nil {
			c.Skipped++
			continue
		}
		// After the close Yahoo appends the closing quote as one more intraday
		// bar opening right at the session's end, with no volume.
		if len(sessions) > 0 {
			for j < len(sessions) && sessions[j][1] <= ts {
				j++
			}
			if j == len(sessions) || ts < sessions[j][0] {
				c.Skipped++
				continue
			}
		}
		b := Bar{T: ts * 1000, O: *o, H: *h, L: *l, C: *cl}
		if v := at(q.Volume, i); v != nil {
			b.V = *v
		}
		c.Bars = append(c.Bars, b)
	}
	return c, nil
}

// regularSessions flattens tradingPeriods into [start, end) pairs, oldest first,
// or returns nil when the chart has none (daily and longer).
func regularSessions(raw json.RawMessage) [][2]int64 {
	var days [][]struct {
		Start int64 `json:"start"`
		End   int64 `json:"end"`
	}
	if json.Unmarshal(raw, &days) != nil {
		return nil
	}
	var s [][2]int64
	for _, d := range days {
		for _, p := range d {
			s = append(s, [2]int64{p.Start, p.End})
		}
	}
	slices.SortFunc(s, func(a, b [2]int64) int { return cmp.Compare(a[0], b[0]) })
	return s
}

func at(s []*float64, i int) *float64 {
	if i < len(s) {
		return s[i]
	}
	return nil
}
