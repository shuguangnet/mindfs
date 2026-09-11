package acp

import "testing"

func TestPromptContentTracking(t *testing.T) {
	s := &sessionState{}
	if s.promptContentSeen() {
		t.Fatal("fresh session should have no prompt content")
	}
	s.clearPromptContent()
	if s.promptContentSeen() {
		t.Fatal("cleared session should have no prompt content")
	}
	s.markPromptContent()
	if !s.promptContentSeen() {
		t.Fatal("session with marked content should report promptContentSeen")
	}
}
