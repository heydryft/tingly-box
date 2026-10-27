package sameport

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
	"github.com/tingly-dev/tingly-box/internal/peertransport"
)

func signedFixture(t *testing.T) (tls.Certificate, *x509.CertPool, string, string) {
	t.Helper()
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	cert := fixture.TLS.Certificates[0]
	roots := x509.NewCertPool()
	roots.AddCert(fixture.Certificate())
	fixture.Close()
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "selfsigned.pem"), filepath.Join(dir, "key.pem")
	key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	return cert, roots, certFile, keyFile
}

func enhanced(roots *x509.CertPool) (*http.Client, *peertransport.Transport) {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	base.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	peer := peertransport.New(base)
	return &http.Client{Transport: peer, Timeout: 4 * time.Second}, peer
}

func streamRequest(client *http.Client, target string) (string, string, error) {
	req, _ := http.NewRequest(http.MethodPost, target, strings.NewReader(`{"synthetic":true}`))
	req.Header.Set("Authorization", "Bearer synthetic")
	res, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return res.Proto, string(body), err
	}
	if res.StatusCode != 200 {
		return res.Proto, string(body), fmt.Errorf("status=%d", res.StatusCode)
	}
	return res.Proto, string(body), nil
}

func waitForH3(t *testing.T, client *http.Client, target string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		proto, body, err := streamRequest(client, target)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, "[DONE]") {
			t.Fatal("incomplete stream")
		}
		if proto == "HTTP/3.0" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("H3 discovery timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSelfSignedHTTPSRequiresTrustForQUIC(t *testing.T) {
	cert, roots, _, _ := signedFixture(t)
	tcp := httptest.NewUnstartedServer(nil)
	_, port, _ := net.SplitHostPort(tcp.Listener.Addr().String())
	tcp.Config.Handler = peertransport.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: hello\n\ndata: [DONE]\n\n")
	}), port)
	tcp.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	tcp.StartTLS()
	defer tcp.Close()
	udp, err := net.ListenPacket("udp", tcp.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	h3 := &http3.Server{Handler: tcp.Config.Handler, TLSConfig: tcp.TLS.Clone()}
	go func() { _ = h3.Serve(udp) }()
	defer h3.Close()
	defer udp.Close()
	untrusted, untrustedPeer := enhanced(x509.NewCertPool())
	defer untrustedPeer.Close()
	if _, _, err := streamRequest(untrusted, tcp.URL+"/generate"); err == nil {
		t.Fatal("self-signed certificate was accepted without trust")
	}
	client, peer := enhanced(roots)
	defer peer.Close()
	first, _, err := streamRequest(client, tcp.URL+"/generate")
	if err != nil || first == "HTTP/3.0" {
		t.Fatalf("initial HTTPS proto=%s err=%v", first, err)
	}
	waitForH3(t, client, tcp.URL+"/generate")
	t.Logf("self-signed: untrusted rejected; explicit root trust accepted; %s -> HTTP/3.0; certificate verification remains enabled", first)
}

type caddyTweaks struct {
	UpstreamHTTP3 bool
	RedirectHTTP  bool
}

func startCaddy(t *testing.T, addr, backend, cert, key string, protocols []string, upstreamCA ...string) func() {
	t.Helper()
	return startCaddyWith(t, addr, backend, cert, key, protocols, caddyTweaks{}, upstreamCA...)
}

func startCaddyWith(t *testing.T, addr, backend, cert, key string, protocols []string, tweaks caddyTweaks, upstreamCA ...string) func() {
	t.Helper()
	binary := os.Getenv("CADDY_BIN")
	if binary == "" {
		t.Skip("set CADDY_BIN to run the optional Caddy integration experiments")
	}
	proxy := map[string]any{"handler": "reverse_proxy", "upstreams": []any{map[string]any{"dial": backend}}, "headers": map[string]any{"response": map[string]any{"delete": []string{"Alt-Svc"}}}}
	if len(upstreamCA) > 0 {
		tlsOptions := map[string]any{}
		if upstreamCA[0] != "" {
			tlsOptions["ca"] = map[string]any{"provider": "file", "pem_files": []string{upstreamCA[0]}}
		}
		proxy["transport"] = map[string]any{"protocol": "http", "tls": tlsOptions}
	}
	cfg := map[string]any{
		"admin":   map[string]any{"disabled": true},
		"logging": map[string]any{"logs": map[string]any{"default": map[string]any{"level": "ERROR"}}},
		"apps": map[string]any{
			"tls": map[string]any{"certificates": map[string]any{"load_files": []any{map[string]any{"certificate": cert, "key": key}}}},
			"http": map[string]any{"servers": map[string]any{"experiment": map[string]any{
				"listen": []string{addr}, "protocols": protocols, "tls_connection_policies": []any{map[string]any{}}, "automatic_https": map[string]any{"disable": true},
				"routes": []any{map[string]any{"handle": []any{proxy}}},
			}}},
		},
	}
	if tweaks.UpstreamHTTP3 {
		transport, ok := proxy["transport"].(map[string]any)
		if !ok {
			t.Fatal("HTTP/3 upstream requires configured TLS trust")
		}
		transport["versions"] = []string{"3"}
	}
	if tweaks.RedirectHTTP {
		apps := cfg["apps"].(map[string]any)
		httpApp := apps["http"].(map[string]any)
		servers := httpApp["servers"].(map[string]any)
		server := servers["experiment"].(map[string]any)
		server["listener_wrappers"] = []any{map[string]any{"wrapper": "http_redirect"}, map[string]any{"wrapper": "tls"}}
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "caddy.json")
	if err = os.WriteFile(config, raw, 0600); err != nil {
		t.Fatal(err)
	}
	logFile, err := os.Create(filepath.Join(dir, "caddy.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "run", "--config", config)
	cmd.Env = append(os.Environ(), "XDG_DATA_HOME="+dir, "XDG_CONFIG_HOME="+dir)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case <-done:
			case <-time.After(4 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
			logFile.Close()
		})
	}
	t.Cleanup(stop)
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return stop
		}
		select {
		case err := <-done:
			once.Do(func() { _ = logFile.Close() })
			data, _ := os.ReadFile(logFile.Name())
			t.Fatalf("Caddy exited: %v %s", err, data)
		default:
		}
		if time.Now().After(deadline) {
			stop()
			data, _ := os.ReadFile(logFile.Name())
			t.Fatalf("Caddy did not start: %s", data)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCaddySelfSignedProxyHTTP3StreamingAndFallback(t *testing.T) {
	_, roots, cert, key := signedFixture(t)
	var business atomic.Int32
	var backendNonHTTP1 atomic.Bool
	gate := make(chan struct{})
	canceled := make(chan struct{}, 1)
	backend := httptest.NewServer(peertransport.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		business.Add(1)
		if r.ProtoMajor != 1 || r.TLS != nil {
			backendNonHTTP1.Store(true)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Connection", "keep-alive")
		io.WriteString(w, "data: hello\n\n")
		w.(http.Flusher).Flush()
		switch r.URL.Path {
		case "/gated":
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		case "/cancel":
			<-r.Context().Done()
			canceled <- struct{}{}
			return
		}
		io.WriteString(w, "data: [DONE]\n\n")
		w.(http.Flusher).Flush()
	}), "0"))
	t.Cleanup(backend.Close)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	stop := startCaddy(t, addr, strings.TrimPrefix(backend.URL, "http://"), cert, key, []string{"h1", "h2", "h3"})
	target := "https://" + addr
	untrusted, bad := enhanced(x509.NewCertPool())
	defer bad.Close()
	if _, _, err := streamRequest(untrusted, target+"/generate"); err == nil {
		t.Fatal("Caddy self-signed leaf trusted without roots")
	}
	client, peer := enhanced(roots)
	defer peer.Close()
	first, _, err := streamRequest(client, target+"/generate")
	if err != nil || first == "HTTP/3.0" {
		t.Fatalf("first proto=%s err=%v", first, err)
	}
	waitForH3(t, client, target+"/generate")
	t.Logf("Caddy: self-signed validation enforced; first %s then HTTP/3.0; upstream is plaintext HTTP/1.1", first)

	req, _ := http.NewRequest(http.MethodPost, target+"/gated", nil)
	req.Header.Set("Authorization", "Bearer synthetic")
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(response.Body)
	line, err := reader.ReadString('\n')
	if err != nil || line != "data: hello\n" {
		t.Fatalf("first SSE before completion: %q %v", line, err)
	}
	close(gate)
	tail, err := io.ReadAll(reader)
	response.Body.Close()
	if err != nil || !strings.Contains(string(tail), "[DONE]") {
		t.Fatalf("terminal SSE=%q err=%v", tail, err)
	}
	t.Log("default Caddy reverse_proxy flushes SSE before backend completion; final marker preserved")

	ctx, cancel := context.WithCancel(context.Background())
	req, _ = http.NewRequestWithContext(ctx, http.MethodPost, target+"/cancel", nil)
	req.Header.Set("Authorization", "Bearer synthetic")
	response, err = client.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	reader = bufio.NewReader(response.Body)
	if _, err = reader.ReadString('\n'); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	response.Body.Close()
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("client cancellation did not propagate through Caddy")
	}
	t.Log("HTTP/3 client cancellation propagated through Caddy to HTTP backend")

	const count = 20
	before := business.Load()
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			proto, body, err := streamRequest(client, target+"/generate")
			if err != nil || proto != "HTTP/3.0" || !strings.Contains(body, "[DONE]") {
				t.Errorf("concurrent proto=%s err=%v", proto, err)
			}
		}()
	}
	wg.Wait()
	if business.Load()-before != count {
		t.Fatal("duplicate/lost business executions")
	}
	if backendNonHTTP1.Load() {
		t.Fatal("unexpected backend protocol")
	}
	t.Log("20 H3 proxy streams completed with exactly 20 HTTP/1.1 backend executions")

	stop()
	startCaddy(t, addr, strings.TrimPrefix(backend.URL, "http://"), cert, key, []string{"h1", "h2"})
	before = business.Load()
	if proto, _, err := streamRequest(client, target+"/generate"); err == nil {
		t.Fatalf("expected failed cached H3 path, got %s", proto)
	}
	if business.Load() != before {
		t.Fatal("failed H3 request was replayed to HTTP backend")
	}
	proto, body, err := streamRequest(client, target+"/generate")
	if err != nil || proto != "HTTP/2.0" || !strings.Contains(body, "[DONE]") {
		t.Fatalf("fallback proto=%s err=%v", proto, err)
	}
	t.Log("Caddy restarted with H3 disabled: failed request not replayed; next stream fell back to same-port HTTP/2")
}

func TestCaddyVerifiesSelfSignedHTTPSUpstream(t *testing.T) {
	_, roots, cert, key := signedFixture(t)
	var calls atomic.Int32
	backend := httptest.NewTLSServer(peertransport.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil {
			t.Error("backend connection lost TLS")
		}
		if r.Header.Get("Authorization") != "Bearer synthetic" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: hello\n\ndata: [DONE]\n\n")
		w.(http.Flusher).Flush()
	}), "9")) // A backend's Alt-Svc must not advertise its internal port to clients.
	t.Cleanup(backend.Close)
	backendCA := filepath.Join(t.TempDir(), "backend-ca.pem")
	if err := os.WriteFile(backendCA, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: backend.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	dial := strings.TrimPrefix(backend.URL, "https://")
	stop := startCaddy(t, addr, dial, cert, key, []string{"h1", "h2", "h3"}, "")
	client, peer := enhanced(roots)
	defer peer.Close()
	req, _ := http.NewRequest(http.MethodPost, "https://"+addr+"/generate", nil)
	req.Header.Set("Authorization", "Bearer synthetic")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadGateway || calls.Load() != 0 {
		t.Fatalf("untrusted backend not rejected: status=%d calls=%d", res.StatusCode, calls.Load())
	}
	stop()
	startCaddy(t, addr, dial, cert, key, []string{"h1", "h2", "h3"}, backendCA)
	trustedClient, trustedPeer := enhanced(roots)
	defer trustedPeer.Close()
	waitForH3(t, trustedClient, "https://"+addr+"/generate")
	if calls.Load() == 0 {
		t.Fatal("trusted HTTPS backend never invoked")
	}
	t.Log("self-signed HTTPS upstream: Caddy returned 502 without root trust; explicit CA trust enabled complete H3->Caddy->HTTPS streams")
	t.Log("backend internal Alt-Svc stripped; external Caddy port advertised; neither TLS segment uses insecure_skip_verify")
}
