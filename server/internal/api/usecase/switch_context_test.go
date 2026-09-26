package usecase

import (
	"strings"
	"testing"

	"mindfs/server/internal/agent"
	rootfs "mindfs/server/internal/fs"
	"mindfs/server/internal/session"
)

func TestTailLogicalLinesHandlesLongEntriesAndTrailingNewline(t *testing.T) {
	long := strings.Repeat("x", 5000)
	input := strings.Join([]string{"a", "b", long, "c", ""}, "\n")

	got := tailLogicalLines(strings.NewReader(input), 2)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (%#v)", len(got), got)
	}
	if got[0] != long || got[1] != "c" {
		t.Fatalf("tail = %#v, want the last two logical lines", got)
	}
}

func TestTailLogicalLinesKeepsFinalLineWithoutNewline(t *testing.T) {
	got := tailLogicalLines(strings.NewReader("a\nb\npartial"), 3)
	if len(got) != 3 || got[2] != "partial" {
		t.Fatalf("tail = %#v", got)
	}
}

func TestTailLogicalLinesBoundsToRequestedCount(t *testing.T) {
	got := tailLogicalLines(strings.NewReader("1\n2\n3\n4\n5\n"), 2)
	if len(got) != 2 || got[0] != "4" || got[1] != "5" {
		t.Fatalf("tail = %#v", got)
	}
}

func TestBuildInlineSwitchContextBoundsEachEntry(t *testing.T) {
	huge := strings.Repeat("y", inlineSwitchContextEntryMaxRunes*2)
	hint := buildInlineSwitchContext([]string{huge}, 1)

	if !strings.Contains(hint, "...") {
		t.Fatal("oversized entry should be truncated")
	}
	if len([]rune(hint)) > inlineSwitchContextEntryMaxRunes+400 {
		t.Fatalf("inlined context too large: %d runes", len([]rune(hint)))
	}
	if !strings.Contains(hint, "cannot read the local session log directly") {
		t.Fatalf("hint should explain why history is inlined: %q", hint)
	}
}

func TestBuildInlineSwitchContextEmptyEntriesProducesNothing(t *testing.T) {
	if got := buildInlineSwitchContext(nil, 5); got != "" {
		t.Fatalf("buildInlineSwitchContext(nil) = %q, want empty", got)
	}
}

// Codex sessions run in an app-server whose cwd is the project root, but the
// exchange log lives under ~/.mindfs for home-located metadata. The agent cannot
// read that path, so the history must be inlined instead of hinted.
func TestBuildPromptInlinesHistoryForCodexWhenLogIsOutsideRuntimeRoot(t *testing.T) {
	root, manager, current := buildHomeMetaSessionFixture(t, "session-1")

	service := Service{}
	got := service.BuildPrompt(BuildPromptInput{
		Session:       current,
		Manager:       manager,
		Agent:         "codex",
		Message:       "继续",
		AgentProtocol: agent.ProtocolCodexSDK,
	})

	if !strings.Contains(got, "local question") && !strings.Contains(got, "历史问题") {
		t.Fatalf("codex prompt did not inline history: %q", got)
	}
	if strings.Contains(got, "read the last") {
		t.Fatalf("codex prompt contains an unreadable log hint: %q", got)
	}
	_ = root
}

// ACP agents run with the project root as cwd and can read the log themselves,
// so they keep the cheaper "go read the file" hint.
func TestBuildPromptKeepsFileHintForACPAgents(t *testing.T) {
	_, manager, current := buildHomeMetaSessionFixture(t, "session-2")

	service := Service{}
	got := service.BuildPrompt(BuildPromptInput{
		Session:       current,
		Manager:       manager,
		Agent:         "pi",
		Message:       "继续",
		AgentProtocol: agent.ProtocolACP,
	})

	if !strings.Contains(got, "read the last") {
		t.Fatalf("ACP prompt should keep the log read hint: %q", got)
	}
}

func buildHomeMetaSessionFixture(t *testing.T, key string) (string, *session.Manager, *session.Session) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)

	rootDir := t.TempDir()
	root := rootfs.NewRootInfo("mindfs", "mindfs", rootDir)
	root.MetaLocation = rootfs.MetaLocationHome
	if _, err := root.EnsureMetaDir(); err != nil {
		t.Fatalf("EnsureMetaDir: %v", err)
	}
	manager := session.NewManager(root)
	created, err := manager.Create(t.Context(), session.CreateInput{Type: session.TypeChat, Name: "handoff"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := manager.AddExchangeForAgent(t.Context(), created, "user", "历史问题", "claude", "", "", ""); err != nil {
		t.Fatalf("AddExchangeForAgent(user): %v", err)
	}
	_ = key
	return rootDir, manager, created
}
