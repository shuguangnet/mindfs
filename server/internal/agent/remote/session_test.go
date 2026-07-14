package remoteagent

import "testing"

func TestMessageContent(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		planMode bool
		want     string
	}{
		{name: "normal", content: "hello", want: "hello"},
		{name: "plan", content: "inspect this", planMode: true, want: "/plan inspect this"},
		{name: "existing plan prefix", content: "/plan inspect this", planMode: true, want: "/plan inspect this"},
		{name: "case insensitive prefix", content: "/PLAN inspect this", planMode: true, want: "/PLAN inspect this"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := messageContent(tt.content, tt.planMode); got != tt.want {
				t.Fatalf("messageContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResponseError(t *testing.T) {
	tests := []struct {
		name string
		resp map[string]any
		want string
	}{
		{name: "error envelope", resp: map[string]any{"error": map[string]any{"message": "denied"}}, want: "denied"},
		{name: "payload", resp: map[string]any{"payload": map[string]any{"message": "failed"}}, want: "failed"},
		{name: "fallback", resp: map[string]any{}, want: "remote session error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := responseError(tt.resp).Error(); got != tt.want {
				t.Fatalf("responseError() = %q, want %q", got, tt.want)
			}
		})
	}
}
