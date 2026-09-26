package usecase

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mindfs/server/internal/agent"
	agenttypes "mindfs/server/internal/agent/types"
	rootfs "mindfs/server/internal/fs"
	"mindfs/server/internal/preferences"
	"mindfs/server/internal/session"
	"mindfs/server/internal/usage"
)

// TestSendMessageWatchdogFailsHungTurn drives the real SendMessage flow with a
// fake ACP agent that accepts the handshake but never answers session/prompt.
// The idle watchdog must cancel the turn and surface an explicit error instead
// of hanging forever.
func TestSendMessageWatchdogFailsHungTurn(t *testing.T) {
	fakeAgent, err := filepath.Abs(filepath.Join("testdata", "fake_acp_agent.py"))
	if err != nil {
		t.Fatal(err)
	}
	prev := turnIdleWatchdog
	turnIdleWatchdog = func() time.Duration { return 1 * time.Second }
	defer func() { turnIdleWatchdog = prev }()

	root := rootfs.NewRootInfo("mindfs", "mindfs", t.TempDir())
	manager := session.NewManager(root)
	pool := agent.NewPool(agent.Config{
		Shells: []agent.Shell{{Command: "sh", Args: []string{"-lc"}}},
		Agents: []agent.Definition{
			{
				Name:     "fake-hang",
				Command:  "python3",
				Args:     []string{fakeAgent},
				Protocol: agent.ProtocolACP,
			},
		},
	})
	registry := &watchdogTestRegistry{root: root, manager: manager, pool: pool}
	service := Service{Registry: registry}
	defer pool.CloseAll()

	created, err := manager.Create(context.Background(), session.CreateInput{Type: session.TypeChat, Agent: "fake-hang", Name: "Watchdog"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	var recoveryNotice string
	start := time.Now()
	sendErr := service.SendMessage(context.Background(), SendMessageInput{
		RootID:  root.ID,
		Key:     created.Key,
		Agent:   "fake-hang",
		Content: "hello there, please answer",
		OnUpdate: func(event agenttypes.Event) {
			if event.Type == agenttypes.EventTypeRecovery {
				if status, ok := event.Data.(agenttypes.RecoveryStatus); ok {
					recoveryNotice = status.Message
				}
			}
		},
	})
	elapsed := time.Since(start)
	if sendErr == nil {
		t.Fatal("SendMessage returned nil, want watchdog error")
	}
	if !strings.Contains(sendErr.Error(), "agent idle for") {
		t.Fatalf("sendErr = %v, want idle watchdog error", sendErr)
	}
	if elapsed > 60*time.Second {
		t.Fatalf("turn took %s, watchdog did not fire in time", elapsed)
	}
	t.Logf("watchdog fired after %s: %v", elapsed, sendErr)
	if recoveryNotice == "" {
		t.Fatal("expected real-time recovery event with watchdog message")
	}

	// The turn must be persisted as a failed exchange so refresh keeps the error.
	got, err := manager.Get(context.Background(), created.Key, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Exchanges) == 0 {
		t.Fatal("no exchanges persisted")
	}
	sawErroredAssistant := false
	for _, ex := range got.Exchanges {
		if ex.Role == "agent" && strings.TrimSpace(ex.Error) != "" {
			sawErroredAssistant = true
		}
	}
	if !sawErroredAssistant {
		t.Fatalf("no errored assistant exchange persisted: %+v", got.Exchanges)
	}
	// Runtime config (agent) must have been persisted at send time.
	if got.Agent != "fake-hang" {
		t.Fatalf("session.Agent = %q, want fake-hang", got.Agent)
	}
}

func TestIsCanceledTurnErrorWatchdogNotUserCancel(t *testing.T) {
	if !isCanceledTurnError(context.Canceled) {
		t.Fatal("context.Canceled should be treated as canceled turn")
	}
	watchdogErr := errors.New("agent idle for 1s, automatically canceled this turn; the model service may be unavailable")
	if isCanceledTurnError(watchdogErr) {
		t.Fatal("watchdog error must not be treated as a user cancel")
	}
}

type watchdogTestRegistry struct {
	root    rootfs.RootInfo
	manager *session.Manager
	pool    *agent.Pool
}

func (r *watchdogTestRegistry) GetRoot(rootID string) (rootfs.RootInfo, error) {
	if rootID != r.root.ID {
		return rootfs.RootInfo{}, errors.New("root not found")
	}
	return r.root, nil
}

func (r *watchdogTestRegistry) GetSessionManager(string) (*session.Manager, error) {
	return r.manager, nil
}

func (r *watchdogTestRegistry) UpsertRoot(string) (rootfs.RootInfo, error) {
	return rootfs.RootInfo{}, nil
}

func (r *watchdogTestRegistry) RemoveRoot(string) (rootfs.RootInfo, error) {
	return rootfs.RootInfo{}, nil
}

func (r *watchdogTestRegistry) RenameRoot(string, string, string) (rootfs.RootInfo, error) {
	return rootfs.RootInfo{}, nil
}

func (r *watchdogTestRegistry) ListRoots() []rootfs.RootInfo {
	return []rootfs.RootInfo{r.root}
}

func (r *watchdogTestRegistry) GetAgentPool() *agent.Pool {
	return r.pool
}

func (r *watchdogTestRegistry) GetPreferences() *preferences.Store {
	return nil
}

func (r *watchdogTestRegistry) GetExternalSessionImporter(string) (agenttypes.ExternalSessionImporter, error) {
	return nil, errors.New("not implemented")
}

func (r *watchdogTestRegistry) GetProber() *agent.Prober {
	return nil
}

func (r *watchdogTestRegistry) GetCandidateRegistry() *CandidateRegistry {
	return NewCandidateRegistry()
}

func (r *watchdogTestRegistry) GetFileWatcher(string, *session.Manager) (*rootfs.SharedFileWatcher, error) {
	return nil, nil
}

func (r *watchdogTestRegistry) ReleaseFileWatcher(string, string) {}

func (r *watchdogTestRegistry) GetUsageStore() *usage.Store { return nil }
