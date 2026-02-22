package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"split-vpn-webui/internal/version"
)

type fakeUnitController struct {
	written map[string]string
	started []string
}

func (f *fakeUnitController) WriteUnit(name, content string) error {
	if f.written == nil {
		f.written = make(map[string]string)
	}
	f.written[name] = content
	return nil
}

func (f *fakeUnitController) Start(name string) error {
	f.started = append(f.started, name)
	return nil
}

func TestCheckUpdatesStatusFromLatestRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/releases/latest") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name":     "v1.2.3",
			"published_at": "2026-02-22T10:00:00Z",
			"assets":       []any{},
		})
	}))
	defer server.Close()

	mgr := newTestManager(t, server, nil)
	status, err := mgr.Check(context.Background(), "")
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if status.LatestVersion != "v1.2.3" {
		t.Fatalf("expected latest version v1.2.3, got %q", status.LatestVersion)
	}
	if !status.UpdateAvailable {
		t.Fatalf("expected update available for dev build")
	}
}

func TestStartUpdateStagesBinaryAndSchedulesUnit(t *testing.T) {
	arch := "amd64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	binaryName := "split-vpn-webui-linux-" + arch
	binaryContent := []byte("new-binary")
	sum := sha256.Sum256(binaryContent)
	sumHex := hex.EncodeToString(sum[:])
	controller := &fakeUnitController{}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/foo/bar/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name":     "v1.2.3",
				"published_at": "2026-02-22T10:00:00Z",
				"assets": []map[string]any{
					{"name": binaryName, "browser_download_url": server.URL + "/assets/" + binaryName},
					{"name": "SHA256SUMS", "browser_download_url": server.URL + "/assets/SHA256SUMS"},
				},
			})
		case "/assets/" + binaryName:
			_, _ = w.Write(binaryContent)
		case "/assets/SHA256SUMS":
			_, _ = fmt.Fprintf(w, "%s  %s\n", sumHex, binaryName)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	mgr := newTestManager(t, server, controller)
	origVersion := version.AppVersion
	version.AppVersion = "v1.0.0"
	t.Cleanup(func() { version.AppVersion = origVersion })

	status, err := mgr.StartUpdate(context.Background(), "")
	if err != nil {
		t.Fatalf("StartUpdate failed: %v", err)
	}
	if status.State != "scheduled" {
		t.Fatalf("expected scheduled state, got %q", status.State)
	}
	if len(controller.started) != 1 || controller.started[0] != mgr.updaterUnit {
		t.Fatalf("expected updater unit start, got %#v", controller.started)
	}
	if _, ok := controller.written[mgr.updaterUnit]; !ok {
		t.Fatalf("expected updater unit content to be written")
	}

	jobBytes, err := os.ReadFile(mgr.jobPath)
	if err != nil {
		t.Fatalf("expected job file: %v", err)
	}
	var job Job
	if err := json.Unmarshal(jobBytes, &job); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if job.TargetVersion != "v1.2.3" {
		t.Fatalf("unexpected target version: %q", job.TargetVersion)
	}
	stagedBytes, err := os.ReadFile(job.StagedBinary)
	if err != nil {
		t.Fatalf("read staged binary: %v", err)
	}
	if string(stagedBytes) != string(binaryContent) {
		t.Fatalf("unexpected staged binary content")
	}
}

func TestStartUpdateRejectsMissingChecksumAsset(t *testing.T) {
	controller := &fakeUnitController{}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/releases/latest") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v1.2.3",
			"assets": []map[string]any{
				{"name": "split-vpn-webui-linux-amd64", "browser_download_url": server.URL + "/assets/bin"},
			},
		})
	}))
	defer server.Close()

	mgr := newTestManager(t, server, controller)
	origVersion := version.AppVersion
	version.AppVersion = "v1.0.0"
	t.Cleanup(func() { version.AppVersion = origVersion })

	if _, err := mgr.StartUpdate(context.Background(), ""); err == nil {
		t.Fatalf("expected missing checksum error")
	}
}

func newTestManager(t *testing.T, server *httptest.Server, controller UnitController) *Manager {
	t.Helper()
	dataDir := t.TempDir()
	binaryPath := filepath.Join(dataDir, "split-vpn-webui")
	if err := os.WriteFile(binaryPath, []byte("old"), 0o755); err != nil {
		t.Fatalf("write test binary: %v", err)
	}
	mgr, err := NewManager(Options{
		Repo:       "foo/bar",
		DataDir:    dataDir,
		BinaryPath: binaryPath,
		Systemd:    controller,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	mgr.github.baseURL = server.URL
	return mgr
}
