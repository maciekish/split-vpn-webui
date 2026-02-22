package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultGitHubAPIBaseURL = "https://api.github.com"
	defaultDownloadTimeout  = 2 * time.Minute
)

// HTTPDoer allows tests to stub HTTP transport.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type githubClient struct {
	repo    string
	baseURL string
	client  HTTPDoer
}

type releaseAPIResponse struct {
	TagName     string            `json:"tag_name"`
	Name        string            `json:"name"`
	Prerelease  bool              `json:"prerelease"`
	PublishedAt string            `json:"published_at"`
	Assets      []releaseAPIAsset `json:"assets"`
}

type releaseAPIAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

func newGitHubClient(repo string, doer HTTPDoer) *githubClient {
	if doer == nil {
		doer = &http.Client{Timeout: 30 * time.Second}
	}
	return &githubClient{
		repo:    repo,
		baseURL: defaultGitHubAPIBaseURL,
		client:  doer,
	}
}

func (c *githubClient) latestRelease(ctx context.Context) (ReleaseMetadata, error) {
	return c.releaseByPath(ctx, "/releases/latest")
}

func (c *githubClient) releaseByTag(ctx context.Context, tag string) (ReleaseMetadata, error) {
	normalized, err := normalizeTag(tag)
	if err != nil {
		return ReleaseMetadata{}, err
	}
	return c.releaseByPath(ctx, "/releases/tags/"+normalized)
}

func (c *githubClient) releaseByPath(ctx context.Context, path string) (ReleaseMetadata, error) {
	url := strings.TrimRight(c.baseURL, "/") + "/repos/" + c.repo + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ReleaseMetadata{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "split-vpn-webui-updater")
	resp, err := c.client.Do(req)
	if err != nil {
		return ReleaseMetadata{}, fmt.Errorf("github release request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ReleaseMetadata{}, fmt.Errorf("github release request returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload releaseAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ReleaseMetadata{}, fmt.Errorf("decode github release response: %w", err)
	}
	meta := ReleaseMetadata{
		Tag:        strings.TrimSpace(payload.TagName),
		Name:       strings.TrimSpace(payload.Name),
		Prerelease: payload.Prerelease,
		Assets:     make([]ReleaseAsset, 0, len(payload.Assets)),
	}
	if payload.PublishedAt != "" {
		if t, err := time.Parse(time.RFC3339, payload.PublishedAt); err == nil {
			meta.PublishedAt = t.UTC()
		}
	}
	for _, asset := range payload.Assets {
		url := strings.TrimSpace(asset.BrowserDownloadURL)
		if url == "" {
			continue
		}
		meta.Assets = append(meta.Assets, ReleaseAsset{
			Name: strings.TrimSpace(asset.Name),
			URL:  url,
			Size: asset.Size,
		})
	}
	if meta.Tag == "" {
		return ReleaseMetadata{}, fmt.Errorf("release metadata missing tag")
	}
	return meta, nil
}

func selectBinaryAsset(release ReleaseMetadata, arch string) (ReleaseAsset, error) {
	normalizedArch := strings.TrimSpace(strings.ToLower(arch))
	for _, asset := range release.Assets {
		name := strings.ToLower(strings.TrimSpace(asset.Name))
		if name == "" {
			continue
		}
		if strings.Contains(name, "sha256") || strings.Contains(name, "checksum") || strings.HasSuffix(name, ".sig") || strings.HasSuffix(name, ".asc") {
			continue
		}
		if strings.Contains(name, "linux-"+normalizedArch) || strings.Contains(name, normalizedArch+"-linux") {
			return asset, nil
		}
	}
	return ReleaseAsset{}, fmt.Errorf("no linux/%s binary asset found in release %s", normalizedArch, release.Tag)
}

func selectChecksumAsset(release ReleaseMetadata) (ReleaseAsset, bool) {
	for _, asset := range release.Assets {
		name := strings.ToLower(strings.TrimSpace(asset.Name))
		if strings.Contains(name, "sha256") || strings.Contains(name, "checksum") {
			return asset, true
		}
	}
	return ReleaseAsset{}, false
}

func downloadFileWithSHA256(ctx context.Context, client HTTPDoer, sourceURL, destPath string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "split-vpn-webui-updater")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("download returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return "", err
	}
	tmpPath := destPath + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(file, hasher), resp.Body); err != nil {
		_ = file.Close()
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func downloadChecksums(ctx context.Context, client HTTPDoer, sourceURL string) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "split-vpn-webui-updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("checksum download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("checksum download returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	result := make(map[string]string)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		sum := strings.TrimSpace(fields[0])
		name := strings.TrimLeft(strings.TrimSpace(fields[len(fields)-1]), "*")
		name = filepath.Base(name)
		if sum != "" && name != "" {
			result[name] = strings.ToLower(sum)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
