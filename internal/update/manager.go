package update

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"split-vpn-webui/internal/version"
)

const (
	defaultRepo        = "maciekish/split-vpn-webui"
	defaultServiceName = "split-vpn-webui.service"
	defaultUpdaterUnit = "split-vpn-webui-updater.service"
)

// Manager handles release checks and self-update job preparation.
type Manager struct {
	repo        string
	arch        string
	dataDir     string
	binaryPath  string
	serviceName string
	updaterUnit string
	statusPath  string
	statusLock  string
	jobPath     string
	updatesDir  string

	systemd UnitController
	github  *githubClient
	now     func() time.Time
}

// NewManager creates an updater manager.
func NewManager(opts Options) (*Manager, error) {
	dataDir := strings.TrimSpace(opts.DataDir)
	if dataDir == "" {
		dataDir = "/data/split-vpn-webui"
	}
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" {
		repo = defaultRepo
	}
	binaryPath := strings.TrimSpace(opts.BinaryPath)
	if binaryPath == "" {
		binaryPath = filepath.Join(dataDir, "split-vpn-webui")
	}
	serviceName := strings.TrimSpace(opts.ServiceName)
	if serviceName == "" {
		serviceName = defaultServiceName
	}
	updaterUnit := strings.TrimSpace(opts.UpdaterUnit)
	if updaterUnit == "" {
		updaterUnit = defaultUpdaterUnit
	}
	arch, err := normalizeArch(runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	m := &Manager{
		repo:        repo,
		arch:        arch,
		dataDir:     dataDir,
		binaryPath:  binaryPath,
		serviceName: serviceName,
		updaterUnit: updaterUnit,
		statusPath:  filepath.Join(dataDir, "update-status.json"),
		statusLock:  filepath.Join(dataDir, "update-status.lock"),
		jobPath:     filepath.Join(dataDir, "update-job.json"),
		updatesDir:  filepath.Join(dataDir, "updates"),
		systemd:     opts.Systemd,
		github:      newGitHubClient(repo, opts.HTTPClient),
		now:         time.Now,
	}
	if err := m.reconcileStatus(); err != nil {
		return nil, err
	}
	return m, nil
}

func normalizeArch(goArch string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(goArch)) {
	case "amd64", "x86_64":
		return "amd64", nil
	case "arm64", "aarch64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported architecture for updater: %s", goArch)
	}
}

// Status returns the current updater status.
func (m *Manager) Status() (Status, error) {
	var stored persistedStatus
	err := withFileLock(m.statusLock, func() error {
		var readErr error
		stored, readErr = loadPersistedStatus(m.statusPath)
		return readErr
	})
	if err != nil {
		return Status{}, err
	}
	return toPublicStatus(stored, version.Current()), nil
}

// Check fetches release metadata from GitHub and updates persisted status.
func (m *Manager) Check(ctx context.Context, tag string) (Status, error) {
	release, err := m.resolveRelease(ctx, tag)
	if err != nil {
		_ = m.updateStatusLocked(func(stored *persistedStatus) {
			stored.InProgress = false
			stored.LastError = err.Error()
			stored.Message = "release check failed"
			stored.State = "failed"
			stored.LastCheckedAt = m.now().UTC().Unix()
		})
		return Status{}, err
	}
	if err := m.updateStatusLocked(func(stored *persistedStatus) {
		stored.LatestVersion = release.Tag
		if !release.PublishedAt.IsZero() {
			stored.LatestPublishedAt = release.PublishedAt.UTC().Unix()
		}
		stored.LastCheckedAt = m.now().UTC().Unix()
		stored.State = "idle"
		stored.Message = "release metadata refreshed"
		stored.LastError = ""
	}); err != nil {
		return Status{}, err
	}
	return m.Status()
}

// StartUpdate downloads, verifies, and schedules an update job via systemd.
func (m *Manager) StartUpdate(ctx context.Context, requestedTag string) (Status, error) {
	if m.systemd == nil {
		return Status{}, fmt.Errorf("systemd manager unavailable")
	}
	var alreadyInProgress bool
	if err := m.updateStatusLocked(func(stored *persistedStatus) {
		if stored.InProgress {
			alreadyInProgress = true
			return
		}
		stored.InProgress = true
		stored.State = "checking"
		stored.Message = "resolving release metadata"
		stored.LastError = ""
		stored.LastAttemptAt = m.now().UTC().Unix()
	}); err != nil {
		return Status{}, err
	}
	if alreadyInProgress {
		return Status{}, fmt.Errorf("update already in progress")
	}

	release, err := m.resolveRelease(ctx, requestedTag)
	if err != nil {
		_ = m.failAttempt("release lookup failed: " + err.Error())
		return Status{}, err
	}
	current := version.Current().Version
	if !isNewerVersion(current, release.Tag) {
		_ = m.updateStatusLocked(func(stored *persistedStatus) {
			stored.InProgress = false
			stored.State = "idle"
			stored.Message = fmt.Sprintf("already on %s", current)
			stored.LastError = ""
			stored.LatestVersion = release.Tag
			if !release.PublishedAt.IsZero() {
				stored.LatestPublishedAt = release.PublishedAt.UTC().Unix()
			}
			stored.LastCheckedAt = m.now().UTC().Unix()
		})
		return m.Status()
	}

	binaryAsset, err := selectBinaryAsset(release, m.arch)
	if err != nil {
		_ = m.failAttempt(err.Error())
		return Status{}, err
	}

	if err := m.updateStatusLocked(func(stored *persistedStatus) {
		stored.State = "downloading"
		stored.Message = fmt.Sprintf("downloading %s", binaryAsset.Name)
		stored.LastError = ""
		stored.LastAttemptedVersion = release.Tag
		stored.LatestVersion = release.Tag
		if !release.PublishedAt.IsZero() {
			stored.LatestPublishedAt = release.PublishedAt.UTC().Unix()
		}
		stored.LastCheckedAt = m.now().UTC().Unix()
	}); err != nil {
		return Status{}, err
	}

	checksumAsset, hasChecksums := selectChecksumAsset(release)
	if !hasChecksums {
		err := fmt.Errorf("release %s is missing checksum asset", release.Tag)
		_ = m.failAttempt(err.Error())
		return Status{}, err
	}
	checksumCtx, cancelChecksums := context.WithTimeout(ctx, defaultDownloadTimeout)
	checksumMap, err := downloadChecksums(checksumCtx, m.github.client, checksumAsset.URL)
	cancelChecksums()
	if err != nil {
		_ = m.failAttempt("checksum download failed: " + err.Error())
		return Status{}, err
	}
	expected := checksumMap[filepath.Base(binaryAsset.Name)]
	if expected == "" {
		err := fmt.Errorf("checksum for asset %s not found in %s", binaryAsset.Name, checksumAsset.Name)
		_ = m.failAttempt(err.Error())
		return Status{}, err
	}

	tagDir, err := m.prepareTagDirectory(release.Tag)
	if err != nil {
		_ = m.failAttempt(err.Error())
		return Status{}, err
	}
	stagedPath := filepath.Join(tagDir, filepath.Base(binaryAsset.Name))
	downloadCtx, cancelBinary := context.WithTimeout(ctx, defaultDownloadTimeout)
	actualHash, err := downloadFileWithSHA256(downloadCtx, m.github.client, binaryAsset.URL, stagedPath)
	cancelBinary()
	if err != nil {
		_ = m.failAttempt("binary download failed: " + err.Error())
		return Status{}, err
	}
	actualHash = strings.ToLower(strings.TrimSpace(actualHash))
	if actualHash != strings.ToLower(strings.TrimSpace(expected)) {
		_ = os.Remove(stagedPath)
		err := fmt.Errorf("checksum mismatch for %s", binaryAsset.Name)
		_ = m.failAttempt(err.Error())
		return Status{}, err
	}
	if err := os.Chmod(stagedPath, 0o755); err != nil {
		_ = os.Remove(stagedPath)
		_ = m.failAttempt("failed to chmod staged binary: " + err.Error())
		return Status{}, err
	}

	job := Job{
		TargetVersion:  release.Tag,
		AssetName:      binaryAsset.Name,
		StagedBinary:   stagedPath,
		ExpectedSHA256: actualHash,
		PreparedAt:     m.now().UTC().Unix(),
		ReleaseSource:  binaryAsset.URL,
	}
	if err := m.writeJob(job); err != nil {
		_ = m.failAttempt("failed to persist update job: " + err.Error())
		return Status{}, err
	}

	if err := m.ensureUpdaterUnit(); err != nil {
		_ = m.failAttempt("failed to ensure updater unit: " + err.Error())
		return Status{}, err
	}
	if err := m.systemd.Start(m.updaterUnit); err != nil {
		_ = m.failAttempt("failed to start updater unit: " + err.Error())
		return Status{}, err
	}

	if err := m.updateStatusLocked(func(stored *persistedStatus) {
		stored.InProgress = true
		stored.State = "scheduled"
		stored.Message = "update job scheduled; service restart pending"
		stored.LastError = ""
		stored.LastAttemptedVersion = release.Tag
		stored.LastAttemptAt = m.now().UTC().Unix()
		stored.LatestVersion = release.Tag
	}); err != nil {
		return Status{}, err
	}
	return m.Status()
}

func (m *Manager) resolveRelease(ctx context.Context, tag string) (ReleaseMetadata, error) {
	trimmed := strings.TrimSpace(tag)
	if trimmed == "" {
		return m.github.latestRelease(ctx)
	}
	return m.github.releaseByTag(ctx, trimmed)
}

func (m *Manager) prepareTagDirectory(tag string) (string, error) {
	normalized, err := normalizeTag(tag)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(m.updatesDir, normalized)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func (m *Manager) writeJob(job Job) error {
	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.jobPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.jobPath)
}

func (m *Manager) updateStatusLocked(mutator func(stored *persistedStatus)) error {
	return withFileLock(m.statusLock, func() error {
		stored, err := loadPersistedStatus(m.statusPath)
		if err != nil {
			return err
		}
		mutator(&stored)
		return savePersistedStatus(m.statusPath, stored)
	})
}

func (m *Manager) failAttempt(message string) error {
	return m.updateStatusLocked(func(stored *persistedStatus) {
		stored.InProgress = false
		stored.State = "failed"
		stored.Message = "update failed"
		stored.LastError = strings.TrimSpace(message)
	})
}

func (m *Manager) reconcileStatus() error {
	return m.updateStatusLocked(func(stored *persistedStatus) {
		currentVersion := version.Current().Version
		if stored.InProgress && stored.LastAttemptedVersion != "" && currentVersion == stored.LastAttemptedVersion {
			stored.InProgress = false
			stored.State = "success"
			stored.Message = fmt.Sprintf("updated to %s", currentVersion)
			stored.LastError = ""
			stored.LastSuccessVersion = currentVersion
			stored.LastSuccessAt = m.now().UTC().Unix()
		}
	})
}
