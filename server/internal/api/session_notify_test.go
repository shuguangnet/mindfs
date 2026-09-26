package api

import (
	"strings"
	"testing"
	"time"
)

// Every terminal state must route to exactly one notification decision. This
// table pins the decision so a failure can never be reported as a completion
// and a user cancel never notifies at all.
func TestSessionNotificationDecision(t *testing.T) {
	cases := []struct {
		name          string
		requestID     string
		pending       PendingSessionSnapshot
		wantNotify    bool
		wantKind      string
		wantScheduled bool
	}{
		{
			name:       "successful turn notifies completion",
			pending:    PendingSessionSnapshot{SessionTitle: "任务", Summary: "做完了"},
			wantNotify: true,
			wantKind:   "session.done",
		},
		{
			name:       "watchdog failure notifies a failure",
			pending:    PendingSessionSnapshot{SessionTitle: "任务", TerminalError: "agent idle for 10m0s, automatically canceled this turn"},
			wantNotify: true,
			wantKind:   "session.failed",
		},
		{
			name:       "non-recoverable error notifies a failure",
			pending:    PendingSessionSnapshot{SessionTitle: "任务", TerminalError: "model service unavailable"},
			wantNotify: true,
			wantKind:   "session.failed",
		},
		{
			name:       "user cancel stays silent",
			pending:    PendingSessionSnapshot{SessionTitle: "任务", Cancelled: true},
			wantNotify: false,
		},
		{
			name:       "cancel wins even with a stray error",
			pending:    PendingSessionSnapshot{SessionTitle: "任务", Cancelled: true, TerminalError: "context canceled"},
			wantNotify: false,
		},
		{
			// The scheduled service notifies successful runs itself, so this path
			// must stay silent to avoid a duplicate push.
			name:          "scheduled success is owned by the scheduled path",
			requestID:     "scheduled:task-1",
			pending:       PendingSessionSnapshot{SessionTitle: "定时", Summary: "跑完了"},
			wantNotify:    false,
			wantScheduled: true,
		},
		{
			name:          "scheduled failure notifies once through the scheduled path",
			requestID:     "scheduled:task-1",
			pending:       PendingSessionSnapshot{SessionTitle: "定时", TerminalError: "agent unavailable"},
			wantNotify:    true,
			wantScheduled: true,
		},
		{
			name:       "scheduled cancel stays silent",
			requestID:  "scheduled:task-1",
			pending:    PendingSessionSnapshot{SessionTitle: "定时", Cancelled: true},
			wantNotify: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := decideSessionNotification(tc.requestID, tc.pending)
			if decision.notify != tc.wantNotify {
				t.Fatalf("notify = %v, want %v", decision.notify, tc.wantNotify)
			}
			if decision.scheduled != tc.wantScheduled {
				t.Fatalf("scheduled = %v, want %v", decision.scheduled, tc.wantScheduled)
			}
			if !tc.wantNotify {
				if decision.eventID != "" {
					t.Fatalf("silent decision must not mint an event id, got %q", decision.eventID)
				}
				return
			}
			if !tc.wantScheduled && decision.kind != tc.wantKind {
				t.Fatalf("kind = %q, want %q", decision.kind, tc.wantKind)
			}
			if tc.wantScheduled {
				if decision.kind != scheduledFailureKind {
					t.Fatalf("kind = %q, want %q", decision.kind, scheduledFailureKind)
				}
				if decision.taskID != "task-1" {
					t.Fatalf("taskID = %q, want task-1", decision.taskID)
				}
			}
			if decision.eventID == "" {
				t.Fatal("eventID must be set for a notification")
			}
			if strings.HasPrefix(decision.eventID, "scheduled:") {
				t.Fatalf("eventID must not reuse the scheduled run marker: %q", decision.eventID)
			}
		})
	}
}

func TestScheduledNotificationIgnoresRunMarkerAsEventID(t *testing.T) {
	// Reusing "scheduled:<task>" as the event id would collide with how scheduled
	// runs are deduplicated, silently dropping the failure notification.
	decision := decideSessionNotification("scheduled:task-9", PendingSessionSnapshot{
		SessionTitle:  "定时",
		TerminalError: "boom",
		UpdatedAt:     time.Date(2026, time.September, 25, 10, 0, 0, 0, time.UTC),
	})
	if decision.eventID == "scheduled:task-9" {
		t.Fatalf("eventID must not reuse the scheduled run marker, got %q", decision.eventID)
	}
	if !strings.Contains(decision.eventID, "session.turn:") {
		t.Fatalf("eventID = %q, want a per-turn id", decision.eventID)
	}
}

func TestScheduledFailureEventIDDiffersPerMessage(t *testing.T) {
	updated := time.Date(2026, time.September, 25, 10, 0, 0, 0, time.UTC)
	first := decideSessionNotification("scheduled:t", PendingSessionSnapshot{TerminalError: "boom", UpdatedAt: updated})
	second := decideSessionNotification("scheduled:t", PendingSessionSnapshot{TerminalError: "different boom", UpdatedAt: updated})
	if first.eventID == second.eventID {
		t.Fatalf("distinct failures shared an event id: %q", first.eventID)
	}
	repeat := decideSessionNotification("scheduled:t", PendingSessionSnapshot{TerminalError: "boom", UpdatedAt: updated})
	if repeat.eventID != first.eventID {
		t.Fatalf("same failure should dedupe: %q vs %q", repeat.eventID, first.eventID)
	}
}

func TestScheduledTaskIDFromRequestID(t *testing.T) {
	if got := scheduledTaskIDFromRequestID("scheduled:task-42"); got != "task-42" {
		t.Fatalf("scheduledTaskIDFromRequestID = %q, want task-42", got)
	}
	if got := scheduledTaskIDFromRequestID("  "); got != "" {
		t.Fatalf("scheduledTaskIDFromRequestID(blank) = %q, want empty", got)
	}
}
