package peertransport

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeHandlerPreservesHTTPRoutes(t *testing.T) {
	calls := 0
	handler := ProbeHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusAccepted)
	}))
	for _, tc := range []struct {
		method, path string
		status       int
		marker       string
	}{
		{http.MethodHead, ProbePath, http.StatusNoContent, Capability},
		{http.MethodGet, ProbePath, http.StatusMethodNotAllowed, ""},
		{http.MethodPost, "/v1/messages", http.StatusAccepted, ""},
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))
		if recorder.Code != tc.status || recorder.Header().Get(CapabilityHeader) != tc.marker {
			t.Fatalf("%s %s: status %d marker %q", tc.method, tc.path, recorder.Code, recorder.Header().Get(CapabilityHeader))
		}
	}
	if calls != 1 {
		t.Fatalf("model handler called %d times", calls)
	}
}
