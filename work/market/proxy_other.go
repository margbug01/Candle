//go:build !windows && !darwin

package market

import "net/http"

// SystemProxy follows HTTPS_PROXY and HTTP_PROXY; there is no system proxy
// to read here.
var SystemProxy = http.ProxyFromEnvironment
