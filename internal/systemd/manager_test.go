package systemd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recordingRunner struct {
	calls      [][]string
	runErrs    map[string]error
	outputErrs map[string]error
	outputs    map[string][]byte
}

func (r *recordingRunner) Run(name string, args ...string) error {
	call := append([]string{name}, args...)
	r.calls = append(r.calls, call)
	if err, ok := r.runErrs[joinCall(call)]; ok {
		return err
	}
	return nil
}

func (r *recordingRunner) Output(name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	r.calls = append(r.calls, call)
	out := r.outputs[joinCall(call)]
	if err, ok := r.outputErrs[joinCall(call)]; ok {
		return out, err
	}
	return out, nil
}

func joinCall(parts []string) string {
	return strings.Join(parts, " ")
}

func TestWriteUnitCreatesCanonicalFileAndSymlink(t *testing.T) {
	tempDir := t.TempDir()
	unitsDir := filepath.Join(tempDir, "units")
	systemdDir := filepath.Join(tempDir, "etc-systemd")
	bootPath := filepath.Join(tempDir, "on_boot.sh")
	runner := &recordingRunner{}

	m := NewManagerWithDeps(filepath.Join(tempDir, "data"), unitsDir, systemdDir, bootPath, runner)
	unitContent := "[Unit]\nDescription=test\n"
	if err := m.WriteUnit("svpn-test", unitContent); err != nil {
		t.Fatalf("WriteUnit failed: %v", err)
	}

	canonicalPath := filepath.Join(unitsDir, "svpn-test.service")
	bytes, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatalf("failed reading canonical unit: %v", err)
	}
	if string(bytes) != unitContent {
		t.Fatalf("unexpected canonical content: %q", string(bytes))
	}
	info, err := os.Stat(canonicalPath)
	if err != nil {
		t.Fatalf("failed stating canonical unit: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o644 {
		t.Fatalf("expected canonical mode 0644, got %o", mode)
	}

	linkPath := filepath.Join(systemdDir, "svpn-test.service")
	linkTarget, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("expected symlink at %s: %v", linkPath, err)
	}
	if linkTarget != canonicalPath {
		t.Fatalf("expected symlink to %s, got %s", canonicalPath, linkTarget)
	}

	if len(runner.calls) != 1 || joinCall(runner.calls[0]) != "systemctl daemon-reload" {
		t.Fatalf("unexpected runner calls: %#v", runner.calls)
	}
}

func TestRemoveUnitRemovesCanonicalAndSymlink(t *testing.T) {
	tempDir := t.TempDir()
	unitsDir := filepath.Join(tempDir, "units")
	systemdDir := filepath.Join(tempDir, "etc-systemd")
	runner := &recordingRunner{}
	m := NewManagerWithDeps(filepath.Join(tempDir, "data"), unitsDir, systemdDir, filepath.Join(tempDir, "boot.sh"), runner)

	if err := m.WriteUnit("svpn-test.service", "[Unit]\nDescription=test\n"); err != nil {
		t.Fatalf("WriteUnit failed: %v", err)
	}
	if err := m.RemoveUnit("svpn-test"); err != nil {
		t.Fatalf("RemoveUnit failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(unitsDir, "svpn-test.service")); !os.IsNotExist(err) {
		t.Fatalf("expected canonical unit to be removed, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(systemdDir, "svpn-test.service")); !os.IsNotExist(err) {
		t.Fatalf("expected symlink to be removed, got %v", err)
	}

	if got := joinCall(runner.calls[len(runner.calls)-1]); got != "systemctl daemon-reload" {
		t.Fatalf("expected final command to daemon-reload, got %s", got)
	}
}

func TestServiceCommands(t *testing.T) {
	runner := &recordingRunner{outputs: map[string][]byte{"systemctl is-active svpn-test.service": []byte("active\n")}}
	tempDir := t.TempDir()
	unitsDir := filepath.Join(tempDir, "units")
	systemdDir := filepath.Join(tempDir, "etc-systemd")
	if err := os.MkdirAll(unitsDir, 0o755); err != nil {
		t.Fatalf("mkdir units dir: %v", err)
	}
	if err := os.MkdirAll(systemdDir, 0o755); err != nil {
		t.Fatalf("mkdir systemd dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(unitsDir, "svpn-test.service"), []byte("[Unit]\nDescription=test\n"), 0o644); err != nil {
		t.Fatalf("write canonical unit: %v", err)
	}
	m := NewManagerWithDeps(filepath.Join(tempDir, "data"), unitsDir, systemdDir, filepath.Join(tempDir, "boot.sh"), runner)

	if err := m.Start("svpn-test"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := m.Stop("svpn-test"); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if err := m.Restart("svpn-test"); err != nil {
		t.Fatalf("Restart failed: %v", err)
	}
	if err := m.Enable("svpn-test"); err != nil {
		t.Fatalf("Enable failed: %v", err)
	}
	if err := m.Disable("svpn-test"); err != nil {
		t.Fatalf("Disable failed: %v", err)
	}
	status, err := m.Status("svpn-test")
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if status != "active" {
		t.Fatalf("expected status active, got %q", status)
	}

	for _, expected := range []string{
		"systemctl start svpn-test.service",
		"systemctl stop svpn-test.service",
		"systemctl restart svpn-test.service",
		"systemctl enable svpn-test.service",
		"systemctl disable svpn-test.service",
		"systemctl is-active svpn-test.service",
	} {
		found := false
		for _, call := range runner.calls {
			if joinCall(call) == expected {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected command %q in calls %#v", expected, runner.calls)
		}
	}
}

func TestSystemctlSelfHealsMissingSymlink(t *testing.T) {
	tempDir := t.TempDir()
	unitsDir := filepath.Join(tempDir, "units")
	systemdDir := filepath.Join(tempDir, "etc-systemd")
	if err := os.MkdirAll(unitsDir, 0o755); err != nil {
		t.Fatalf("mkdir units dir: %v", err)
	}
	if err := os.MkdirAll(systemdDir, 0o755); err != nil {
		t.Fatalf("mkdir systemd dir: %v", err)
	}
	canonicalPath := filepath.Join(unitsDir, "svpn-test.service")
	if err := os.WriteFile(canonicalPath, []byte("[Unit]\nDescription=test\n"), 0o644); err != nil {
		t.Fatalf("write canonical unit: %v", err)
	}

	runner := &recordingRunner{}
	m := NewManagerWithDeps(filepath.Join(tempDir, "data"), unitsDir, systemdDir, filepath.Join(tempDir, "boot.sh"), runner)
	if err := m.Start("svpn-test"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	linkPath := filepath.Join(systemdDir, "svpn-test.service")
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("expected symlink to be recreated: %v", err)
	}
	if target != canonicalPath {
		t.Fatalf("expected symlink target %q, got %q", canonicalPath, target)
	}

	if len(runner.calls) < 2 {
		t.Fatalf("expected daemon-reload then start calls, got %#v", runner.calls)
	}
	if got := joinCall(runner.calls[0]); got != "systemctl daemon-reload" {
		t.Fatalf("expected first call daemon-reload, got %s", got)
	}
	if got := joinCall(runner.calls[1]); got != "systemctl start svpn-test.service" {
		t.Fatalf("expected second call start, got %s", got)
	}
}

func TestWriteBootHook(t *testing.T) {
	tempDir := t.TempDir()
	bootPath := filepath.Join(tempDir, "on_boot.d", "10-split-vpn-webui.sh")
	m := NewManagerWithDeps(filepath.Join(tempDir, "data"), filepath.Join(tempDir, "units"), filepath.Join(tempDir, "etc-systemd"), bootPath, &recordingRunner{})

	if err := m.WriteBootHook(); err != nil {
		t.Fatalf("WriteBootHook failed: %v", err)
	}

	content, err := os.ReadFile(bootPath)
	if err != nil {
		t.Fatalf("failed reading boot hook: %v", err)
	}
	text := string(content)
	for _, expected := range []string{
		"#!/bin/bash",
		"svpn-*.service",
		"systemctl daemon-reload",
		"systemctl restart split-vpn-webui.service",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("boot hook missing %q", expected)
		}
	}

	info, err := os.Stat(bootPath)
	if err != nil {
		t.Fatalf("failed stating boot hook: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o755 {
		t.Fatalf("expected mode 0755, got %o", mode)
	}

	if err := m.WriteBootHook(); err != nil {
		t.Fatalf("second WriteBootHook should be idempotent: %v", err)
	}
}

func TestStatusReturnsOutputOnFailure(t *testing.T) {
	runner := &recordingRunner{
		outputErrs: map[string]error{"systemctl is-active broken.service": errors.New("exit 3")},
		outputs:    map[string][]byte{"systemctl is-active broken.service": []byte("inactive\n")},
	}
	m := NewManagerWithDeps("/data", "/data/units", "/etc/systemd/system", "/data/on_boot.d/10-split-vpn-webui.sh", runner)

	status, err := m.Status("broken")
	if err == nil {
		t.Fatalf("expected status error")
	}
	if status != "inactive" {
		t.Fatalf("expected status inactive, got %q", status)
	}
}

func TestStartIncludesSystemctlOutputOnFailure(t *testing.T) {
	runner := &recordingRunner{
		outputErrs: map[string]error{"systemctl start broken.service": errors.New("exit 1")},
		outputs:    map[string][]byte{"systemctl start broken.service": []byte("Job for broken.service failed\n")},
	}
	m := NewManagerWithDeps("/data", "/data/units", "/etc/systemd/system", "/data/on_boot.d/10-split-vpn-webui.sh", runner)

	err := m.Start("broken")
	if err == nil {
		t.Fatalf("expected start error")
	}
	if !strings.Contains(err.Error(), "Job for broken.service failed") {
		t.Fatalf("expected systemctl output in error, got %v", err)
	}
}
