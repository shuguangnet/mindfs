package api

import (
	"encoding/json"
	"net/http"

	"mindfs/server/internal/sshops"

	"github.com/go-chi/chi/v5"
)

func (h *HTTPHandler) sshOpsManager() *sshops.Manager {
	if h == nil || h.AppContext == nil {
		return nil
	}
	return h.AppContext.GetSSHOpsManager()
}

func (h *HTTPHandler) handleSSHServersList(w http.ResponseWriter, r *http.Request) {
	manager := h.sshOpsManager()
	if manager == nil {
		respondJSON(w, http.StatusOK, emptySSHListResponse())
		return
	}
	resp, err := manager.List()
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err)
		return
	}
	respondJSON(w, http.StatusOK, resp)
}

func emptySSHListResponse() sshops.ListResponse {
	return sshops.ListResponse{
		Servers: []sshops.PublicServer{},
		Reuse: sshops.ReuseInfo{
			KeyPaths:    []string{},
			Passwords:   []sshops.ReuseRef{},
			ManagedKeys: []sshops.ReuseRef{},
		},
	}
}

func (h *HTTPHandler) handleSSHServerSave(w http.ResponseWriter, r *http.Request) {
	manager := h.sshOpsManager()
	if manager == nil {
		respondError(w, http.StatusServiceUnavailable, errServiceUnavailable("ssh server manager not configured"))
		return
	}
	var input sshops.SaveInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequest("invalid json body"))
		return
	}
	result, err := manager.Save(input)
	if err != nil {
		respondSSHError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (h *HTTPHandler) handleSSHServerDelete(w http.ResponseWriter, r *http.Request) {
	manager := h.sshOpsManager()
	if manager == nil {
		respondError(w, http.StatusServiceUnavailable, errServiceUnavailable("ssh server manager not configured"))
		return
	}
	if err := manager.Delete(chi.URLParam(r, "id")); err != nil {
		respondSSHError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (h *HTTPHandler) handleSSHServerTest(w http.ResponseWriter, r *http.Request) {
	manager := h.sshOpsManager()
	if manager == nil {
		respondError(w, http.StatusServiceUnavailable, errServiceUnavailable("ssh server manager not configured"))
		return
	}
	result, err := manager.Test(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondSSHError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (h *HTTPHandler) handleSSHServerDeployKey(w http.ResponseWriter, r *http.Request) {
	manager := h.sshOpsManager()
	if manager == nil {
		respondError(w, http.StatusServiceUnavailable, errServiceUnavailable("ssh server manager not configured"))
		return
	}
	result, err := manager.DeployKey(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondSSHError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (h *HTTPHandler) handleSSHServerFSList(w http.ResponseWriter, r *http.Request) {
	manager := h.sshOpsManager()
	if manager == nil {
		respondError(w, http.StatusServiceUnavailable, errServiceUnavailable("ssh server manager not configured"))
		return
	}
	listing, err := manager.ListFS(r.URL.Query().Get("path"))
	if err != nil {
		respondSSHError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, listing)
}

type sshImportPreviewRequest struct {
	Source     string `json:"source"`
	Payload    string `json:"payload"`
	Passphrase string `json:"passphrase,omitempty"`
}

func (h *HTTPHandler) handleSSHServersImportPreview(w http.ResponseWriter, r *http.Request) {
	manager := h.sshOpsManager()
	if manager == nil {
		respondError(w, http.StatusServiceUnavailable, errServiceUnavailable("ssh server manager not configured"))
		return
	}
	var req sshImportPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequest("invalid json body"))
		return
	}
	if req.Source == sshops.SourceSSHConfig && req.Payload == "" {
		payload, err := manager.ReadUserSSHConfig()
		if err != nil {
			respondError(w, http.StatusServiceUnavailable, errInvalidRequest("no local ssh config available"))
			return
		}
		req.Payload = payload
	}
	preview, err := manager.ImportPreview(req.Source, req.Payload, req.Passphrase)
	if err != nil {
		respondSSHError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, preview)
}

func (h *HTTPHandler) handleSSHServersImportApply(w http.ResponseWriter, r *http.Request) {
	manager := h.sshOpsManager()
	if manager == nil {
		respondError(w, http.StatusServiceUnavailable, errServiceUnavailable("ssh server manager not configured"))
		return
	}
	var req sshops.ImportApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequest("invalid json body"))
		return
	}
	if req.Source == sshops.SourceSSHConfig && req.Payload == "" {
		payload, err := manager.ReadUserSSHConfig()
		if err != nil {
			respondError(w, http.StatusServiceUnavailable, errInvalidRequest("no local ssh config available"))
			return
		}
		req.Payload = payload
	}
	result, err := manager.ImportApply(req)
	if err != nil {
		respondSSHError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

type sshExportRequest struct {
	Mode       string `json:"mode"`
	Passphrase string `json:"passphrase,omitempty"`
}

func (h *HTTPHandler) handleSSHServersExport(w http.ResponseWriter, r *http.Request) {
	manager := h.sshOpsManager()
	if manager == nil {
		respondError(w, http.StatusServiceUnavailable, errServiceUnavailable("ssh server manager not configured"))
		return
	}
	var req sshExportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequest("invalid json body"))
		return
	}
	if req.Mode == "" {
		req.Mode = sshops.ExportEncrypted
	}
	file, err := manager.Export(req.Mode, req.Passphrase)
	if err != nil {
		respondSSHError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, file)
}

// respondSSHError maps structured sshops errors to proper HTTP status codes.
func respondSSHError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch sshops.CodeOf(err) {
	case sshops.ErrCodeNotFound:
		status = http.StatusNotFound
	case sshops.ErrCodeAliasConflict:
		status = http.StatusConflict
	case sshops.ErrCodeSecretLocked:
		status = http.StatusServiceUnavailable
	}
	payload := map[string]any{"error": err.Error()}
	if code := sshops.CodeOf(err); code != "" {
		payload["code"] = code
	}
	respondJSON(w, status, payload)
}
