package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"mindfs/server/internal/piexport"
)

type nativeExportRequest struct {
	Agent string `json:"agent,omitempty"` // default: the session's agent
	Mode  string `json:"mode,omitempty"`  // write (default) | download
}

type nativeExportResponse struct {
	SessionID string `json:"session_id"`
	Dir       string `json:"dir,omitempty"`
	Path      string `json:"path,omitempty"`
	Existed   bool   `json:"existed,omitempty"`
	Mode      string `json:"mode"`
}

// handleSessionExportNative writes (or downloads) a MindFS session in the
// target agent's native session format. Pi is the first supported agent; the
// file reuses the agent binding's session id so both MindFS resume and the
// native CLI can pick the conversation up afterwards.
func (h *HTTPHandler) handleSessionExportNative(w http.ResponseWriter, r *http.Request) {
	rootID := strings.TrimSpace(r.URL.Query().Get("root"))
	key := strings.TrimSpace(chi.URLParam(r, "key"))
	if rootID == "" || key == "" {
		respondError(w, http.StatusBadRequest, errInvalidRequest("root and session key required"))
		return
	}
	var req nativeExportRequest
	if r.Body != nil {
		if err := jsonDecodeLimited(r.Body, 1<<20, &req); err != nil {
			respondError(w, http.StatusBadRequest, errInvalidRequest("invalid json body"))
			return
		}
	}
	if req.Mode == "" {
		req.Mode = "write"
	}
	if req.Mode != "write" && req.Mode != "download" {
		respondError(w, http.StatusBadRequest, errInvalidRequest("mode must be write or download"))
		return
	}
	if h.AppContext == nil {
		respondError(w, http.StatusServiceUnavailable, errServiceUnavailable("app context missing"))
		return
	}
	root, err := h.AppContext.GetRoot(rootID)
	if err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	manager, err := h.AppContext.GetSessionManager(rootID)
	if err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	detail, err := manager.Get(r.Context(), key, 0)
	if err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}

	agentName := strings.TrimSpace(req.Agent)
	if agentName == "" {
		agentName = strings.TrimSpace(detail.Agent)
	}
	if agentName != "pi" {
		respondJSON(w, http.StatusBadRequest, map[string]any{
			"error": "native export not supported for agent " + agentName,
			"code":  "unsupported_agent",
		})
		return
	}

	// Reuse the binding's native session id when it exists; otherwise the
	// MindFS session key doubles as a stable (and charset-valid) native id.
	sessionID := strings.TrimSpace(detail.Key)
	if binding, err := manager.GetAgentBinding(r.Context(), key, "pi"); err == nil && binding != nil {
		if id := strings.TrimSpace(binding.AgentSessionID); id != "" {
			sessionID = id
		}
	}

	in := piexport.Session{Cwd: root.RootPath, SessionID: sessionID, CreatedAt: detail.CreatedAt}
	for _, ex := range detail.Exchanges {
		in.Exchanges = append(in.Exchanges, piexport.Exchange{
			Role:      ex.Role,
			Content:   ex.Content,
			Timestamp: ex.Timestamp,
		})
	}

	if req.Mode == "download" {
		payload, id, err := piexport.Generate(in)
		if err != nil {
			respondError(w, http.StatusBadRequest, errInvalidRequest(err.Error()))
			return
		}
		w.Header().Set("Content-Type", "application/jsonl")
		w.Header().Set("Content-Disposition", `attachment; filename="`+id+".jsonl"+`"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
		return
	}

	res, err := piexport.Write(in)
	if err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequest(err.Error()))
		return
	}
	respondJSON(w, http.StatusOK, nativeExportResponse{
		SessionID: res.SessionID,
		Dir:       res.Dir,
		Path:      res.Path,
		Existed:   res.Existed,
		Mode:      "write",
	})
}

func jsonDecodeLimited(body io.Reader, limit int64, v any) error {
	return json.NewDecoder(io.LimitReader(body, limit)).Decode(v)
}
