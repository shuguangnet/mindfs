package api

import (
	"encoding/json"
	"net/http"

	"mindfs/server/internal/remote"

	"github.com/go-chi/chi/v5"
)

func (h *HTTPHandler) handleRemoteServersList(w http.ResponseWriter, r *http.Request) {
	manager := h.AppContext.GetRemoteManager()
	if manager == nil {
		respondJSON(w, http.StatusOK, map[string]any{"servers": []remote.PublicServer{}})
		return
	}
	items, err := manager.PublicList()
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"servers": items})
}

func (h *HTTPHandler) handleRemoteServerSave(w http.ResponseWriter, r *http.Request) {
	manager := h.AppContext.GetRemoteManager()
	if manager == nil {
		respondError(w, http.StatusServiceUnavailable, errServiceUnavailable("remote manager not configured"))
		return
	}
	var server remote.Server
	if err := json.NewDecoder(r.Body).Decode(&server); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequest("invalid json body"))
		return
	}
	saved, err := manager.Save(server)
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	respondJSON(w, http.StatusOK, remote.Public(saved))
}

func (h *HTTPHandler) handleRemoteServerDelete(w http.ResponseWriter, r *http.Request) {
	manager := h.AppContext.GetRemoteManager()
	if manager == nil {
		respondError(w, http.StatusServiceUnavailable, errServiceUnavailable("remote manager not configured"))
		return
	}
	if err := manager.Delete(chi.URLParam(r, "id")); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (h *HTTPHandler) handleRemoteServerTest(w http.ResponseWriter, r *http.Request) {
	manager := h.AppContext.GetRemoteManager()
	if manager == nil {
		respondError(w, http.StatusServiceUnavailable, errServiceUnavailable("remote manager not configured"))
		return
	}
	roots, runtime, err := manager.Test(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"roots":  roots,
		"agents": runtime.Agents,
		"shells": runtime.Shells,
	})
}
