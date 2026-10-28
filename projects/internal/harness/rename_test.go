package harness

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tingly-dev/tingly-box/projects/internal/db"
	"github.com/tingly-dev/tingly-box/projects/llm"
)

// R1: when a thread's goal changes, the coordinator renames its card; the
// work, branch and history stay, and the next turn sees the new title.
func TestRenameThreadWhenTheGoalChanges(t *testing.T) {
	const newTitle = "Add end-of-turn and rename tools"
	w := New(t, Options{
		Files: projectsRepo,
		Actor: Diligent("done"),
		Brain: func(c *BrainCtx) llm.Result {
			switch {
			case c.AfterTools():
				return llm.Result{Text: "Renamed."}
			case c.CheckIn():
				return llm.Result{ToolCalls: []llm.ToolCall{Call("n", "no_update", map[string]string{})}}
			case strings.Contains(c.Last(), "rename tool too"):
				return llm.Result{ToolCalls: []llm.ToolCall{Call("r", "rename_thread", map[string]string{
					"thread_id": c.ThreadRef(endTool), "title": newTitle})}}
			default:
				return startThread(endTool, "Add the no_update tool.")
			}
		},
	})

	w.Say("Add an end-of-turn tool.")
	before := w.ThreadIs(endTool, db.ThreadReady)
	w.Quiet()

	w.Say("Make it the rename tool too.")
	w.Eventually("renamed", func() bool { return len(w.ThreadList()) == 1 && w.ThreadList()[0].Title == newTitle })
	w.Quiet()

	after := w.ThreadList()[0]
	assert.Equal(t, before.ID, after.ID)
	assert.Equal(t, before.Branch, after.Branch, "the branch keeps its name")
	assert.Equal(t, db.ThreadReady, after.Status)
	assert.Len(t, w.Prompts(), 1, "renaming does not start a turn")
}

// R2: an empty title is refused and the thread keeps its name.
func TestRenameThreadRejectsAnEmptyTitle(t *testing.T) {
	var refused string
	w := New(t, Options{
		Files: projectsRepo,
		Actor: Diligent("done"),
		Brain: func(c *BrainCtx) llm.Result {
			switch {
			case c.AfterTools():
				refused = c.Req.Messages[len(c.Req.Messages)-1].ToolResults[0].Content
				return llm.Result{Text: "Could not rename."}
			case c.CheckIn():
				return llm.Result{ToolCalls: []llm.ToolCall{Call("n", "no_update", map[string]string{})}}
			case strings.Contains(c.Last(), "rename"):
				return llm.Result{ToolCalls: []llm.ToolCall{Call("r", "rename_thread", map[string]string{
					"thread_id": c.ThreadRef(endTool), "title": "  "})}}
			default:
				return startThread(endTool, "Add the no_update tool.")
			}
		},
	})

	w.Say("Add an end-of-turn tool.")
	w.ThreadIs(endTool, db.ThreadReady)
	w.Quiet()
	w.Say("Please rename it.")
	w.Quiet()

	assert.NotEmpty(t, refused)
	assert.Equal(t, endTool, w.ThreadList()[0].Title)
}
