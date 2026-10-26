package market

import "testing"

func TestPickProxy(t *testing.T) {
	for in, want := range map[string]string{
		"127.0.0.1:10810": "127.0.0.1:10810",
		" http=127.0.0.1:1;https=127.0.0.1:2;socks=127.0.0.1:3 ": "127.0.0.1:2",
		"http=127.0.0.1:1":  "127.0.0.1:1",
		"socks=127.0.0.1:3": "",
		"":                  "",
	} {
		if got := pickProxy(in); got != want {
			t.Errorf("pickProxy(%q) = %q, want %q", in, got, want)
		}
	}
}
