// Package market turns quote and exchange APIs into one candle shape.
package market

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// Bar is one candle. T is the open time in Unix milliseconds.
type Bar struct {
	T          int64
	O, H, L, C float64
	V          float64
}

func get(ctx context.Context, c *http.Client, u string) ([]byte, error) {
	if c == nil {
		c = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	// Yahoo answers 429 to curl's and Go's default user agents.
	req.Header.Set("User-Agent", "Mozilla/5.0")
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %.300s", res.Status, body)
	}
	return body, nil
}
