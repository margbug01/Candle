package market

import (
	"bufio"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// SystemProxy is an http.Transport Proxy that follows the macOS system proxy,
// the one Shoes, Clash and v2rayN switch on, as scutil --proxy shows it: an app
// opened from the Finder gets no proxy environment. It reads it again every few
// seconds, so turning the proxy on or off needs no restart. HTTPS_PROXY and
// HTTP_PROXY win when set.
//
// shortcut: ignores the exceptions list and PAC files, as on Windows.
func SystemProxy(req *http.Request) (*url.URL, error) {
	if os.Getenv("HTTPS_PROXY") != "" || os.Getenv("HTTP_PROXY") != "" {
		return http.ProxyFromEnvironment(req)
	}
	proxyMu.Lock()
	defer proxyMu.Unlock()
	if time.Since(proxyRead) > proxyFresh {
		proxyRead, proxyURL = time.Now(), readScutilProxy()
	}
	return proxyURL, nil
}

// proxyFresh is how long a reading of the system proxy is used: scutil takes
// a few milliseconds, and a chart asks for many pages in a row.
const proxyFresh = 3 * time.Second

var (
	proxyMu   sync.Mutex
	proxyRead time.Time
	proxyURL  *url.URL
)

func readScutilProxy() *url.URL {
	out, err := exec.Command("/usr/sbin/scutil", "--proxy").Output()
	if err != nil {
		return nil
	}
	return parseScutilProxy(string(out))
}

// parseScutilProxy picks the HTTPS proxy, else the HTTP one, else SOCKS,
// whichever is on.
func parseScutilProxy(text string) *url.URL {
	fields := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), " : "); ok {
			fields[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	for _, p := range []struct{ key, scheme string }{{"HTTPS", "http"}, {"HTTP", "http"}, {"SOCKS", "socks5"}} {
		host, port := fields[p.key+"Proxy"], fields[p.key+"Port"]
		if fields[p.key+"Enable"] != "1" || host == "" || port == "" {
			continue
		}
		return &url.URL{Scheme: p.scheme, Host: net.JoinHostPort(host, port)}
	}
	return nil
}
