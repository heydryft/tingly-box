package project

// The thinking-effort levels a project can give its coordinator and its
// threads, the same ladder the gateway's rules speak (internal/protocol/
// thinking). "minimal" and "xhigh" are left out like the routing UI's own
// effort picker: outside a handful of the newest models they silently
// collapse onto "low"/"high" server-side.

// Effort is one level of the ladder; empty is the endpoint's default.
type Effort = string

// EffortLevels lists the selectable levels, default first.
var EffortLevels = []Effort{"", "low", "medium", "high", "max"}

// ValidEffort reports whether e is a level of the ladder; empty is valid.
func ValidEffort(e Effort) bool {
	for _, level := range EffortLevels {
		if e == level {
			return true
		}
	}
	return false
}
