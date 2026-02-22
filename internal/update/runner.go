package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	updateRunnerLogName    = "split-vpn-webui-update.log"
	applyRestartTimeout    = 45 * time.Second
	applyActiveWaitTimeout = 30 * time.Second
)

// RunPendingJob executes the staged update job. This is invoked by
// `split-vpn-webui --self-update-run` under a dedicated systemd oneshot unit.
func (m *Manager) RunPendingJob(ctx context.Context) error {
	job, err := m.readJob()
	if err != nil {
		_ = m.failAttempt("failed to read update job: " + err.Error())
		return err
	}
	if err := m.updateStatusLocked(func(stored *persistedStatus) {
		stored.InProgress = true
		stored.State = "applying"
		stored.Message = fmt.Sprintf("applying %s", job.TargetVersion)
		stored.LastError = ""
		stored.LastAttemptedVersion = job.TargetVersion
		stored.LastAttemptAt = m.now().UTC().Unix()
	}); err != nil {
		return err
	}
	if err := m.applyJob(ctx, job); err != nil {
		_ = m.failAttempt(err.Error())
		return err
	}
	if err := m.updateStatusLocked(func(stored *persistedStatus) {
		stored.InProgress = false
		stored.State = "success"
		stored.Message = fmt.Sprintf("updated to %s", job.TargetVersion)
		stored.LastError = ""
		stored.LastSuccessVersion = job.TargetVersion
		stored.LastSuccessAt = m.now().UTC().Unix()
		stored.LastAttemptedVersion = job.TargetVersion
		stored.LastAttemptAt = m.now().UTC().Unix()
	}); err != nil {
		return err
	}
	_ = os.Remove(m.jobPath)
	return nil
}

func (m *Manager) applyJob(ctx context.Context, job Job) error {
	stagedPath, err := m.validateStagedPath(job.StagedBinary)
	if err != nil {
		return err
	}
	actualHash, err := fileSHA256(stagedPath)
	if err != nil {
		return fmt.Errorf("hash staged binary: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(job.ExpectedSHA256), strings.TrimSpace(actualHash)) {
		return fmt.Errorf("staged binary checksum mismatch")
	}

	if err := os.MkdirAll(filepath.Dir(m.binaryPath), 0o755); err != nil {
		return err
	}
	newPath := m.binaryPath + ".new"
	backupPath := m.binaryPath + ".previous"

	if err := copyFile(stagedPath, newPath, 0o755); err != nil {
		return fmt.Errorf("prepare new binary: %w", err)
	}
	if err := copyFile(m.binaryPath, backupPath, 0o755); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("backup current binary: %w", err)
	}
	if err := os.Rename(newPath, m.binaryPath); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("activate new binary: %w", err)
	}

	if err := m.restartAndWait(ctx); err != nil {
		restoreErr := copyFile(backupPath, m.binaryPath+".rollback", 0o755)
		if restoreErr == nil {
			if errRename := os.Rename(m.binaryPath+".rollback", m.binaryPath); errRename != nil {
				restoreErr = errRename
			}
		}
		_, _ = m.runSystemctl(ctx, "restart", m.serviceName)
		if restoreErr != nil {
			return fmt.Errorf("restart failed (%v) and rollback failed (%v)", err, restoreErr)
		}
		return fmt.Errorf("restart failed after update and rollback was applied: %w", err)
	}
	_ = os.Remove(stagedPath)
	return nil
}

func (m *Manager) restartAndWait(ctx context.Context) error {
	restartCtx, cancelRestart := context.WithTimeout(ctx, applyRestartTimeout)
	defer cancelRestart()
	if output, err := m.runSystemctl(restartCtx, "restart", m.serviceName); err != nil {
		return fmt.Errorf("systemctl restart failed: %s: %w", output, err)
	}
	deadline := m.now().Add(applyActiveWaitTimeout)
	var lastState string
	for m.now().Before(deadline) {
		statusCtx, cancelStatus := context.WithTimeout(ctx, 8*time.Second)
		output, err := m.runSystemctl(statusCtx, "is-active", m.serviceName)
		cancelStatus()
		state := strings.TrimSpace(output)
		lastState = state
		if err == nil && state == "active" {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	if lastState == "" {
		lastState = "unknown"
	}
	return fmt.Errorf("service did not become active (last state: %s)", lastState)
}

func (m *Manager) runSystemctl(ctx context.Context, args ...string) (string, error) {
	cmdArgs := append([]string{}, args...)
	cmd := exec.CommandContext(ctx, "systemctl", cmdArgs...)
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func (m *Manager) readJob() (Job, error) {
	bytes, err := os.ReadFile(m.jobPath)
	if err != nil {
		return Job{}, err
	}
	var job Job
	if err := json.Unmarshal(bytes, &job); err != nil {
		return Job{}, err
	}
	if strings.TrimSpace(job.TargetVersion) == "" || strings.TrimSpace(job.StagedBinary) == "" {
		return Job{}, fmt.Errorf("update job is missing required fields")
	}
	return job, nil
}

func (m *Manager) validateStagedPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("staged binary path is required")
	}
	cleaned := filepath.Clean(path)
	updatesRoot := filepath.Clean(m.updatesDir) + string(os.PathSeparator)
	if !strings.HasPrefix(cleaned, updatesRoot) {
		return "", fmt.Errorf("staged binary path must be under %s", m.updatesDir)
	}
	info, err := os.Stat(cleaned)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("staged binary path points to a directory")
	}
	return cleaned, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func copyFile(srcPath, dstPath string, mode os.FileMode) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = os.Remove(dstPath)
		return err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(dstPath)
		return err
	}
	return os.Chmod(dstPath, mode)
}

func (m *Manager) ensureUpdaterUnit() error {
	logPath := filepath.Join(m.dataDir, "logs", updateRunnerLogName)
	content := fmt.Sprintf(`[Unit]
Description=Split VPN Web UI Self-Updater
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=%s --self-update-run --data-dir %s
WorkingDirectory=%s
StandardOutput=append:%s
StandardError=append:%s

[Install]
WantedBy=multi-user.target
`, m.binaryPath, m.dataDir, m.dataDir, logPath, logPath)
	return m.systemd.WriteUnit(m.updaterUnit, content)
}
