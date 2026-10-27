package peertransport

import (
	"net/http"
	"strings"
)

// Handler advertises the same-port HTTP/3 listener on HTTPS only, offers an
// uncredentialed, side-effect-free capability probe, and removes HTTP/1-only
// response fields at the HTTP/3 boundary. Existing routing/auth stays in next.
func Handler(next http.Handler, port string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil {
			w.Header().Set("Alt-Svc", `h3=":`+port+`"; ma=300`)
		}
		if r.ProtoMajor == 3 {
			w = &h3Writer{ResponseWriter: w}
		}
		if r.URL.Path == ProbePath {
			if r.Method != http.MethodHead {
				w.Header().Set("Allow", "HEAD")
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set(CapabilityHeader, Capability)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type h3Writer struct {
	http.ResponseWriter
	wrote bool
}

func (w *h3Writer) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *h3Writer) WriteHeader(code int) {
	if !w.wrote {
		for _, v := range w.Header().Values("Connection") {
			for _, key := range strings.Split(v, ",") {
				w.Header().Del(strings.TrimSpace(key))
			}
		}
		for _, key := range []string{"Connection", "Keep-Alive", "Proxy-Connection", "Transfer-Encoding", "Upgrade"} {
			w.Header().Del(key)
		}
	}
	w.ResponseWriter.WriteHeader(code)
	if code >= 200 {
		w.wrote = true
	}
}
func (w *h3Writer) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
func (w *h3Writer) Flush() { _ = w.FlushError() }
func (w *h3Writer) FlushError() error {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}
