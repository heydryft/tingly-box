package sameport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quic-go/quic-go/http3"
	"github.com/soheilhy/cmux"
	"github.com/tingly-dev/tingly-box/internal/peertransport"
	"github.com/tingly-dev/tingly-box/internal/protocol"
)

type unifiedTB struct {
	addr     string
	calls    atomic.Int32
	canceled chan struct{}
	stopQUIC func()
}

func unifiedBackend(t *testing.T, cert tls.Certificate) *unifiedTB {
	t.Helper()
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := tcp.Addr().String()
	_, port, _ := net.SplitHostPort(addr)
	udp, err := net.ListenPacket("udp", addr)
	if err != nil {
		tcp.Close()
		t.Fatal(err)
	}
	backend := &unifiedTB{addr: addr, canceled: make(chan struct{}, 1)}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/generate", func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer synthetic" {
			c.AbortWithStatus(401)
			return
		}
		backend.calls.Add(1)
		c.Header("X-Backend-Protocol", c.Request.Proto)
		c.Header("Content-Type", "text/event-stream")
		c.Header("Connection", "keep-alive")
		step := 0
		protocol.RunLoop(c, func(w io.Writer) bool {
			step++
			if step == 1 {
				io.WriteString(w, "data: hello\n\n")
				return true
			}
			if step == 2 {
				io.WriteString(w, "data: [DONE]\n\n")
				return true
			}
			return false
		})
	})
	router.POST("/cancel", func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer synthetic" {
			c.AbortWithStatus(401)
			return
		}
		backend.calls.Add(1)
		c.Header("Content-Type", "text/event-stream")
		c.Header("X-Backend-Protocol", c.Request.Proto)
		io.WriteString(c.Writer, "data: hello\n\n")
		c.Writer.Flush()
		<-c.Request.Context().Done()
		backend.canceled <- struct{}{}
	})
	handler := peertransport.Handler(router, port)
	mux := cmux.New(tcp)
	mux.SetReadTimeout(2 * time.Second)
	plain := mux.Match(cmux.HTTP1Fast())
	secure := mux.Match(cmux.TLS())
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	httpServer := &http.Server{Handler: handler, ReadHeaderTimeout: 2 * time.Second}
	tlsServer := &http.Server{Handler: handler, TLSConfig: tlsConfig.Clone(), ReadHeaderTimeout: 2 * time.Second}
	quicServer := &http3.Server{Handler: handler, TLSConfig: tlsConfig.Clone()}
	var wg sync.WaitGroup
	wg.Add(4)
	go func() { defer wg.Done(); _ = httpServer.Serve(plain) }()
	go func() { defer wg.Done(); _ = tlsServer.ServeTLS(secure, "", "") }()
	go func() { defer wg.Done(); _ = quicServer.Serve(udp) }()
	go func() { defer wg.Done(); _ = mux.Serve() }()
	var quicOnce sync.Once
	backend.stopQUIC = func() { quicOnce.Do(func() { _ = quicServer.Close(); _ = udp.Close() }) }
	t.Cleanup(func() {
		backend.stopQUIC()
		_ = httpServer.Close()
		_ = tlsServer.Close()
		mux.Close()
		_ = tcp.Close()
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("unified TB listeners did not close")
		}
	})
	return backend
}

func tcpTrusted(roots *x509.CertPool) *http.Client {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	base.TLSClientConfig = &tls.Config{RootCAs: roots}
	return &http.Client{Transport: base, Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func probeStream(client *http.Client, target string) (front, back string, status int, err error) {
	req, _ := http.NewRequest(http.MethodPost, target, strings.NewReader(`{"synthetic":true}`))
	req.Header.Set("Authorization", "Bearer synthetic")
	res, err := client.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err == nil && res.StatusCode == 200 && !strings.Contains(string(body), "[DONE]") {
		err = fmt.Errorf("incomplete SSE: %q", body)
	}
	return res.Proto, res.Header.Get("X-Backend-Protocol"), res.StatusCode, err
}

func TestCaddyAndTBEachUseOnePort(t *testing.T) {
	variants := []struct {
		name, upstreamProtocol  string
		tlsUpstream, h3Upstream bool
	}{
		{"HTTP upstream", "HTTP/1.1", false, false},
		{"HTTPS upstream", "HTTP/2.0", true, false},
		{"QUIC upstream", "HTTP/3.0", true, true},
	}
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			cert, roots, certFile, keyFile := signedFixture(t)
			backend := unifiedBackend(t, cert)
			direct := tcpTrusted(roots)
			defer direct.CloseIdleConnections()
			for _, scheme := range []string{"http", "https"} {
				front, back, status, err := probeStream(direct, scheme+"://"+backend.addr+"/generate")
				if err != nil || status != 200 || front != back {
					t.Fatalf("direct %s front=%s back=%s status=%d err=%v", scheme, front, back, status, err)
				}
				t.Logf("TB %s %s direct: %s", backend.addr, scheme, front)
			}
			quic := &http3.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}
			directH3 := &http.Client{Transport: quic, Timeout: 4 * time.Second}
			defer quic.Close()
			front, back, status, err := probeStream(directH3, "https://"+backend.addr+"/generate")
			if err != nil || status != 200 || front != "HTTP/3.0" || back != "HTTP/3.0" {
				t.Fatalf("direct H3: %s %s %d %v", front, back, status, err)
			}
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			addr := ln.Addr().String()
			ln.Close()
			var trust []string
			if variant.tlsUpstream {
				trust = []string{certFile}
			}
			startCaddyWith(t, addr, backend.addr, certFile, keyFile, []string{"h1", "h2", "h3"}, caddyTweaks{UpstreamHTTP3: variant.h3Upstream}, trust...)
			client, peer := enhanced(roots)
			defer peer.Close()
			// Caddy's public HTTPS and QUIC share one numbered TCP/UDP port.
			waitForH3(t, client, "https://"+addr+"/generate")
			front, back, status, err = probeStream(client, "https://"+addr+"/generate")
			if err != nil || status != 200 || front != "HTTP/3.0" || back != variant.upstreamProtocol {
				t.Fatalf("proxied front=%s upstream=%s status=%d err=%v", front, back, status, err)
			}
			t.Logf("client %s -> Caddy %s -> %s TB %s; complete SSE", front, addr, back, backend.addr)
			before := backend.calls.Load()
			_, _, status, err = probeStream(direct, "http://"+addr+"/generate")
			if err != nil || status != http.StatusBadRequest || backend.calls.Load() != before {
				t.Fatalf("plain HTTP on Caddy TLS port status=%d err=%v backendDelta=%d", status, err, backend.calls.Load()-before)
			}
			t.Log("Caddy standard TLS listener: same-port plaintext HTTP returns 400 and is not sent to TB")

			const n = 20
			before = backend.calls.Load()
			var wg sync.WaitGroup
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					front, back, status, err := probeStream(client, "https://"+addr+"/generate")
					if err != nil || status != 200 || front != "HTTP/3.0" || back != variant.upstreamProtocol {
						t.Errorf("mixed port proxy: %s/%s %d %v", front, back, status, err)
					}
				}()
			}
			wg.Wait()
			if backend.calls.Load()-before != n {
				t.Fatalf("business executions=%d want=%d", backend.calls.Load()-before, n)
			}
			t.Log("20 proxied streams completed with exactly 20 TB executions")

			ctx, cancel := context.WithCancel(context.Background())
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+addr+"/cancel", nil)
			req.Header.Set("Authorization", "Bearer synthetic")
			res, err := client.Do(req)
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			buf := make([]byte, len("data: hello\n\n"))
			_, err = io.ReadFull(res.Body, buf)
			cancel()
			res.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-backend.canceled:
			case <-time.After(3 * time.Second):
				t.Fatal("cancellation failed across the unified ports")
			}
			t.Log("cancellation propagated from client through Caddy to TB")
			if variant.h3Upstream {
				backend.stopQUIC()
				before = backend.calls.Load()
				ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
				request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+addr+"/generate", strings.NewReader("synthetic"))
				request.Header.Set("Authorization", "Bearer synthetic")
				response, err := client.Do(request)
				cancel()
				if response != nil {
					response.Body.Close()
				}
				if err == nil && response.StatusCode < 500 {
					t.Fatalf("forced QUIC upstream unexpectedly succeeded: %d", response.StatusCode)
				}
				if backend.calls.Load() != before {
					t.Fatal("forced QUIC upstream silently replayed on TCP")
				}
				for _, scheme := range []string{"http", "https"} {
					_, _, status, err := probeStream(direct, scheme+"://"+backend.addr+"/generate")
					if err != nil || status != 200 {
						t.Fatalf("TB TCP paths failed after UDP stopped: %s %d %v", scheme, status, err)
					}
				}
				t.Log("TB UDP stopped: configured Caddy HTTP/3-only upstream fails instead of auto-falling back; same-port TB HTTP/HTTPS remain healthy")
			}

		})
	}
}

func TestCaddySamePortHTTPRedirect(t *testing.T) {
	cert, roots, certFile, keyFile := signedFixture(t)
	backend := unifiedBackend(t, cert)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	startCaddyWith(t, addr, backend.addr, certFile, keyFile, []string{"h1", "h2", "h3"}, caddyTweaks{RedirectHTTP: true}, certFile)
	client := tcpTrusted(roots)
	defer client.CloseIdleConnections()
	before := backend.calls.Load()
	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/generate", strings.NewReader("synthetic"))
	req.Header.Set("Authorization", "Bearer synthetic")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusPermanentRedirect || res.Header.Get("Location") != "https://"+addr+"/generate" || backend.calls.Load() != before {
		t.Fatalf("redirect status=%d location=%s backendDelta=%d", res.StatusCode, res.Header.Get("Location"), backend.calls.Load()-before)
	}
	t.Logf("Caddy %s http_redirect wrapper: HTTP POST -> 308 to HTTPS on the SAME port; no backend execution yet", addr)
	client.CheckRedirect = nil
	front, back, status, err := probeStream(client, "http://"+addr+"/generate")
	if err != nil || status != 200 || backend.calls.Load() != before+1 {
		t.Fatalf("trusted redirect follow: %s/%s %d %v", front, back, status, err)
	}
	t.Log("trusted redirect-following client: POST and authorization preserved; exactly one TB execution")
	enhancedClient, peer := enhanced(roots)
	defer peer.Close()
	waitForH3(t, enhancedClient, "https://"+addr+"/generate")
	t.Log("same Caddy port continues to serve HTTPS and automatically discovered QUIC")
}
