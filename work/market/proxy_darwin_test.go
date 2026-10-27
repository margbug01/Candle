package market

import "testing"

// The HTTPS proxy comes first, then HTTP, then SOCKS; one turned off or
// missing its port does not count.
func TestParseScutilProxy(t *testing.T) {
	cases := []struct{ text, want string }{
		{"<dictionary> {\n  HTTPEnable : 1\n  HTTPPort : 8080\n  HTTPProxy : 10.0.0.2\n  HTTPSEnable : 1\n  HTTPSPort : 10808\n  HTTPSProxy : 127.0.0.1\n}", "http://127.0.0.1:10808"},
		{"<dictionary> {\n  HTTPEnable : 1\n  HTTPPort : 8080\n  HTTPProxy : 10.0.0.2\n  HTTPSEnable : 0\n  HTTPSPort : 10808\n  HTTPSProxy : 127.0.0.1\n}", "http://10.0.0.2:8080"},
		{"<dictionary> {\n  SOCKSEnable : 1\n  SOCKSPort : 1080\n  SOCKSProxy : ::1\n}", "socks5://[::1]:1080"},
		{"<dictionary> {\n  HTTPSEnable : 1\n  HTTPSProxy : 127.0.0.1\n}", ""},
		{"<dictionary> {\n  ExceptionsList : <array> {\n    0 : localhost\n  }\n  HTTPEnable : 0\n  SOCKSEnable : 0\n}", ""},
	}
	for _, c := range cases {
		got := ""
		if u := parseScutilProxy(c.text); u != nil {
			got = u.String()
		}
		if got != c.want {
			t.Errorf("got %q, want %q for\n%s", got, c.want, c.text)
		}
	}
}
