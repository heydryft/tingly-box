package protocolserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tingly-dev/tingly-box/ai"
	"github.com/tingly-dev/tingly-box/internal/client"
	"github.com/tingly-dev/tingly-box/internal/config"
	"github.com/tingly-dev/tingly-box/internal/loadbalance"
	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/routing"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

func TestCompactEndpointNativeForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const result = `{"object":"response.compaction", "output":[{"type":"message","role":"user","content":"retained"},{"type":"compaction","encrypted_content":"opaque-result","future":true}],"usage":{"input_tokens":9,"output_tokens":2}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/responses/compact", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, "native-model", gjson.GetBytes(body, "model").String())
		require.Equal(t, `[{"type":"reasoning","encrypted_content":"opaque-input","future":7}]`, gjson.GetBytes(body, "input").Raw)
		require.False(t, gjson.GetBytes(body, "stream").Exists())
		require.False(t, gjson.GetBytes(body, "store").Exists())
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, result)
	}))
	defer upstream.Close()
	// Exercise dual-URL resolution as well as the real routing/client stack.
	router := newDualProviderRouter(t, upstream.URL, protocol.APIStyleAnthropic, typ.ScenarioOpenAI, "compact-model", "native-model")
	for _, path := range []string{"/tingly/openai/v1/responses/compact", "/tingly/openai/responses/compact"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"compact-model", "input":[{"type":"reasoning","encrypted_content":"opaque-input","future":7}]}`))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, "application/json", w.Header().Get("Content-Type"))
		require.Equal(t, result, w.Body.String())
	}
}

// Compact has a JSON response contract, but TB must not impose its own
// stream-parameter policy on native upstreams.
func TestCompactEndpointStreamPassthrough(t *testing.T) {
	for _, value := range []string{"false", "true"} {
		t.Run(value, func(t *testing.T) {
			for _, status := range []int{http.StatusOK, http.StatusBadRequest} {
				var calls atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					body, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					require.Equal(t, value, gjson.GetBytes(body, "stream").Raw)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(status)
					if status == http.StatusOK {
						_, _ = io.WriteString(w, `{"object":"response.compaction","output":[]}`)
					} else {
						_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"upstream rejected stream"}}`)
					}
				}))
				router := newDualProviderRouter(t, upstream.URL, protocol.APIStyleOpenAI, typ.ScenarioOpenAI, "compact-model", "native-model")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/tingly/openai/v1/responses/compact", strings.NewReader(`{"model":"compact-model","stream":`+value+`}`)))
				upstream.Close()
				require.Equal(t, status, w.Code, w.Body.String())
				require.Equal(t, int32(1), calls.Load())
				if status == http.StatusBadRequest {
					require.Contains(t, w.Body.String(), "upstream rejected stream")
				}
			}
		})
	}
}

func TestCompactEndpointValidationAndUpstreamFailures(t *testing.T) {
	for _, status := range []int{400, 403, 429, 502} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":{"type":"internal_error","message":"response protection is unavailable"}}`)
			}))
			defer upstream.Close()
			router := newDualProviderRouter(t, upstream.URL, protocol.APIStyleOpenAI, typ.ScenarioOpenAI, "compact-model", "native-model")
			for _, bad := range []string{`{`, `[]`, `{}`, `{"model":3}`, `{"model":" "}`} {
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/tingly/openai/v1/responses/compact", strings.NewReader(bad)))
				require.Equal(t, 400, w.Code, w.Body.String())
			}
			require.Zero(t, calls.Load())
			// input is optional in the published schema (e.g. previous_response_id).
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/tingly/openai/v1/responses/compact", strings.NewReader(`{"model":"compact-model","previous_response_id":"resp_prior"}`)))
			require.Equal(t, status, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), "response protection is unavailable")
			require.Equal(t, int32(1), calls.Load())
		})
	}
}

func TestCompactEndpointRejectsNonNativeBackend(t *testing.T) {
	for _, style := range []protocol.APIStyle{protocol.APIStyleAnthropic, protocol.APIStyleOpenAI} {
		cfg, err := config.NewConfig(config.WithConfigDir(t.TempDir()), config.WithDisableMigration(), config.WithDisableBuiltIn())
		require.NoError(t, err)
		require.NoError(t, cfg.AddProvider(&typ.Provider{UUID: "compact-provider", Name: "compact-provider", Enabled: true, AuthType: typ.AuthTypeAPIKey, Token: "test", APIStyle: style, APIBase: "http://127.0.0.1:1/v1", OpenAIEndpointMode: ai.EndpointModeChat}))
		require.NoError(t, cfg.AddRule(typ.Rule{Scenario: typ.ScenarioOpenAI, RequestModel: "compact-model", Active: true, Services: []*loadbalance.Service{{Provider: "compact-provider", Model: "native-model", Active: true}}}))
		hm := loadbalance.NewHealthMonitor(loadbalance.HealthMonitorConfig{ProbeEnabled: false})
		lb := NewLoadBalancer(cfg, routing.NewHealthFilter(hm))
		ph := NewHandler(ProtocolHandlerDeps{Config: cfg, ClientPool: client.NewClientPool(), RoutingSelector: routing.NewSimpleSelector(routing.NewServiceSelector(cfg, NewAffinityStore(0), lb)), LoadBalancer: lb, HealthMonitor: hm})
		router := gin.New()
		ph.RegisterRoutes(router, func(c *gin.Context) { c.Next() })
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/tingly/openai/v1/responses/compact", strings.NewReader(`{"model":"compact-model","input":[]}`)))
		require.Equal(t, http.StatusNotImplemented, w.Code, w.Body.String())
		require.Equal(t, "unsupported_operation", gjson.Get(w.Body.String(), "error.code").String())
	}
}

func TestCompactEndpointAuthAndTeamScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(ProtocolHandlerDeps{}).RegisterRoutes(router, func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) })
	for _, path := range []string{"/tingly/openai/v1/responses/compact", "/tingly/openai/responses/compact", "/tingly/team/v1/responses/compact"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"m"}`)))
		require.Equal(t, http.StatusUnauthorized, w.Code)
	}
	router = gin.New()
	NewHandler(ProtocolHandlerDeps{}).RegisterRoutes(router, setTeamIDFromHeader)
	req := httptest.NewRequest(http.MethodPost, "/tingly/team/v1/responses/compact", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("X-Test-Team-ID", "team-a")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Contains(t, w.Body.String(), "not configured", "team-scoped request reaches rule resolution")
}
