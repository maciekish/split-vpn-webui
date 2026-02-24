package diaglog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerWritesWhenEnabledAndLevelMatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diagnostics.log")
	logger := New(path)
	defer logger.Close()

	if err := logger.Configure(true, "debug"); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}
	logger.Debugf("debug message %d", 42)
	logger.Infof("info message")

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "[DEBUG] debug message 42") {
		t.Fatalf("expected debug line in log: %q", text)
	}
	if !strings.Contains(text, "[INFO] info message") {
		t.Fatalf("expected info line in log: %q", text)
	}
}

func TestManagerRespectsLevelFiltering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diagnostics.log")
	logger := New(path)
	defer logger.Close()

	if err := logger.Configure(true, "warn"); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}
	logger.Debugf("debug hidden")
	logger.Infof("info hidden")
	logger.Warnf("warn shown")

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	text := string(content)
	if strings.Contains(text, "debug hidden") || strings.Contains(text, "info hidden") {
		t.Fatalf("unexpected filtered lines in log: %q", text)
	}
	if !strings.Contains(text, "warn shown") {
		t.Fatalf("expected warning line in log: %q", text)
	}
}

func TestManagerDisableStopsWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diagnostics.log")
	logger := New(path)
	defer logger.Close()

	if err := logger.Configure(true, "debug"); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}
	logger.Infof("before disable")
	if err := logger.Configure(false, "debug"); err != nil {
		t.Fatalf("Configure disable failed: %v", err)
	}
	logger.Errorf("after disable")

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "before disable") {
		t.Fatalf("expected line before disable in log: %q", text)
	}
	if strings.Contains(text, "after disable") {
		t.Fatalf("did not expect line after disable in log: %q", text)
	}
}
