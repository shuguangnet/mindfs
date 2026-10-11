package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"mindfs/server/internal/backup"
	"mindfs/server/internal/backupbundle"
)

// backupImportMaxBytes caps uploaded bundles; instance backups can be much
// larger than regular file uploads.
const backupImportMaxBytes = 4 << 30 // 4 GiB

type backupExportRequest struct {
	Passphrase            string `json:"passphrase"`
	IncludeUploads        bool   `json:"include_uploads"`
	IncludeDeviceIdentity bool   `json:"include_device_identity"`
}

func (h *HTTPHandler) backupManager() *backup.Manager {
	if h.AppContext == nil {
		return nil
	}
	return h.AppContext.GetBackupManager()
}

func (h *HTTPHandler) handleBackupExport(w http.ResponseWriter, r *http.Request) {
	manager := h.backupManager()
	if manager == nil {
		respondError(w, http.StatusServiceUnavailable, errServiceUnavailable("backup manager not configured"))
		return
	}
	var req backupExportRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequest("invalid json body"))
		return
	}
	bundle, err := manager.Export(backup.ExportRequest{
		Passphrase:            req.Passphrase,
		IncludeUploads:        req.IncludeUploads,
		IncludeDeviceIdentity: req.IncludeDeviceIdentity,
	})
	if err != nil {
		respondBackupError(w, err)
		return
	}
	defer bundle.Cleanup()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+bundle.Name+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(bundle.Size, 10))
	w.Header().Set("X-Mindfs-Backup-Created-At", bundle.Manifest.CreatedAt.Format(time.RFC3339))
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, bundle.File); err != nil {
		// Headers already sent; nothing left but to drop the connection.
		return
	}
}

func (h *HTTPHandler) handleBackupImport(w http.ResponseWriter, r *http.Request) {
	manager := h.backupManager()
	if manager == nil {
		respondError(w, http.StatusServiceUnavailable, errServiceUnavailable("backup manager not configured"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, backupImportMaxBytes)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequest("invalid multipart form"))
		return
	}
	if r.MultipartForm == nil {
		respondError(w, http.StatusBadRequest, errInvalidRequest("bundle file required"))
		return
	}
	passphrase := r.FormValue("passphrase")
	fileHeaders := r.MultipartForm.File["file"]
	if len(fileHeaders) == 0 {
		respondError(w, http.StatusBadRequest, errInvalidRequest("bundle file required"))
		return
	}
	file, err := fileHeaders[0].Open()
	if err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequest("cannot open bundle"))
		return
	}
	defer file.Close()

	preview, err := manager.Import(file, passphrase)
	if err != nil {
		respondBackupError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, preview)
}

func (h *HTTPHandler) handleBackupApply(w http.ResponseWriter, r *http.Request) {
	manager := h.backupManager()
	if manager == nil {
		respondError(w, http.StatusServiceUnavailable, errServiceUnavailable("backup manager not configured"))
		return
	}
	var req backup.ApplyRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequest("invalid json body"))
		return
	}
	result, err := manager.Apply(req)
	if err != nil {
		respondBackupError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func respondBackupError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, backupbundle.ErrShortPassphrase):
		payload := map[string]any{"error": err.Error(), "code": "passphrase_too_short"}
		respondJSON(w, status, payload)
		return
	case errors.Is(err, backupbundle.ErrBadPassphrase):
		payload := map[string]any{"error": "wrong passphrase or corrupted bundle", "code": "bad_passphrase"}
		respondJSON(w, http.StatusUnauthorized, payload)
		return
	case errors.Is(err, backup.ErrBadToken):
		status = http.StatusGone
	case errors.Is(err, backup.ErrInvalidRequest):
		status = http.StatusBadRequest
	case errors.Is(err, backupbundle.ErrTruncated), errors.Is(err, backupbundle.ErrBadBundle):
		status = http.StatusBadRequest
	}
	payload := map[string]any{"error": err.Error()}
	respondJSON(w, status, payload)
}
