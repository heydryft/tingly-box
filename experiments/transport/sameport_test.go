package sameport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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

func TestOriginalPortHTTPHTTPSAndQUIC(t *testing.T) {
	gin.SetMode(gin.TestMode)
	certificateFixture := httptest.NewTLSServer(http.NotFoundHandler())
	cert := certificateFixture.TLS.Certificates[0]
	roots := x509.NewCertPool()
	roots.AddCert(certificateFixture.Certificate())
	certificateFixture.Close()

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
	var plainCount, tlsCount, h3Count atomic.Int32
	router := gin.New()
	router.POST("/generate", func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer synthetic" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		if c.Request.ProtoMajor == 3 {
			h3Count.Add(1)
		} else if c.Request.TLS != nil {
			tlsCount.Add(1)
		} else {
			plainCount.Add(1)
		}
		c.Header("Content-Type", "text/event-stream")
		c.Header("Connection", "keep-alive")
		step := 0
		protocol.RunLoop(c, func(w io.Writer) bool {
			step++
			switch step {
			case 1:
				_, _ = io.WriteString(w, "data: hello\n\n")
				return true
			case 2:
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
				return true
			default:
				return false
			}
		})
	})
	handler := peertransport.Handler(router, port)
	mux := cmux.New(tcp)
	mux.SetReadTimeout(2 * time.Second)
	plainListener := mux.Match(cmux.HTTP1Fast())
	tlsListener := mux.Match(cmux.TLS())
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	plainServer := &http.Server{Handler: handler, ReadHeaderTimeout: 2 * time.Second}
	tlsServer := &http.Server{Handler: handler, TLSConfig: tlsConfig.Clone(), ReadHeaderTimeout: 2 * time.Second}
	h3Server := &http3.Server{Handler: handler, TLSConfig: tlsConfig.Clone()}
	var servers sync.WaitGroup
	servers.Add(4)
	go func() { defer servers.Done(); _ = plainServer.Serve(plainListener) }()
	go func() { defer servers.Done(); _ = tlsServer.ServeTLS(tlsListener, "", "") }()
	go func() { defer servers.Done(); _ = h3Server.Serve(udp) }()
	go func() { defer servers.Done(); _ = mux.Serve() }()
	t.Cleanup(func() {
		_ = plainServer.Close()
		_ = tlsServer.Close()
		_ = h3Server.Close()
		mux.Close()
		_ = tcp.Close()
		_ = udp.Close()
		done := make(chan struct{})
		go func() { servers.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("listeners did not shut down")
		}
	})

	oldTransport := &http.Transport{Proxy: nil}
	oldClient := &http.Client{Transport: oldTransport, Timeout: 3 * time.Second}
	defer oldTransport.CloseIdleConnections()
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	base.TLSClientConfig = &tls.Config{RootCAs: roots}
	peer := peertransport.New(base)
	defer peer.Close()
	enhancedClient := &http.Client{Transport: peer, Timeout: 3 * time.Second}

	request := func(client *http.Client, scheme string) (string, error) {
		req, _ := http.NewRequest(http.MethodPost, scheme+"://"+addr+"/generate", strings.NewReader(`{"synthetic":true}`))
		req.Header.Set("Authorization", "Bearer synthetic")
		response, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return "", err
		}
		if response.StatusCode != 200 || string(body) != "data: hello\n\ndata: [DONE]\n\n" {
			return "", fmt.Errorf("response status=%d body=%q", response.StatusCode, body)
		}
		return response.Proto, nil
	}
	check := func(client *http.Client, scheme, want string) {
		t.Helper()
		got, err := request(client, scheme)
		if err != nil || got != want {
			t.Fatalf("%s request proto=%s want=%s err=%v", scheme, got, want, err)
		}
	}
	check(oldClient, "http", "HTTP/1.1")
	tcpProtocol, err := request(enhancedClient, "https")
	if err != nil || (tcpProtocol != "HTTP/1.1" && tcpProtocol != "HTTP/2.0") {
		t.Fatalf("first HTTPS path=%s err=%v", tcpProtocol, err)
	}
	t.Logf("initial HTTPS TCP protocol: %s", tcpProtocol)
	// Only the HEAD capability probe selects the new path; no generation is raced.
	deadline := time.Now().Add(3 * time.Second)
	for {
		got, err := request(enhancedClient, "https")
		if err != nil {
			t.Fatal(err)
		}
		if got == "HTTP/3.0" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("automatic QUIC selection did not happen")
		}
		time.Sleep(5 * time.Millisecond)
	}
	check(oldClient, "http", "HTTP/1.1")
	t.Logf("same %s: legacy HTTP works; HTTPS discovery succeeded; subsequent stream HTTP/3.0; legacy HTTP still works", addr)

	// Mixed clients continue to receive complete streams concurrently on the
	// original numeric port. Failed calls must not be hidden by retries.
	const concurrent = 20
	before := plainCount.Load() + tlsCount.Load() + h3Count.Load()
	var wg sync.WaitGroup
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			client, scheme, want := enhancedClient, "https", "HTTP/3.0"
			if i%2 == 0 {
				client, scheme, want = oldClient, "http", "HTTP/1.1"
			}
			got, err := request(client, scheme)
			if err != nil || got != want {
				t.Errorf("mixed request proto=%s want=%s err=%v", got, want, err)
			}
		}(i)
	}
	wg.Wait()
	if after := plainCount.Load() + tlsCount.Load() + h3Count.Load(); after-before != concurrent {
		t.Fatalf("business execution count delta=%d want=%d", after-before, concurrent)
	}
	t.Log("20 mixed HTTP/QUIC concurrent streams completed; exactly 20 business executions")

	// Stop only UDP/H3. An already selected H3 request reports its failure;
	// the next request returns to HTTPS/TCP on exactly the same port.
	_ = h3Server.Close()
	_ = udp.Close()
	before = plainCount.Load() + tlsCount.Load() + h3Count.Load()
	if got, err := request(enhancedClient, "https"); err == nil {
		t.Fatalf("expected H3 failure, got %s", got)
	}
	if after := plainCount.Load() + tlsCount.Load() + h3Count.Load(); after != before {
		t.Fatal("failed H3 request was replayed as a business request")
	}
	check(enhancedClient, "https", tcpProtocol)
	check(oldClient, "http", "HTTP/1.1")
	t.Log("UDP stopped: H3 failure surfaced without TCP replay; next HTTPS request fell back; legacy HTTP unaffected")

	// Verify original TCP route retains authentication after mixing protocols.
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://"+addr+"/generate", nil)
	res, err := oldClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatal("authentication bypassed")
	}
}
