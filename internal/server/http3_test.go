package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quic-go/quic-go/http3"
	"github.com/tingly-dev/tingly-box/internal/middleware"
	"github.com/tingly-dev/tingly-box/internal/peertransport"
)

func TestOptionalHTTP3ListenerUsesExistingHandler(t *testing.T) {
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	cert := fixture.TLS.Certificates[0]
	key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	root := fixture.Certificate()
	fixture.Close()
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()
	router := gin.New()
	router.GET("/private", func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer synthetic" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Header("Connection", "keep-alive")
		c.String(200, "authorized")
	})
	stream := func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		_, _ = io.WriteString(c.Writer, "data: first\n\n")
		c.Writer.Flush()
		time.Sleep(300 * time.Millisecond)
		_, _ = io.WriteString(c.Writer, "data: completed\n\n")
		c.Writer.Flush()
	}
	router.GET("/bounded", stream)
	router.GET("/stream", middleware.ClearServerIOTimeouts(), stream)
	s := &Server{engine: router, http3Options: HTTP3Options{Addr: addr, CertFile: certFile, KeyFile: keyFile}, httpTimeouts: HTTPTimeouts{WriteTimeout: 200 * time.Millisecond}}
	if err = s.startHTTP3(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.http3Stop(ctx)
	})
	roots := x509.NewCertPool()
	roots.AddCert(root)
	h3 := &http3.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}
	defer h3.Close()
	tcp := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}
	defer tcp.CloseIdleConnections()
	for _, transport := range []http.RoundTripper{tcp, h3} {
		client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
		req, _ := http.NewRequest(http.MethodHead, "https://"+addr+peertransport.ProbePath, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 204 || resp.Header.Get(peertransport.CapabilityHeader) != peertransport.Capability {
			t.Fatal("probe contract failed")
		}
		resp, err = client.Get("https://" + addr + "/private")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatalf("authentication bypassed over %s", resp.Proto)
		}
		req, _ = http.NewRequest(http.MethodGet, "https://"+addr+"/private", nil)
		req.Header.Set("Authorization", "Bearer synthetic")
		resp, err = client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || string(body) != "authorized" {
			t.Fatalf("handler failed: %q %v", body, err)
		}
		if transport == h3 && (resp.ProtoMajor != 3 || resp.Header.Get("Connection") != "") {
			t.Fatalf("H3 wire invalid: %s", resp.Proto)
		}
		for _, path := range []string{"/bounded", "/stream"} {
			resp, err := client.Get("https://" + addr + path)
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if path == "/stream" {
				if readErr != nil || !strings.Contains(string(body), "data: completed") {
					t.Fatalf("AI stream truncated over %s: %q %v", resp.Proto, body, readErr)
				}
			} else if strings.Contains(string(body), "data: completed") {
				t.Fatalf("ordinary route lost its write deadline over %s", resp.Proto)
			}
		}
	}
}

func TestHTTP3IsOptionalAndRejectsIncompleteConfiguration(t *testing.T) {
	s := &Server{}
	if err := s.startHTTP3(); err != nil || s.http3Stop != nil {
		t.Fatal("default unexpectedly starts H3")
	}
	s.http3Options = HTTP3Options{Addr: "127.0.0.1:0"}
	if err := s.startHTTP3(); err == nil {
		t.Fatal("missing TLS certificate accepted")
	}
}
