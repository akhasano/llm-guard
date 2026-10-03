package proxy

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseUpstreamProxy(t *testing.T) {
	for _, raw := range []string{"", "http://localhost:3128", "https://user:p%40ss@localhost", "socks5://localhost:1080", "socks5h://user:pass@[::1]:1080"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := parseUpstreamProxy(raw); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, raw := range []string{"localhost:3128", "ftp://localhost:21", "http://", "socks5://localhost", "http://localhost:0", "http://localhost:65536", "http://localhost:bad", "http://localhost/path", "http://localhost?x=1", "http://localhost#fragment", "socks5://:password@localhost:1080", "http://user:private-password@localhost:%xx"} {
		t.Run(raw, func(t *testing.T) {
			_, err := New("https://api.example.com", newTestRedactor(t), nil, Options{UpstreamProxy: raw})
			if err == nil {
				t.Fatal("expected invalid proxy URL error")
			}
			if strings.Contains(err.Error(), "private-password") {
				t.Fatal("error exposes proxy credentials")
			}
		})
	}
}

func proxyRequest(t *testing.T, p *Proxy) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r := httptest.NewRequest(http.MethodPost, "/v1/chat?stream=1", strings.NewReader(`{"text":"AKIAIOSFODNN7EXAMPLE"}`)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer provider-key")
	r.Header.Set("Proxy-Authorization", "Basic client-credentials")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	return w
}

func TestProxy_HTTPOutboundProxy(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(fmt.Sprintf("TLS=%v", secure), func(t *testing.T) {
			outbound := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.String() != "http://provider.invalid/v1/chat?stream=1" {
					t.Errorf("unexpected proxy target: %s", r.URL)
				}
				wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:p@ss"))
				if r.Header.Get("Proxy-Authorization") != wantAuth {
					t.Error("missing configured proxy authentication")
				}
				if r.Header.Get("Authorization") != "Bearer provider-key" {
					t.Error("provider authorization changed")
				}
				body, _ := io.ReadAll(r.Body)
				if strings.Contains(string(body), "AKIAIOSFODNN7EXAMPLE") || !strings.Contains(string(body), "⟦RG:") {
					t.Error("outbound proxy received unredacted body")
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write(body)
			}))
			if secure {
				outbound.StartTLS()
			} else {
				outbound.Start()
			}
			defer outbound.Close()
			proxyURL := strings.Replace(outbound.URL, "://", "://user:p%40ss@", 1)
			p, err := New("http://provider.invalid", newTestRedactor(t), nil, Options{UpstreamProxy: proxyURL})
			if err != nil {
				t.Fatal(err)
			}
			transport := p.client.Transport.(*http.Transport)
			if secure {
				transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // Test server certificate only.
			}
			defer transport.CloseIdleConnections()
			w := proxyRequest(t, p)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "AKIAIOSFODNN7EXAMPLE") {
				t.Fatalf("response = %d %s", w.Code, w.Body)
			}
		})
	}
}

func relay(conn, target net.Conn) {
	defer conn.Close()
	defer target.Close()
	go func() {
		io.Copy(target, conn)
		target.Close()
	}()
	io.Copy(conn, target)
}

func TestProxy_HTTPConnect(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials leaked to provider")
		}
		w.Header().Set("Content-Type", "application/json")
		io.Copy(w, r.Body)
	}))
	defer upstream.Close()
	outbound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != strings.TrimPrefix(upstream.URL, "https://") {
			t.Error("expected CONNECT to upstream")
			http.Error(w, "bad CONNECT", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("user:pass")) {
			t.Error("CONNECT missing proxy authentication")
		}
		target, err := net.Dial("tcp", r.Host)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			target.Close()
			return
		}
		rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		rw.Flush()
		relay(conn, target)
	}))
	defer outbound.Close()
	p, err := New(upstream.URL, newTestRedactor(t), nil, Options{UpstreamProxy: strings.Replace(outbound.URL, "://", "://user:pass@", 1)})
	if err != nil {
		t.Fatal(err)
	}
	transport := p.client.Transport.(*http.Transport)
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // Test server certificate only.
	defer transport.CloseIdleConnections()
	w := proxyRequest(t, p)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("response = %d %s", w.Code, w.Body)
	}
}

// A minimal SOCKS5 test server verifies remote DNS and RFC 1929 credentials,
// then routes the requested fake hostname to our local upstream.
func serveSOCKS(conn net.Conn, targetAddr string, auth bool) error {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	read := func(n int) ([]byte, error) {
		b := make([]byte, n)
		_, err := io.ReadFull(conn, b)
		return b, err
	}
	header, err := read(2)
	if err != nil {
		return err
	}
	if header[0] != 5 {
		return fmt.Errorf("not SOCKS5")
	}
	if _, err := read(int(header[1])); err != nil {
		return err
	}
	method := byte(0)
	if auth {
		method = 2
	}
	conn.Write([]byte{5, method})
	if auth {
		h, err := read(2)
		if err != nil {
			return err
		}
		user, err := read(int(h[1]))
		if err != nil {
			return err
		}
		n, err := read(1)
		if err != nil {
			return err
		}
		pass, err := read(int(n[0]))
		if err != nil {
			return err
		}
		if h[0] != 1 || string(user) != "user" || string(pass) != "p@ss" {
			return fmt.Errorf("invalid SOCKS authentication")
		}
		conn.Write([]byte{1, 0})
	}
	h, err := read(5)
	if err != nil {
		return err
	}
	if h[0] != 5 || h[1] != 1 || h[3] != 3 {
		return fmt.Errorf("expected SOCKS CONNECT with remote hostname")
	}
	host, err := read(int(h[4]))
	if err != nil {
		return err
	}
	port, err := read(2)
	if err != nil {
		return err
	}
	if string(host) != "provider.invalid" || binary.BigEndian.Uint16(port) != 80 {
		return fmt.Errorf("unexpected SOCKS destination: %s:%d", host, binary.BigEndian.Uint16(port))
	}
	target, err := net.Dial("tcp", targetAddr)
	if err != nil {
		return err
	}
	conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80})
	relay(conn, target)
	return nil
}

func TestProxy_SOCKS5(t *testing.T) {
	for _, scheme := range []string{"socks5", "socks5h"} {
		for _, auth := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/auth=%v", scheme, auth), func(t *testing.T) {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					if strings.Contains(string(body), "AKIAIOSFODNN7EXAMPLE") || !strings.Contains(string(body), "⟦RG:") {
						t.Error("SOCKS upstream received unredacted body")
					}
					if r.Header.Get("Proxy-Authorization") != "" {
						t.Error("proxy credentials leaked to provider")
					}
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "data: %s\n\n", body)
					w.(http.Flusher).Flush()
				}))
				defer upstream.Close()
				ln, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer ln.Close()
				done := make(chan error, 1)
				go func() {
					conn, err := ln.Accept()
					if err == nil {
						err = serveSOCKS(conn, strings.TrimPrefix(upstream.URL, "http://"), auth)
					}
					done <- err
				}()
				credentials := ""
				if auth {
					credentials = "user:p%40ss@"
				}
				p, err := New("http://provider.invalid", newTestRedactor(t), nil, Options{UpstreamProxy: scheme + "://" + credentials + ln.Addr().String()})
				if err != nil {
					t.Fatal(err)
				}
				w := proxyRequest(t, p)
				p.client.CloseIdleConnections()
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "AKIAIOSFODNN7EXAMPLE") || !strings.HasPrefix(w.Body.String(), "data: ") {
					t.Fatalf("response = %d %s", w.Code, w.Body)
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestProxy_NoDirectFallback(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected direct request bypassing proxy")
	}))
	defer upstream.Close()
	for _, scheme := range []string{"http", "socks5"} {
		p, err := New(upstream.URL, newTestRedactor(t), nil, Options{UpstreamProxy: scheme + "://127.0.0.1:0"})
		// Port zero is rejected before any requests can bypass the proxy.
		if err == nil || p != nil {
			t.Fatal("expected invalid port to fail at startup")
		}
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := ln.Addr().String()
		ln.Close()
		p, err = New(upstream.URL, newTestRedactor(t), nil, Options{UpstreamProxy: scheme + "://" + addr})
		if err != nil {
			t.Fatal(err)
		}
		if w := proxyRequest(t, p); w.Code != http.StatusBadGateway {
			t.Fatalf("response = %d, want 502", w.Code)
		}
	}
}

func TestProxy_EmptyProxyIgnoresEnvironment(t *testing.T) {
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(key, "http://127.0.0.1:1")
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("incoming proxy credentials leaked to provider")
		}
		w.Header().Set("Content-Type", "application/json")
		io.Copy(w, r.Body)
	}))
	defer upstream.Close()
	p, err := New(upstream.URL, newTestRedactor(t), nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.client.CloseIdleConnections()
	if p.client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("empty upstream_proxy must disable environment proxy lookup")
	}
	w := proxyRequest(t, p)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("response = %d %s", w.Code, w.Body)
	}
}
