package protocolserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/require"
	"github.com/tingly-dev/tingly-box/ai"
	"github.com/tingly-dev/tingly-box/internal/config"
	"github.com/tingly-dev/tingly-box/internal/loadbalance"
	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/protocol/stream"
	"github.com/tingly-dev/tingly-box/internal/routing"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

func TestWorkBuddyFailureRouting(t *testing.T) {
	for _, tc := range []struct {
		name, kind, message string
		status              int
		wantCalls           []string
		restricted          bool
	}{
		{"channel", protocol.FailureChannelRefused, "unapproved channel", 400, []string{"wb", "other"}, false},
		{"restricted", protocol.FailureAccountRestricted, "account restricted", 403, []string{"wb", "wb-sibling"}, true},
		{"shape", protocol.FailureInvalidRequest, "first message is not system prompt", 400, []string{"wb"}, false},
		{"unrelated numeric code", "", "11128", 400, []string{"wb"}, false},
	} {
		for _, streaming := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/complete", true: "/stream"}[streaming], func(t *testing.T) {
				loadbalance.DefaultBreakerStore().Reset()
				defer loadbalance.DefaultBreakerStore().Reset()
				cfg, err := config.NewConfig(config.WithConfigDir(t.TempDir()))
				require.NoError(t, err)
				wb := &typ.Provider{UUID: "wb", Name: "WorkBuddy", Enabled: true, APIStyle: "openai", APIBase: "https://example.invalid/v2", AuthType: typ.AuthTypeOAuth, OAuthDetail: &typ.OAuthDetail{Issuer: ai.IssuerWorkBuddyCN}}
				for _, p := range []*typ.Provider{wb, {UUID: "wb-sibling", Name: "Sibling", Enabled: true, APIStyle: "openai", APIBase: "https://example.invalid/v2", AuthType: typ.AuthTypeOAuth, OAuthDetail: &typ.OAuthDetail{Issuer: ai.IssuerWorkBuddy}}, {UUID: "other", Name: "Other", Enabled: true, APIStyle: "openai", APIBase: "https://example.invalid/v1"}} {
					require.NoError(t, cfg.AddProvider(p))
				}
				rule := &typ.Rule{UUID: "workbuddy-test", LBTactic: typ.Tactic{Type: loadbalance.TacticTier, Params: typ.DefaultTierParams()}, Services: []*loadbalance.Service{
					{Provider: "wb", Model: "primary", Active: true, Tier: 0}, {Provider: "wb", Model: "other-model", Active: true, Tier: 1}, {Provider: "wb-sibling", Model: "m", Active: true, Tier: 2}, {Provider: "other", Model: "m", Active: true, Tier: 3},
				}}
				hm := loadbalance.NewHealthMonitor(loadbalance.DefaultHealthMonitorConfig())
				h := NewHandler(ProtocolHandlerDeps{Config: cfg, LoadBalancer: NewLoadBalancer(cfg, routing.NewHealthFilter(hm)), HealthMonitor: hm})
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
				var calls []string
				h.DispatchWithPriorityFailover(c, rule, wb, "primary", func(p *typ.Provider, model string) {
					calls = append(calls, p.UUID)
					if p.UUID == "wb" {
						err := &openai.Error{StatusCode: tc.status, Type: tc.kind, Code: "11128", Message: tc.message}
						if streaming {
							stream.SendStreamingError(c, err)
						} else {
							SendErrorResponse(c, err, "Failed to forward request")
						}
						return
					}
					c.String(200, "ok")
					CommitFirstChunkIfGate(c.Writer)
				})
				require.Equal(t, tc.wantCalls, calls)
				require.Equal(t, tc.restricted, hm.IsProviderRestricted("wb"))
				if len(calls) > 1 {
					require.Equal(t, "ok", rec.Body.String())
				} else {
					require.Equal(t, tc.status, rec.Code)
				}
				require.False(t, hm.GetHealth("wb/primary").AuthError)
				// Neither content rejection nor account restriction is a transient
				// server failure. Accepted channels keep this service available.
				require.Equal(t, loadbalance.BreakerClosed, loadbalance.DefaultBreakerStore().Get(rule.UUID, "wb/primary").State())
				if !tc.restricted {
					require.True(t, hm.IsHealthy("wb/primary"))
				}
			})
		}
	}
}

func TestWorkBuddySingleServiceRestriction(t *testing.T) {
	hm := loadbalance.NewHealthMonitor(loadbalance.DefaultHealthMonitorConfig())
	h := NewHandler(ProtocolHandlerDeps{HealthMonitor: hm})
	h.ReportHealthStatus(&typ.Provider{UUID: "wb"}, "m", &openai.Error{StatusCode: http.StatusForbidden, Type: protocol.FailureAccountRestricted, Code: "11140"}, "")
	require.False(t, hm.IsHealthy("wb/another/model"))
	require.Equal(t, protocol.FailureAccountRestricted, classifyErrorCode(&openai.Error{StatusCode: 403, Type: protocol.FailureAccountRestricted}))
}
