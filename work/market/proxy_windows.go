package market

import (
	"net/http"
	"net/url"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// SystemProxy is an http.Transport Proxy that follows the Windows system proxy,
// the one Clash, v2rayN and Shoes switch on. It reads the registry on every new
// connection, so turning the proxy on or off needs no restart. HTTPS_PROXY and
// HTTP_PROXY win when set.
//
// shortcut: ignores ProxyOverride (the bypass list) and PAC scripts, revisit when
// a quote host sits on someone's bypass list or their tool only publishes a PAC.
func SystemProxy(req *http.Request) (*url.URL, error) {
	if os.Getenv("HTTPS_PROXY") != "" || os.Getenv("HTTP_PROXY") != "" {
		return http.ProxyFromEnvironment(req)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return nil, nil
	}
	defer k.Close()
	if on, _, _ := k.GetIntegerValue("ProxyEnable"); on == 0 {
		return nil, nil
	}
	server, _, _ := k.GetStringValue("ProxyServer")
	if addr := pickProxy(server); addr != "" {
		return &url.URL{Scheme: "http", Host: addr}, nil
	}
	return nil, nil
}

// pickProxy reads ProxyServer, which is either "host:port" or "http=a:1;https=b:2;socks=c:3".
func pickProxy(server string) string {
	server = strings.TrimSpace(server)
	if !strings.Contains(server, "=") {
		return server
	}
	entries := map[string]string{}
	for _, part := range strings.Split(server, ";") {
		if k, v, ok := strings.Cut(part, "="); ok {
			entries[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
	}
	if v := entries["https"]; v != "" {
		return v
	}
	return entries["http"]
}
