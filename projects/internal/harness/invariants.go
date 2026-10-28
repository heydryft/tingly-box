package harness

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tingly-dev/tingly-box/projects/internal/db"
)

// Invariants hold at the end of every scenario, whatever it did. Each is a
// rule of the Projects workflow, not of one test.
//
//	trust.report-labelled  a thread's report reaches the coordinator labelled
//	                       as a report, never as the user's words
//	wait.has-reason        a thread waiting on the user says what for
//	status.ready-has-commits  a thread is only "ready for review" when its
//	                       branch has commits to review
//	memory.index-resolves  every link in MEMORY.md names a file that exists
func (w *World) CheckInvariants() {
	t := w.T
	t.Helper()

	for _, req := range w.Requests() {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "[Thread update]") && !strings.Contains(m.Content, "not a message from the user") {
				t.Errorf("trust.report-labelled: a thread update reached the coordinator unlabelled:\n%s", m.Content)
			}
		}
	}

	for _, th := range w.ThreadList() {
		if th.Status == db.ThreadWaiting && strings.TrimSpace(th.WaitReason) == "" {
			t.Errorf("wait.has-reason: thread %q is waiting with no reason", th.Title)
		}
	}

	for _, th := range w.ThreadList() {
		if th.Status == db.ThreadReady && th.Branch != "" && th.CommitsAhead == 0 {
			t.Errorf("status.ready-has-commits: thread %q is ready with no commits", th.Title)
		}
	}

	idx := w.Memory("MEMORY.md")
	for _, m := range regexp.MustCompile(`\]\(([^)]+\.md)\)|\[\[([^\]]+\.md)\]\]`).FindAllStringSubmatch(idx, -1) {
		name := m[1]
		if name == "" {
			name = m[2]
		}
		if _, err := os.Stat(filepath.Join(w.Layout.MemoryDir(w.Project.ID), name)); err != nil {
			t.Errorf("memory.index-resolves: MEMORY.md links %s, which is missing", name)
		}
	}
}
