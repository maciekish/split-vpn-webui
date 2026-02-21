//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const integrationWGConfig = `[Interface]
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
Address = 10.250.0.2/32
DNS = 1.1.1.1

[Peer]
PublicKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=
AllowedIPs = 0.0.0.0/0
Endpoint = 1.1.1.1:51820
PersistentKeepalive = 25
`

func TestIntegrationVPNLifecycle(t *testing.T) {
	if os.Getenv("SPLITVPNWEBUI_RUN_INTEGRATION") != "1" {
		t.Skip("set SPLITVPNWEBUI_RUN_INTEGRATION=1 to run integration tests")
	}
	if os.Geteuid() != 0 {
		t.Skip("integration test requires root privileges")
	}

	binaryPath := strings.TrimSpace(os.Getenv("SPLITVPNWEBUI_BIN"))
	if binaryPath == "" {
		binaryPath = filepath.Clean("./split-vpn-webui")
	}
	if _, err := os.Stat(binaryPath); err != nil {
		t.Fatalf("split-vpn-webui binary not found at %s: %v", binaryPath, err)
	}

	addr, err := freeLocalAddr()
	if err != nil {
		t.Fatalf("failed to choose listen address: %v", err)
	}
	baseURL := "http://" + addr
	dataDir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "--systemd", "--data-dir", dataDir, "--addr", addr)
	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start split-vpn-webui: %v", err)
	}
	defer func() {
		cancel()
		_ = cmd.Process.Kill()
		_, _ = ioCopyDiscardAndWait(cmd)
		if t.Failed() {
			t.Logf("server logs:\n%s", logs.String())
		}
	}()

	if err := waitForHTTP(baseURL+"/login", 20*time.Second); err != nil {
		t.Fatalf("server did not become ready: %v", err)
	}

	client, err := authenticatedClient(baseURL)
	if err != nil {
		t.Fatalf("failed to authenticate test client: %v", err)
	}

	vpnName := "integration-wg"
	if err := createVPN(client, baseURL, vpnName, integrationWGConfig); err != nil {
		t.Fatalf("failed to create vpn: %v", err)
	}
	defer func() {
		_ = deleteVPN(client, baseURL, vpnName)
	}()

	if err := postNoBody(client, baseURL+"/api/configs/"+vpnName+"/start", http.StatusAccepted); err != nil {
		t.Fatalf("failed to start vpn: %v", err)
	}

	serviceName := "svpn-" + vpnName + ".service"
	if err := waitForServiceActive(serviceName, 20*time.Second); err != nil {
		t.Fatalf("service %s did not become active: %v", serviceName, err)
	}
}

func authenticatedClient(baseURL string) (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Second, Jar: jar}

	resp, err := client.PostForm(baseURL+"/login", url.Values{"password": {"split-vpn"}})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("login failed with status %d", resp.StatusCode)
	}
	return client, nil
}

func createVPN(client *http.Client, baseURL, name, rawConfig string) error {
	payload := map[string]any{
		"name":   name,
		"type":   "wireguard",
		"config": rawConfig,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := client.Post(baseURL+"/api/vpns", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("create vpn failed with status %d", resp.StatusCode)
	}
	return nil
}

func deleteVPN(client *http.Client, baseURL, name string) error {
	req, err := http.NewRequest(http.MethodDelete, baseURL+"/api/vpns/"+name, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete vpn failed with status %d", resp.StatusCode)
	}
	return nil
}

func postNoBody(client *http.Client, endpoint string, expectedStatus int) error {
	req, err := http.NewRequest(http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != expectedStatus {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

func waitForHTTP(endpoint string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(endpoint)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s", endpoint)
}

func waitForServiceActive(serviceName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := exec.Command("systemctl", "is-active", serviceName).CombinedOutput()
		if err == nil && strings.TrimSpace(string(out)) == "active" {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s to become active", serviceName)
}

func freeLocalAddr() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return "", err
	}
	return addr, nil
}

func ioCopyDiscardAndWait(cmd *exec.Cmd) (int, error) {
	err := cmd.Wait()
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode(), err
	}
	return -1, err
}
