package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Binance's website lists every coin with its icon; the market-data mirror
// has neither. Like binance.com itself, it needs a proxy from mainland China:
// without one there are no icons, and the rest works as before.
const binanceAssets = "https://www.binance.com/bapi/asset/v2/public/asset/asset/get-all-asset"

// maxIcon bounds an icon's download: they are a few KB.
const maxIcon = 1 << 20

// Icons maps each coin Binance lists, as "BTC", to its icon's address. The
// list is about 100 KB compressed.
func (b Binance) Icons(ctx context.Context) (map[string]string, error) {
	body, err := get(ctx, b.HTTP, binanceAssets)
	if err != nil {
		return nil, fmt.Errorf("binance icons: %w", err)
	}
	return parseIcons(body)
}

func parseIcons(body []byte) (map[string]string, error) {
	var r struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    []struct {
			Code string `json:"assetCode"`
			Logo string `json:"logoUrl"`
			Full string `json:"fullLogoUrl"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("binance icons: %w", err)
	}
	if !r.Success || len(r.Data) == 0 {
		return nil, fmt.Errorf("binance icons: %d coins, %q", len(r.Data), r.Message)
	}
	out := make(map[string]string, len(r.Data))
	for _, d := range r.Data {
		u := d.Logo
		if u == "" {
			u = d.Full
		}
		if d.Code != "" && iconURL(u) {
			out[d.Code] = u
		}
	}
	return out, nil
}

// iconURL is whether u is an address to fetch an icon from: https, with a host.
func iconURL(u string) bool {
	p, err := url.Parse(u)
	return err == nil && p.Scheme == "https" && p.Host != ""
}

// Icon downloads an icon from an address Icons gave: PNG, JPEG or WebP.
func (b Binance) Icon(ctx context.Context, u string) ([]byte, error) {
	if !iconURL(u) {
		return nil, fmt.Errorf("icon %q: not an https address", u)
	}
	c := b.HTTP
	if c == nil {
		c = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	res, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("icon: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("icon %s: %s", u, res.Status)
	}
	if ct := res.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "image/") && !strings.HasPrefix(ct, "application/octet-stream") {
		return nil, fmt.Errorf("icon %s: %s", u, ct)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxIcon+1))
	if err != nil {
		return nil, fmt.Errorf("icon: %w", err)
	}
	if len(data) > maxIcon {
		return nil, fmt.Errorf("icon %s: over %d bytes", u, maxIcon)
	}
	return data, nil
}
