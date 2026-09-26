package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"mindfs/server/internal/api/usecase"
	"mindfs/server/internal/usage"
)

type usageReportResponse struct {
	usage.Report
	RootID string `json:"root_id,omitempty"`
}

func (h *HTTPHandler) handleUsageReport(w http.ResponseWriter, r *http.Request) {
	days, err := parsePositiveIntQuery(r, "days")
	if err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequest("days must be a positive integer"))
		return
	}
	rootID := strings.TrimSpace(r.URL.Query().Get("root"))
	out, err := h.service().BuildUsageReport(r.Context(), usecase.UsageServiceInput{
		RootID: rootID,
		Days:   days,
	})
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err)
		return
	}
	respondJSON(w, http.StatusOK, usageReportResponse{Report: out, RootID: rootID})
}

func (h *HTTPHandler) handleUsagePreferencesGet(w http.ResponseWriter, _ *http.Request) {
	prices, budget, err := h.service().GetUsagePreferences()
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"prices": prices,
		"budget": budget,
	})
}

func (h *HTTPHandler) handleUsagePreferencesPut(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prices usage.PriceTable `json:"prices"`
		Budget usage.Budget     `json:"budget"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxUploadRequestBytes)).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequest("invalid request body"))
		return
	}
	if err := h.service().SaveUsagePreferences(req.Prices, req.Budget); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	prices, budget, err := h.service().GetUsagePreferences()
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"prices": prices,
		"budget": budget,
	})
}
