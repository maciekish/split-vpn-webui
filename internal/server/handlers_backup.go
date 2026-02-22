package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"split-vpn-webui/internal/backup"
)

const (
	backupImportFormFileField = "file"
)

func (s *Server) handleExportBackup(w http.ResponseWriter, r *http.Request) {
	if s.backup == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "backup manager unavailable"})
		return
	}
	snapshot, err := s.backup.Export(r.Context())
	if err != nil {
		writeBackupError(w, err)
		return
	}
	payload, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		writeBackupError(w, err)
		return
	}
	filename := fmt.Sprintf("split-vpn-webui-backup-%s.json", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func (s *Server) handleImportBackup(w http.ResponseWriter, r *http.Request) {
	if s.backup == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "backup manager unavailable"})
		return
	}
	snapshot, err := decodeBackupImport(r)
	if err != nil {
		writeBackupError(w, err)
		return
	}

	resume, err := s.pauseSchedulers()
	if err != nil {
		writeBackupError(w, err)
		return
	}
	result, importErr := s.backup.Import(r.Context(), snapshot)
	resumeErr := resume()
	if importErr != nil {
		writeBackupError(w, combineImportAndResumeError(importErr, resumeErr))
		return
	}
	if resumeErr != nil {
		writeBackupError(w, resumeErr)
		return
	}

	if err := s.refreshState(); err != nil {
		writeBackupError(w, err)
		return
	}
	s.broadcastUpdate(nil)

	response := map[string]any{"status": "ok"}
	if len(result.Warnings) > 0 {
		response["warnings"] = result.Warnings
	}
	writeJSON(w, http.StatusOK, response)
}

func decodeBackupImport(r *http.Request) (backup.Snapshot, error) {
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		if err := r.ParseMultipartForm(128 << 20); err != nil {
			return backup.Snapshot{}, fmt.Errorf("%w: invalid multipart payload", backup.ErrInvalidSnapshot)
		}
		file, _, err := r.FormFile(backupImportFormFileField)
		if err != nil {
			return backup.Snapshot{}, fmt.Errorf("%w: backup file is required", backup.ErrInvalidSnapshot)
		}
		defer file.Close()
		return decodeBackupSnapshot(file)
	}
	return decodeBackupSnapshot(r.Body)
}

func decodeBackupSnapshot(reader io.Reader) (backup.Snapshot, error) {
	var snapshot backup.Snapshot
	if err := json.NewDecoder(reader).Decode(&snapshot); err != nil {
		return backup.Snapshot{}, fmt.Errorf("%w: invalid JSON body", backup.ErrInvalidSnapshot)
	}
	return snapshot, nil
}

func writeBackupError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, backup.ErrInvalidSnapshot) {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func combineImportAndResumeError(importErr, resumeErr error) error {
	if resumeErr == nil {
		return importErr
	}
	return fmt.Errorf("%v; scheduler restart failed: %w", importErr, resumeErr)
}

func (s *Server) pauseSchedulers() (func() error, error) {
	resolver := s.resolver
	prewarm := s.prewarm

	if resolver != nil {
		if err := resolver.Stop(); err != nil {
			return nil, err
		}
	}
	if prewarm != nil {
		if err := prewarm.Stop(); err != nil {
			if resolver != nil {
				_ = resolver.Start()
			}
			return nil, err
		}
	}
	return func() error {
		var restartErr error
		if resolver != nil {
			if err := resolver.Start(); err != nil {
				restartErr = err
			}
		}
		if prewarm != nil {
			if err := prewarm.Start(); err != nil {
				if restartErr == nil {
					restartErr = err
				} else {
					restartErr = fmt.Errorf("%v; %w", restartErr, err)
				}
			}
		}
		return restartErr
	}, nil
}
