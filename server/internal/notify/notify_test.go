package notify

import (
	"strings"
	"testing"
)

func TestBuildSessionPayloadFailureRequiresInteraction(t *testing.T) {
	payload := BuildSessionPayload(SessionNotification{
		Type:         "session.failed",
		RootID:       "root-1",
		RootTitle:    "MindFS Repo",
		SessionKey:   "sess-1",
		SessionTitle: "长任务",
		Summary:      "部分输出",
		Error:        "agent idle for 10m0s, automatically canceled this turn",
		EventID:      "event-1",
	})

	if payload.Title != "MindFS Repo · 长任务 · 失败" {
		t.Fatalf("title = %q", payload.Title)
	}
	if !strings.Contains(payload.Body, "agent idle for 10m0s") {
		t.Fatalf("body should carry the failure reason, got %q", payload.Body)
	}
	if !payload.RequireInteraction {
		t.Fatal("failure notification should require interaction")
	}
	if !payload.Renotify {
		t.Fatal("failure notification should renotify")
	}
	if !strings.Contains(payload.Tag, "event-1") {
		t.Fatalf("failure tag should include event id, got %q", payload.Tag)
	}
	if payload.Data["type"] != "session.failed" {
		t.Fatalf("data.type = %v", payload.Data["type"])
	}
}

func TestBuildSessionPayloadCancelledDoesNotRequireInteraction(t *testing.T) {
	payload := BuildSessionPayload(SessionNotification{
		Type:         "session.cancelled",
		RootID:       "root-1",
		RootTitle:    "MindFS Repo",
		SessionKey:   "sess-1",
		SessionTitle: "长任务",
		EventID:      "event-2",
	})

	if payload.Title != "MindFS Repo · 长任务 · 已取消" {
		t.Fatalf("title = %q", payload.Title)
	}
	if payload.RequireInteraction {
		t.Fatal("cancelled notification should not require interaction")
	}
	if !payload.Renotify {
		t.Fatal("cancelled notification should still renotify")
	}
}

func TestBuildSessionPayloadFailureWithoutReasonKeepsSummary(t *testing.T) {
	payload := BuildSessionPayload(SessionNotification{
		Type:       "session.failed",
		RootTitle:  "MindFS",
		SessionKey: "sess-1",
		Summary:    "唯一线索",
	})

	if payload.Body != "唯一线索" {
		t.Fatalf("body = %q, want the summary when no error text is present", payload.Body)
	}
}

func TestBuildSessionPayloadDoneStillCompletes(t *testing.T) {
	payload := BuildSessionPayload(SessionNotification{
		Type:       "session.done",
		RootTitle:  "MindFS",
		SessionKey: "sess-1",
		Summary:    "做完了",
	})
	if payload.Title != "MindFS · 会话 · 完成" {
		t.Fatalf("title = %q", payload.Title)
	}
	if payload.RequireInteraction {
		t.Fatal("done notification must not require interaction")
	}
}
