package market

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Cut down from the real list: a coin with its icon, one whose logoUrl is
// empty but fullLogoUrl is not, one with neither and one whose address is
// not https.
func TestParseIcons(t *testing.T) {
	body := `{"code":"000000","message":null,"messageDetail":null,"success":true,"data":[
		{"id":"1","assetCode":"BTC","assetName":"Bitcoin","logoUrl":"https://bin.bnbstatic.com/image/admin_mgs_image_upload/20201110/87496d50-2408-43e1-ad4c-78b47b448a6a.png","fullLogoUrl":"https://bin.bnbstatic.com/image/admin_mgs_image_upload/20201110/87496d50-2408-43e1-ad4c-78b47b448a6a.png","trading":true},
		{"id":"2","assetCode":"SUI","logoUrl":"","fullLogoUrl":"https://bin.bnbstatic.com/image/sui.jpg"},
		{"id":"3","assetCode":"NONE","logoUrl":"","fullLogoUrl":null},
		{"id":"4","assetCode":"PLAIN","logoUrl":"http://example.com/a.png"}
	]}`
	got, err := parseIcons([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !strings.HasSuffix(got["BTC"], "6a.png") || got["SUI"] != "https://bin.bnbstatic.com/image/sui.jpg" {
		t.Fatalf("got %v", got)
	}
	for _, bad := range []string{`{"success":false,"message":"busy","data":null}`, `{"success":true,"data":[]}`, `<html>`} {
		if _, err := parseIcons([]byte(bad)); err == nil {
			t.Fatalf("%s parsed", bad)
		}
	}
}

func TestIconRefusesWhatIsNoImage(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("\x89PNG"))
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html>"))
		case "/huge":
			w.Header().Set("Content-Type", "image/png")
			w.Write(make([]byte, maxIcon+1))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	b := Binance{HTTP: srv.Client()}
	ctx := context.Background()
	if data, err := b.Icon(ctx, srv.URL+"/ok.png"); err != nil || string(data) != "\x89PNG" {
		t.Fatalf("ok.png: %q, %v", data, err)
	}
	for _, path := range []string{"/page", "/huge", "/missing"} {
		if _, err := b.Icon(ctx, srv.URL+path); err == nil {
			t.Fatalf("%s: no error", path)
		}
	}
	if _, err := b.Icon(ctx, "http://example.com/a.png"); err == nil {
		t.Fatal("an http address was fetched")
	}
}
