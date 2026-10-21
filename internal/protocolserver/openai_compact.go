package protocolserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/openai/openai-go/v3/responses"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"github.com/tingly-dev/tingly-box/ai"
	"github.com/tingly-dev/tingly-box/internal/forwarding"
	"github.com/tingly-dev/tingly-box/internal/protocol"
	protocolusage "github.com/tingly-dev/tingly-box/internal/protocol/usage"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// HandleResponsesCompact serves the native, non-streaming compact endpoint.
// Read only model for routing; preserve opaque input and output window items.
// This endpoint cannot be downgraded to a chat summary on another API style.
func (ph *ProtocolHandler) HandleResponsesCompact(c *gin.Context) {
	scenario := typ.RuleScenario(c.Param("scenario"))
	if !IsValidRuleScenario(scenario) || !typ.ScenarioSupportsTransport(scenario, typ.TransportOpenAI) {
		compactBadRequest(c, fmt.Sprintf("scenario %s does not support OpenAI compaction", scenario))
		return
	}
	body, err := c.GetRawData()
	if err != nil {
		compactBadRequest(c, "Failed to read request body: "+err.Error())
		return
	}
	requestModel, err := compactRequestModel(body)
	if err != nil {
		compactBadRequest(c, err.Error())
		return
	}
	rule, err := ph.determineRuleWithScenario(c, scenario, requestModel)
	if err != nil {
		compactBadRequest(c, err.Error())
		return
	}
	// Select one service without classifying opaque history as a chat prompt.
	provider, service, err := ph.selectServiceForEmbeddings(c, scenario, rule)
	if err != nil {
		compactBadRequest(c, err.Error())
		return
	}
	provider = provider.ResolveStyle(protocol.APIStyleOpenAI)
	if provider.APIStyle != protocol.APIStyleOpenAI || (provider.OpenAIEndpointMode == ai.EndpointModeChat && !provider.IsCodexProvider()) {
		unsupportedResponsesCompaction(c)
		return
	}
	upstreamBody, err := sjson.SetBytes(body, "model", service.Model)
	if err != nil {
		compactBadRequest(c, "Invalid request body: "+err.Error())
		return
	}
	c.Request = c.Request.WithContext(typ.WithSessionID(c.Request.Context(), resolveSessionID(c, nil)))
	SetTrackingContext(c, rule, provider, service.Model, requestModel, false)
	wrapper := ph.deps.ClientPool.GetOpenAIClient(c.Request.Context(), provider, service.Model)
	fc := forwarding.NewForwardContext(c.Request.Context(), provider)
	result, cancel, err := forwarding.ForwardOpenAIResponsesCompact(fc, wrapper, upstreamBody)
	if cancel != nil {
		defer cancel()
	}
	if err != nil {
		ph.failForward(c, err)
		return
	}
	var usage responses.ResponseUsage
	_ = json.Unmarshal([]byte(gjson.GetBytes(result, "usage").Raw), &usage)
	ph.trackUsageWithTokenUsage(c, protocolusage.FromOpenAIResponses(usage), nil)
	// CompactedResponse normally has no model; only rewrite an echoed alias.
	if model := gjson.GetBytes(result, "model"); model.Type == gjson.String {
		if rewritten, rewriteErr := sjson.SetBytes(result, "model", requestModel); rewriteErr == nil {
			result = rewritten
		}
	}
	c.Data(http.StatusOK, "application/json", result)
}

func compactRequestModel(body []byte) (string, error) {
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return "", fmt.Errorf("request body must be a JSON object")
	}
	model := gjson.GetBytes(body, "model")
	if model.Type != gjson.String || strings.TrimSpace(model.String()) == "" {
		return "", fmt.Errorf("model is required")
	}
	return model.String(), nil
}

func compactBadRequest(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, ErrorResponse{Error: ErrorDetail{Message: message, Type: "invalid_request_error"}})
}

func unsupportedResponsesCompaction(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, ErrorResponse{Error: ErrorDetail{
		Message: "responses/compact requires a compatible native Responses backend", Type: "invalid_request_error", Code: "unsupported_operation",
	}})
}
