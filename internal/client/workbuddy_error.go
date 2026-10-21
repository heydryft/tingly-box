package client

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/tingly-dev/tingly-box/internal/protocol"
)

// WorkBuddy's errors use {code,msg,displayMsg}, not OpenAI's {error:{...}}.
// Normalize at this provider's transport boundary, preserving the envelope for
// diagnostics and the HTTP status for callers. The SDK can then retain both the
// vendor code and the failure kind through streaming and complete-call paths.
func workbuddyNormalizeError(resp *http.Response) error {
	if resp == nil || resp.Body == nil || resp.StatusCode < 400 {
		return nil
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return err
	}
	body := raw
	var env map[string]json.RawMessage
	if json.Unmarshal(raw, &env) == nil && env["error"] == nil && env["code"] != nil {
		code := strings.Trim(string(env["code"]), `"`)
		var msg, message string
		_ = json.Unmarshal(env["msg"], &msg)
		_ = json.Unmarshal(env["message"], &message)
		if msg == "" {
			msg = message
		}
		var display map[string]string
		_ = json.Unmarshal(env["displayMsg"], &display)
		kind := "workbuddy_error"
		switch {
		case resp.StatusCode == http.StatusForbidden && code == "11140":
			kind = protocol.FailureAccountRestricted
			msg += ": account restricted by the upstream; submit feedback in WorkBuddy to appeal"
		case code == "11128" && strings.Contains(strings.ToLower(msg), "unapproved channel"), code == "10085":
			kind = protocol.FailureChannelRefused
			msg += "; use an allowed client channel or another provider"
		case resp.StatusCode == http.StatusBadRequest:
			kind = protocol.FailureInvalidRequest
		}
		if display["en"] != "" {
			msg += ": " + display["en"]
		}
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		env["error"], _ = json.Marshal(map[string]any{"code": code, "type": kind, "message": msg, "vendor_error": json.RawMessage(raw)})
		if normalized, marshalErr := json.Marshal(env); marshalErr == nil {
			body = normalized
		}
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	return nil
}
