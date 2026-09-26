package session

import (
	"context"
	"testing"
	"time"

	rootfs "mindfs/server/internal/fs"
)

func TestSearchAppliesAgentAndTimeFilters(t *testing.T) {
	root := rootfs.NewRootInfo("mindfs", "mindfs", t.TempDir())
	manager := NewManager(root)
	ctx := context.Background()

	codexSession, err := manager.Create(ctx, CreateInput{Type: TypeChat, Name: "deploy checklist"})
	if err != nil {
		t.Fatalf("Create(codex): %v", err)
	}
	if err := manager.UpdateAgentState(ctx, codexSession, "codex", 1, "codex-session"); err != nil {
		t.Fatalf("UpdateAgentState(codex): %v", err)
	}
	piSession, err := manager.Create(ctx, CreateInput{Type: TypeChat, Name: "deploy checklist"})
	if err != nil {
		t.Fatalf("Create(pi): %v", err)
	}
	if err := manager.UpdateAgentState(ctx, piSession, "pi", 1, "pi-session"); err != nil {
		t.Fatalf("UpdateAgentState(pi): %v", err)
	}

	keys := func(hits []SearchHit) []string {
		out := make([]string, 0, len(hits))
		for _, hit := range hits {
			out = append(out, hit.Key)
		}
		return out
	}

	all, err := manager.Search(ctx, SearchOptions{Query: "deploy", Limit: 10})
	if err != nil {
		t.Fatalf("Search(all): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("unfiltered hits = %d, want 2", len(all))
	}

	byAgent, err := manager.Search(ctx, SearchOptions{Query: "deploy", Limit: 10, Agent: "codex"})
	if err != nil {
		t.Fatalf("Search(agent): %v", err)
	}
	if got := keys(byAgent); len(got) != 1 || got[0] != codexSession.Key {
		t.Fatalf("agent filter hits = %v, want [%s]", got, codexSession.Key)
	}

	// Agent names are matched case-insensitively so the UI can pass either form.
	mixedCase, err := manager.Search(ctx, SearchOptions{Query: "deploy", Limit: 10, Agent: "PI"})
	if err != nil {
		t.Fatalf("Search(agent, mixed case): %v", err)
	}
	if got := keys(mixedCase); len(got) != 1 || got[0] != piSession.Key {
		t.Fatalf("case-insensitive agent filter hits = %v, want [%s]", got, piSession.Key)
	}

	after, err := manager.Search(ctx, SearchOptions{
		Query:     "deploy",
		Limit:     10,
		AfterTime: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Search(after): %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("after-filter hits = %d, want 0", len(after))
	}

	before, err := manager.Search(ctx, SearchOptions{
		Query:      "deploy",
		Limit:      10,
		BeforeTime: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("Search(before): %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("before-filter hits = %d, want 0", len(before))
	}

	inRange, err := manager.Search(ctx, SearchOptions{
		Query:      "deploy",
		Limit:      10,
		BeforeTime: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Search(in range): %v", err)
	}
	if len(inRange) != 2 {
		t.Fatalf("in-range hits = %d, want 2", len(inRange))
	}
}
