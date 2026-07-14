package remoteagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	agenttypes "mindfs/server/internal/agent/types"
	"mindfs/server/internal/remote"
)

type Runtime struct {
	manager *remote.Manager
}

type OpenOptions struct {
	ServerID        string
	AgentName       string
	SessionKey      string
	Model           string
	Mode            string
	Effort          string
	FastService     string
	PlanMode        bool
	RemoteSessionID string
}

type Session struct {
	manager   *remote.Manager
	server    remote.Server
	client    *remote.Client
	agentName string

	sessionKey      string
	remoteSessionID string
	model           string
	mode            string
	effort          string
	fastService     string
	planMode        bool

	mu       sync.Mutex
	onUpdate func(agenttypes.Event)
	cancel   context.CancelFunc
}

func NewRuntime(manager *remote.Manager) *Runtime {
	return &Runtime{manager: manager}
}

func (r *Runtime) OpenSession(ctx context.Context, opts OpenOptions) (*Session, error) {
	if r == nil || r.manager == nil {
		return nil, errors.New("remote manager not configured")
	}
	client, server, ok, err := r.manager.Client(opts.ServerID)
	if err != nil {
		return nil, err
	}
	if !ok || client == nil {
		return nil, remote.ErrServerNotFound
	}
	if !server.Enabled {
		return nil, errors.New("remote server disabled")
	}
	if strings.TrimSpace(server.DefaultRootID) == "" {
		return nil, errors.New("remote default_root_id required")
	}
	return &Session{
		manager:         r.manager,
		server:          server,
		client:          client,
		agentName:       strings.TrimSpace(opts.AgentName),
		sessionKey:      strings.TrimSpace(opts.SessionKey),
		remoteSessionID: strings.TrimSpace(opts.RemoteSessionID),
		model:           strings.TrimSpace(opts.Model),
		mode:            strings.TrimSpace(opts.Mode),
		effort:          strings.TrimSpace(opts.Effort),
		fastService:     strings.TrimSpace(opts.FastService),
		planMode:        opts.PlanMode,
	}, nil
}

func (s *Session) SendMessage(ctx context.Context, content string) error {
	if strings.TrimSpace(content) == "" {
		return errors.New("content required")
	}
	s.mu.Lock()
	planMode := s.planMode
	s.mu.Unlock()
	content = messageContent(content, planMode)
	turnCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.cancel = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.cancel = nil
		s.mu.Unlock()
		cancel()
	}()

	ws, err := s.client.DialWS(turnCtx, "/ws")
	if err != nil {
		return err
	}
	defer ws.Close()
	go func() {
		<-turnCtx.Done()
		_ = ws.Close()
	}()

	requestID := fmt.Sprintf("remote-msg-%d", time.Now().UnixNano())
	payload := map[string]any{
		"root_id":      s.server.DefaultRootID,
		"session_key":  s.remoteSessionID,
		"type":         "chat",
		"agent":        s.agentName,
		"model":        s.model,
		"agent_mode":   s.mode,
		"effort":       s.effort,
		"fast_service": s.fastService,
		"content":      content,
	}
	if err := ws.SendJSON(map[string]any{
		"id":      requestID,
		"type":    "session.message",
		"payload": payload,
	}); err != nil {
		return err
	}

	for {
		var resp map[string]any
		if err := ws.ReadJSON(&resp); err != nil {
			if turnCtx.Err() != nil {
				return turnCtx.Err()
			}
			return err
		}
		if err := s.handleWSResponse(resp); err != nil {
			return err
		}
		if strings.TrimSpace(stringValue(resp["type"])) == "session.done" {
			return nil
		}
	}
}

func (s *Session) AnswerQuestion(ctx context.Context, answer agenttypes.AskUserAnswer) error {
	if strings.TrimSpace(s.remoteSessionID) == "" {
		return errors.New("remote session id missing")
	}
	ws, err := s.client.DialWS(ctx, "/ws")
	if err != nil {
		return err
	}
	defer ws.Close()
	requestID := fmt.Sprintf("remote-answer-%d", time.Now().UnixNano())
	if err := ws.SendJSON(map[string]any{
		"id":   requestID,
		"type": "session.answer_question",
		"payload": map[string]any{
			"root_id":     s.server.DefaultRootID,
			"session_key": s.remoteSessionID,
			"agent":       s.agentName,
			"tool_use_id": answer.ToolUseID,
			"answers":     answer.Answers,
		},
	}); err != nil {
		return err
	}
	return waitForResponse(ws, "session.answer_question.accepted")
}

func (s *Session) CurrentModel() string { return s.model }

func (s *Session) SetModel(_ context.Context, model string) error {
	s.model = strings.TrimSpace(model)
	return nil
}

func (s *Session) ListModels(ctx context.Context) (agenttypes.ModelList, error) {
	agents, err := s.client.Agents(ctx)
	if err != nil {
		return agenttypes.ModelList{}, err
	}
	for _, item := range agents.Agents {
		if stringValue(item["name"]) != s.agentName {
			continue
		}
		var out agenttypes.ModelList
		out.CurrentModelID = stringValue(item["current_model_id"])
		if raw, ok := item["models"]; ok {
			_ = remarshal(raw, &out.Models)
		}
		return out, nil
	}
	return agenttypes.ModelList{}, nil
}

func (s *Session) SetMode(_ context.Context, mode string) error {
	s.mode = strings.TrimSpace(mode)
	return nil
}

func (s *Session) SetPlanMode(ctx context.Context, enabled bool) error {
	s.mu.Lock()
	remoteSessionID := s.remoteSessionID
	s.mu.Unlock()
	if remoteSessionID != "" {
		ws, err := s.client.DialWS(ctx, "/ws")
		if err != nil {
			return err
		}
		defer ws.Close()
		requestID := fmt.Sprintf("remote-plan-%d", time.Now().UnixNano())
		if err := ws.SendJSON(map[string]any{
			"id":   requestID,
			"type": "session.plan_mode.set",
			"payload": map[string]any{
				"root_id":     s.server.DefaultRootID,
				"session_key": remoteSessionID,
				"enabled":     enabled,
			},
		}); err != nil {
			return err
		}
		if err := waitForResponse(ws, "session.done"); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.planMode = enabled
	s.mu.Unlock()
	return nil
}

func (s *Session) ListModes(ctx context.Context) (agenttypes.ModeList, error) {
	agents, err := s.client.Agents(ctx)
	if err != nil {
		return agenttypes.ModeList{}, err
	}
	for _, item := range agents.Agents {
		if stringValue(item["name"]) != s.agentName {
			continue
		}
		var out agenttypes.ModeList
		out.CurrentModeID = stringValue(item["current_mode_id"])
		if raw, ok := item["modes"]; ok {
			_ = remarshal(raw, &out.Modes)
		}
		return out, nil
	}
	return agenttypes.ModeList{}, nil
}

func (s *Session) ListCommands(ctx context.Context) (agenttypes.CommandList, error) {
	agents, err := s.client.Agents(ctx)
	if err != nil {
		return agenttypes.CommandList{}, err
	}
	for _, item := range agents.Agents {
		if stringValue(item["name"]) != s.agentName {
			continue
		}
		var out agenttypes.CommandList
		if raw, ok := item["commands"]; ok {
			_ = remarshal(raw, &out.Commands)
		}
		return out, nil
	}
	return agenttypes.CommandList{}, nil
}

func (s *Session) CancelCurrentTurn() error {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (s *Session) OnUpdate(onUpdate func(agenttypes.Event)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onUpdate = onUpdate
}

func (s *Session) SessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.remoteSessionID
}

func (s *Session) ContextWindow(context.Context) (agenttypes.ContextWindow, error) {
	return agenttypes.ContextWindow{}, nil
}

func (s *Session) Close() error {
	return s.CancelCurrentTurn()
}

func (s *Session) handleWSResponse(resp map[string]any) error {
	respType := strings.TrimSpace(stringValue(resp["type"]))
	payload, _ := resp["payload"].(map[string]any)
	if respType == "session.accepted" && payload != nil {
		if key := stringValue(payload["session_key"]); key != "" {
			s.mu.Lock()
			s.remoteSessionID = key
			s.mu.Unlock()
		}
		return nil
	}
	if respType == "session.error" {
		return responseError(resp)
	}
	if respType != "session.stream" || payload == nil {
		return nil
	}
	event, _ := payload["event"].(map[string]any)
	if event == nil {
		return nil
	}
	update, ok := streamEventToUpdate(event)
	if !ok {
		return nil
	}
	s.emit(update)
	return nil
}

func messageContent(content string, planMode bool) string {
	if !planMode {
		return content
	}
	trimmed := strings.TrimSpace(content)
	lower := strings.ToLower(trimmed)
	if lower == "/plan" || strings.HasPrefix(lower, "/plan ") {
		return content
	}
	return "/plan " + content
}

func waitForResponse(ws *remote.WSConn, terminalType string) error {
	for {
		var resp map[string]any
		if err := ws.ReadJSON(&resp); err != nil {
			return err
		}
		respType := strings.TrimSpace(stringValue(resp["type"]))
		if respType == "session.error" {
			return responseError(resp)
		}
		if respType == terminalType {
			return nil
		}
	}
}

func responseError(resp map[string]any) error {
	if errPayload, ok := resp["error"].(map[string]any); ok {
		if msg := stringValue(errPayload["message"]); msg != "" {
			return errors.New(msg)
		}
	}
	if payload, ok := resp["payload"].(map[string]any); ok {
		if msg := stringValue(payload["message"]); msg != "" {
			return errors.New(msg)
		}
	}
	return errors.New("remote session error")
}

func (s *Session) emit(update agenttypes.Event) {
	s.mu.Lock()
	onUpdate := s.onUpdate
	s.mu.Unlock()
	if onUpdate != nil {
		onUpdate(update)
	}
}

func streamEventToUpdate(event map[string]any) (agenttypes.Event, bool) {
	eventType := strings.TrimSpace(stringValue(event["type"]))
	data := event["data"]
	switch eventType {
	case "message_chunk":
		var out agenttypes.MessageChunk
		if remarshal(data, &out) == nil {
			return agenttypes.Event{Type: agenttypes.EventTypeMessageChunk, Data: out}, true
		}
	case "thought_chunk":
		var out agenttypes.ThoughtChunk
		if remarshal(data, &out) == nil {
			return agenttypes.Event{Type: agenttypes.EventTypeThoughtChunk, Data: out}, true
		}
	case "tool_call":
		var out agenttypes.ToolCall
		if remarshal(data, &out) == nil {
			return agenttypes.Event{Type: agenttypes.EventTypeToolCall, Data: out}, true
		}
	case "tool_call_update":
		var out agenttypes.ToolCall
		if remarshal(data, &out) == nil {
			return agenttypes.Event{Type: agenttypes.EventTypeToolUpdate, Data: out}, true
		}
	case "todo_update":
		var out agenttypes.TodoUpdate
		if remarshal(data, &out) == nil {
			return agenttypes.Event{Type: agenttypes.EventTypeTodoUpdate, Data: out}, true
		}
	case "plan_update":
		var out agenttypes.PlanUpdate
		if remarshal(data, &out) == nil {
			return agenttypes.Event{Type: agenttypes.EventTypePlanUpdate, Data: out}, true
		}
	case "compact_notice":
		var out agenttypes.CompactNotice
		if remarshal(data, &out) == nil {
			return agenttypes.Event{Type: agenttypes.EventTypeCompact, Data: out}, true
		}
	case "message_done":
		var out agenttypes.MessageDone
		_ = remarshal(data, &out)
		return agenttypes.Event{Type: agenttypes.EventTypeMessageDone, Data: out}, true
	case "recovery":
		var out agenttypes.RecoveryStatus
		_ = remarshal(data, &out)
		return agenttypes.Event{Type: agenttypes.EventTypeRecovery, Data: out}, true
	case "error":
		if m, ok := data.(map[string]any); ok {
			if msg := stringValue(m["message"]); msg != "" {
				return agenttypes.Event{}, false
			}
		}
	}
	return agenttypes.Event{}, false
}

func remarshal(in any, out any) error {
	payload, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, out)
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}
