package project

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tingly-dev/tingly-box/agentboot/engine/enginetest"
	"github.com/tingly-dev/tingly-box/projects/internal/db"
	"github.com/tingly-dev/tingly-box/projects/llm"
)

type selectedModelLLM struct{ *fakeLLM }

func (f selectedModelLLM) ClientFor(_ context.Context, model string) (llm.Client, string, error) {
	return f.fakeLLM, model, nil
}

func TestConfigurationSaveNotifiesCoordinatorWithoutMetadataWakeups(t *testing.T) {
	f := newFixture(t, func(turn *enginetest.Turn) error { return nil })
	ai := &fakeLLM{fn: func(llm.Request) llm.Result { return llm.Result{Text: noUpdate} }}
	coord := NewCoordinator(f.store, f.threads.layout, f.threads.hub, f.threads, ai)
	t.Cleanup(func() { settleCoordinator(coord) })
	svc := &Service{Store: f.store, Hub: f.threads.hub, Coordinator: coord}

	f.project.Starred = true
	require.NoError(t, svc.Save(f.project))
	require.NoError(t, svc.Save(f.project))
	assert.Empty(t, ai.requests())
	assert.Empty(t, convo(t, f.store, f.project.ID))

	f.project.Instructions = "Use the new workflow."
	f.project.Repos = nil
	require.NoError(t, svc.Save(f.project))
	require.Eventually(t, func() bool { return !coord.Busy(f.project.ID) }, 5*time.Second, time.Millisecond)
	reqs := ai.requests()
	require.Len(t, reqs, 1)
	assert.Contains(t, reqs[0].System, "Use the new workflow.")
	assert.Contains(t, reqs[0].System, "no repositories")
	assert.Contains(t, lastUserText(reqs[0]), "instructions, repositories")
	assert.Contains(t, lastUserText(reqs[0]), "Continue previously requested work")
	events := convo(t, f.store, f.project.ID)
	require.Len(t, events, 1, "NO_UPDATE stays silent")
	assert.Equal(t, db.EvSystem, events[0].Kind)
	assert.Equal(t, db.SourceSystem, events[0].Source)

	require.NoError(t, svc.Save(f.project))
	assert.Len(t, ai.requests(), 1, "a no-op edit must not wake the coordinator")
	assert.Len(t, convo(t, f.store, f.project.ID), 1)

	f.project.Paused = true
	f.project.Goal = "A new goal"
	require.NoError(t, svc.Save(f.project))
	assert.False(t, coord.Busy(f.project.ID))
	assert.Len(t, ai.requests(), 1, "paused projects record settings without running")
	assert.Len(t, convo(t, f.store, f.project.ID), 2)
}

func TestConfigurationChangeDuringResponseRejectsStaleToolsAndUsesNewModel(t *testing.T) {
	f := newFixture(t, func(turn *enginetest.Turn) error { turn.Say("done", 1, 1); return nil })
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	ai := &fakeLLM{fn: func(req llm.Request) llm.Result {
		if req.Model == "old-model" {
			close(entered)
			<-release
			return llm.Result{ToolCalls: []llm.ToolCall{call("stale", "start_thread", map[string]string{
				"title": "Old plan", "task": "must not run with stale settings", "repo": f.repo,
			})}}
		}
		if hasToolResults(req) && req.Messages[len(req.Messages)-1].ToolResults[0].CallID == "stale" {
			return llm.Result{ToolCalls: []llm.ToolCall{call("fresh", "start_thread", map[string]string{
				"title": "Updated plan", "task": "continue in the replacement repository",
			})}}
		}
		return llm.Result{Text: noUpdate}
	}}
	coord := NewCoordinator(f.store, f.threads.layout, f.threads.hub, f.threads, selectedModelLLM{ai})
	t.Cleanup(func() { once.Do(func() { close(release) }); settleCoordinator(coord) })
	svc := &Service{Store: f.store, Hub: f.threads.hub, Coordinator: coord}
	f.project.CoordinatorModel = "old-model"
	require.NoError(t, f.store.SaveProject(f.project))
	_, err := coord.Post(f.project.ID, "Continue the migration", nil, Source{})
	require.NoError(t, err)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("coordinator did not request the old model")
	}
	f.project.CoordinatorModel = "new-model"
	f.project.Instructions = "Use the replacement repository."
	f.project.Repos = []db.Repo{{Path: newRepo(t), BaseBranch: "main"}}
	require.NoError(t, svc.Save(f.project))
	once.Do(func() { close(release) })
	require.Eventually(t, func() bool { return !coord.Busy(f.project.ID) }, 5*time.Second, time.Millisecond)
	threads, err := f.store.ListThreads(f.project.ID)
	require.NoError(t, err)
	require.Len(t, threads, 1, "only the re-evaluated plan can start work")
	assert.Equal(t, "Updated plan", threads[0].Title)
	assert.Equal(t, f.project.Repos[0].Path, threads[0].RepoPath)
	reqs := ai.requests()
	require.GreaterOrEqual(t, len(reqs), 2)
	assert.Equal(t, "new-model", reqs[1].Model)
	assert.Contains(t, reqs[1].System, f.project.Repos[0].Path)
	assert.NotContains(t, reqs[1].System, f.repo+" (base branch")
	assert.Contains(t, reqs[1].System, f.project.Instructions)
	require.True(t, hasToolResults(reqs[1]))
	assert.True(t, reqs[1].Messages[len(reqs[1].Messages)-1].ToolResults[0].IsError)
	assert.Contains(t, reqs[1].Messages[len(reqs[1].Messages)-1].ToolResults[0].Content, "not executed")
}
