package peertransport

import "net/http"

// ProbeHandler exposes a side-effect-free peer capability probe on the
// existing HTTP listener. TLS and HTTP/3 terminate at the deployment proxy.
func ProbeHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ProbePath {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodHead {
			w.Header().Set("Allow", "HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(CapabilityHeader, Capability)
		w.WriteHeader(http.StatusNoContent)
	})
}
