package peertransport

import "net/http"

// ProbeHandler exposes a side-effect-free peer capability probe on the
// existing HTTP listener. TLS and HTTP/3 terminate at the deployment proxy.
func ProbeHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead || r.Header.Get(ProbeHeader) != Capability {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set(CapabilityHeader, Capability)
		w.WriteHeader(http.StatusNoContent)
	})
}
