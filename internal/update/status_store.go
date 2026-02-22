package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"split-vpn-webui/internal/version"
)

func loadPersistedStatus(path string) (persistedStatus, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return persistedStatus{}, nil
		}
		return persistedStatus{}, err
	}
	var stored persistedStatus
	if err := json.Unmarshal(bytes, &stored); err != nil {
		return persistedStatus{}, err
	}
	return stored, nil
}

func savePersistedStatus(path string, status persistedStatus) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func toPublicStatus(stored persistedStatus, current version.Info) Status {
	out := Status{
		Current:              current,
		LatestVersion:        stored.LatestVersion,
		InProgress:           stored.InProgress,
		State:                stored.State,
		Message:              stored.Message,
		LastError:            stored.LastError,
		LastAttemptedVersion: stored.LastAttemptedVersion,
		LastSuccessVersion:   stored.LastSuccessVersion,
	}
	if out.State == "" {
		out.State = "idle"
	}
	if stored.LatestPublishedAt > 0 {
		t := time.Unix(stored.LatestPublishedAt, 0).UTC()
		out.LatestPublishedAt = &t
	}
	if stored.LastCheckedAt > 0 {
		t := time.Unix(stored.LastCheckedAt, 0).UTC()
		out.LastCheckedAt = &t
	}
	if stored.LastAttemptAt > 0 {
		t := time.Unix(stored.LastAttemptAt, 0).UTC()
		out.LastAttemptAt = &t
	}
	if stored.LastSuccessAt > 0 {
		t := time.Unix(stored.LastSuccessAt, 0).UTC()
		out.LastSuccessAt = &t
	}
	out.UpdateAvailable = isNewerVersion(out.Current.Version, out.LatestVersion)
	return out
}

func withFileLock(lockPath string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return fn()
}
