package vpn

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func parseSupportingUploads(payload []SupportingFileUpload) (map[string][]byte, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	uploads := make(map[string][]byte, len(payload))
	for _, item := range payload {
		name, err := sanitizeSupportingFileName(item.Name)
		if err != nil {
			return nil, err
		}
		if _, exists := uploads[name]; exists {
			return nil, fmt.Errorf("%w: duplicate supporting file %q", ErrVPNValidation, name)
		}
		content := strings.TrimSpace(item.ContentBase64)
		if content == "" {
			return nil, fmt.Errorf("%w: supporting file %q has empty content", ErrVPNValidation, name)
		}
		decoded, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			decoded, err = base64.RawStdEncoding.DecodeString(content)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: supporting file %q content is not valid base64", ErrVPNValidation, name)
		}
		if len(decoded) == 0 {
			return nil, fmt.Errorf("%w: supporting file %q has empty content", ErrVPNValidation, name)
		}
		uploads[name] = decoded
	}
	return uploads, nil
}

func validateRequiredSupportingFiles(dir string, required []string, uploads map[string][]byte) error {
	if len(required) == 0 {
		return nil
	}
	for _, fileName := range required {
		if _, ok := uploads[fileName]; ok {
			continue
		}
		if dir != "" && supportingFileExists(dir, fileName) {
			continue
		}
		return fmt.Errorf("%w: openvpn config references supporting file %q, but it was not uploaded", ErrVPNValidation, fileName)
	}
	return nil
}

func writeSupportingUploads(dir string, uploads map[string][]byte) error {
	if len(uploads) == 0 {
		return nil
	}
	names := make([]string, 0, len(uploads))
	for name := range uploads {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := writeFileAtomic(filepath.Join(dir, name), uploads[name], 0o600); err != nil {
			return err
		}
	}
	return nil
}

func supportingFileExists(dir, fileName string) bool {
	info, err := os.Stat(filepath.Join(dir, fileName))
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func listSupportingFiles(dir, configFileName string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "" || name == "vpn.conf" || name == configFileName {
			continue
		}
		files = append(files, name)
	}
	sort.Strings(files)
	return files, nil
}
