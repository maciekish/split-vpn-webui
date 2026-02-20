package systemd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WriteBootHook writes/updates the on-boot script that re-links units after firmware updates.
func (m *Manager) WriteBootHook() error {
	if strings.TrimSpace(m.bootHookPath) == "" {
		return fmt.Errorf("boot hook path is required")
	}
	if strings.TrimSpace(m.dataDir) == "" {
		return fmt.Errorf("data directory is required")
	}
	content := m.bootHookContent()

	if existing, err := os.ReadFile(m.bootHookPath); err == nil {
		if string(existing) == content {
			return os.Chmod(m.bootHookPath, 0o755)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(m.bootHookPath), 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(m.bootHookPath, []byte(content), 0o755); err != nil {
		return err
	}
	return os.Chmod(m.bootHookPath, 0o755)
}

func (m *Manager) bootHookContent() string {
	return fmt.Sprintf(`#!/bin/bash
# /data/on_boot.d/10-split-vpn-webui.sh
set -e

DATA_DIR=%q
UNITS_DIR="${DATA_DIR}/units"
SYSTEMD_DIR=%q

mkdir -p "${UNITS_DIR}"

if [ -f "${UNITS_DIR}/split-vpn-webui.service" ]; then
    ln -sf "${UNITS_DIR}/split-vpn-webui.service" "${SYSTEMD_DIR}/split-vpn-webui.service"
fi

for unit in "${UNITS_DIR}"/svpn-*.service; do
    [ -f "${unit}" ] || continue
    ln -sf "${unit}" "${SYSTEMD_DIR}/$(basename "${unit}")"
done

systemctl daemon-reload
systemctl enable split-vpn-webui.service 2>/dev/null || true
systemctl restart split-vpn-webui.service
`, m.dataDir, m.systemdDir)
}
