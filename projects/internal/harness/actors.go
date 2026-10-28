package harness

import (
	"os"
	"path/filepath"

	"github.com/stretchr/testify/require"
	"github.com/tingly-dev/tingly-box/agentboot/engine/enginetest"
)

// ActorFunc plays one thread turn, standing in for Claude Code.
type ActorFunc func(a *ActorCtx) error

// ActorCtx is what an actor sees of its turn.
type ActorCtx struct {
	World  *World
	Turn   *enginetest.Turn
	Prompt string // the message the turn starts with
	Dir    string // the thread's working directory
}

// Say emits an assistant message.
func (a *ActorCtx) Say(text string) { a.Turn.Say(text, 1, 1) }

// Commit writes a file and commits it in the thread's branch.
func (a *ActorCtx) Commit(name, body string) {
	t := a.World.T
	path := filepath.Join(a.Dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	git(t, a.Dir, "add", ".")
	git(t, a.Dir, "commit", "-qm", "update "+name)
}

// Diligent does the work, commits it and reports with how it checked.
func Diligent(report string) ActorFunc {
	return func(a *ActorCtx) error {
		a.Commit("change.txt", a.Prompt+"\n")
		a.Say(report)
		return nil
	}
}

// Reply ends the turn with text and no commit: a question, an answer.
func Reply(text string) ActorFunc {
	return func(a *ActorCtx) error {
		a.Say(text)
		return nil
	}
}

// Script picks an actor by the message the turn starts with; the first
// entry whose match is contained in the prompt plays, the last is the default.
type Branch struct {
	Match string
	Actor ActorFunc
}

func ByPrompt(branches ...Branch) ActorFunc {
	return func(a *ActorCtx) error {
		for _, b := range branches {
			if b.Match == "" || contains(a.Prompt, b.Match) {
				return b.Actor(a)
			}
		}
		return nil
	}
}
