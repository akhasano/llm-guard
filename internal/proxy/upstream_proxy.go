package proxy

import (
	"fmt"
	"net/url"
	"strconv"
)

// parseUpstreamProxy validates without including the URL in errors: it may
// contain credentials, including a password in url.Parse's error message.
func parseUpstreamProxy(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid upstream_proxy URL")
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("upstream_proxy scheme must be http, https, socks5, or socks5h")
	}
	if u.Hostname() == "" || u.Opaque != "" {
		return nil, fmt.Errorf("upstream_proxy URL must include a host")
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, fmt.Errorf("upstream_proxy URL must not include a path, query, or fragment")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("upstream_proxy port must be between 1 and 65535")
		}
	} else if u.Scheme == "socks5" || u.Scheme == "socks5h" {
		return nil, fmt.Errorf("upstream_proxy SOCKS5 URL must include a port")
	}
	if (u.Scheme == "socks5" || u.Scheme == "socks5h") && u.User != nil {
		password, _ := u.User.Password()
		if len(u.User.Username()) == 0 || len(u.User.Username()) > 255 || len(password) > 255 {
			return nil, fmt.Errorf("upstream_proxy SOCKS5 username must be 1–255 bytes and password at most 255 bytes")
		}
	}
	return u, nil
}
