package project

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tingly-dev/tingly-box/projects/internal/db"
)

// Trust levels an <event> envelope carries. Only a principal may instruct the
// coordinator; everything else is information.
const (
	trustPrincipal = "principal" // the user, on the web or in a bound chat
	trustRelay     = "relay"     // a thread's report, relayed text
	trustSystem    = "system"    // the harness: settings changes, check-ins
)

// envelope wraps one conversation event for the coordinator's model with
// where it came from and how far to trust it. attrs are key, value pairs.
// Text from a non-principal source is escaped so it cannot close the envelope
// or open one of its own.
func envelope(trust string, body string, attrs ...string) string {
	var b strings.Builder
	b.WriteString("<event")
	for i := 0; i+1 < len(attrs); i += 2 {
		if attrs[i+1] != "" {
			fmt.Fprintf(&b, " %s=%q", attrs[i], attrs[i+1])
		}
	}
	fmt.Fprintf(&b, " trust=%q>\n", trust)
	if trust != trustPrincipal {
		body = escapeEnvelope(body)
	}
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n</event>")
	return b.String()
}

// escapeEnvelope stops text from forging envelope tags.
func escapeEnvelope(s string) string {
	s = strings.ReplaceAll(s, "<event", "‹event")
	return strings.ReplaceAll(s, "</event", "‹/event")
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04Z")
}

// userSource is the envelope source of a user event.
func userSource(e db.ThreadEvent) string {
	if e.Source == db.SourceIM {
		return "im"
	}
	return "web"
}

// repliesSinceUser counts the coordinator's replies after the user's last
// message, and how long ago that message was.
func repliesSinceUser(history []db.ThreadEvent, now time.Time) (replies int, since time.Duration, known bool) {
	for i := len(history) - 1; i >= 0; i-- {
		switch history[i].Kind {
		case db.EvAssistant:
			replies++
		case db.EvUser:
			return replies, now.Sub(history[i].CreatedAt), true
		}
	}
	return replies, 0, false
}

func roundMinutes(d time.Duration) string {
	return strconv.Itoa(max(0, int(d.Minutes())))
}
