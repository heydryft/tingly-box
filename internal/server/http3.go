package server

import (
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/sirupsen/logrus"
	"github.com/tingly-dev/tingly-box/internal/peertransport"
)

// HTTP3Options adds a separate HTTPS + QUIC endpoint without changing the
// existing plaintext listener. Addr is disabled when empty. TCP and UDP use
// the same address/port, and CertFile must be trusted by the connecting TB.
type HTTP3Options struct{ Addr, CertFile, KeyFile string }

func WithHTTP3(options HTTP3Options) ServerOption {
	return func(s *Server) { s.http3Options = options }
}

func (s *Server) startHTTP3() error {
	opts := s.http3Options
	if opts.Addr == "" {
		return nil
	}
	if opts.CertFile == "" || opts.KeyFile == "" {
		return fmt.Errorf("HTTP/3 requires both --http3-cert and --http3-key")
	}
	cert, err := tls.LoadX509KeyPair(opts.CertFile, opts.KeyFile)
	if err != nil {
		return fmt.Errorf("HTTP/3 certificate: %w", err)
	}
	tcp, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return fmt.Errorf("HTTP/3 HTTPS listener: %w", err)
	}
	_, port, err := net.SplitHostPort(tcp.Addr().String())
	if err != nil {
		tcp.Close()
		return err
	}
	udpAddr, err := net.ResolveUDPAddr("udp", tcp.Addr().String())
	if err != nil {
		tcp.Close()
		return err
	}
	udp, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		tcp.Close()
		return fmt.Errorf("HTTP/3 UDP listener: %w", err)
	}
	readTimeout := cmp.Or(s.httpTimeouts.ReadTimeout, 30*time.Second)
	writeTimeout := cmp.Or(s.httpTimeouts.WriteTimeout, 10*time.Minute)
	idleTimeout := cmp.Or(s.httpTimeouts.IdleTimeout, 120*time.Second)
	handler := peertransport.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 3 {
			// H3 has per-stream deadlines instead of http.Server's TCP
			// deadlines. AI routes clear these through the existing middleware;
			// management routes retain the same bounds as the original listener.
			rc := http.NewResponseController(w)
			_ = rc.SetReadDeadline(time.Now().Add(readTimeout))
			_ = rc.SetWriteDeadline(time.Now().Add(writeTimeout))
		}
		s.engine.ServeHTTP(w, r)
	}), port)
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
	tlsServer := &http.Server{
		Handler: handler, TLSConfig: tlsConfig,
		ReadHeaderTimeout: cmp.Or(s.httpTimeouts.ReadHeaderTimeout, 10*time.Second),
		ReadTimeout:       readTimeout, WriteTimeout: writeTimeout, IdleTimeout: idleTimeout,
	}
	n, _ := strconv.Atoi(port)
	h3Server := &http3.Server{Handler: handler, TLSConfig: tlsConfig.Clone(), Port: n, IdleTimeout: idleTimeout, QUICConfig: &quic.Config{MaxIdleTimeout: 120 * time.Second, KeepAlivePeriod: 15 * time.Second}}
	var once sync.Once
	var stopErr error
	s.http3Stop = func(ctx context.Context) error {
		once.Do(func() {
			done := make(chan error, 1)
			go func() { done <- h3Server.Shutdown(ctx) }()
			err1 := tlsServer.Shutdown(ctx)
			err2 := <-done
			_ = h3Server.Close()
			_ = udp.Close()
			_ = tlsServer.Close()
			stopErr = errors.Join(err1, err2)
		})
		return stopErr
	}
	go func() {
		if err := tlsServer.ServeTLS(tcp, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logrus.WithError(err).Error("HTTP/3 HTTPS endpoint stopped")
			_ = h3Server.Close()
			_ = udp.Close()
		}
	}()
	go func() {
		if err := h3Server.Serve(udp); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			logrus.WithError(err).Error("HTTP/3 endpoint stopped")
			_ = tlsServer.Close()
		}
	}()
	logrus.Infof("Optional HTTPS + HTTP/3 endpoint listening on %s (TCP and UDP)", tcp.Addr())
	return nil
}
