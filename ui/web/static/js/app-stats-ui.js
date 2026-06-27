(() => {
  window.SplitVPNUI = window.SplitVPNUI || {};

  function formatCPUUsage(usage) {
    if (!usage) {
      return '–';
    }
    if (usage.source === 'kernel') {
      return 'kernel';
    }
    if (!usage.available) {
      return '–';
    }
    const percent = Number(usage.percent);
    if (!Number.isFinite(percent)) {
      return '–';
    }
    return `${percent.toFixed(percent >= 10 ? 1 : 2)}%`;
  }

  function formatLoadValue(value) {
    return value.toFixed(2);
  }

  function createStatsUI(ctx) {
    const wanLabel = ctx?.wanLabel || null;
    const loadLabel = ctx?.loadLabel || null;
    const updatedAt = ctx?.updatedAt || null;
    const formatThroughput = ctx?.formatThroughput || ((value) => `${Number(value || 0)} bps`);
    const formatBytes = ctx?.formatBytes || ((value) => `${Number(value || 0)} B`);

    function updateTimestamp(timestamp) {
      if (!updatedAt) {
        return;
      }
      if (!timestamp) {
        updatedAt.textContent = '–';
        return;
      }
      const time = new Date(timestamp);
      updatedAt.textContent = 'Updated ' + time.toLocaleTimeString();
    }

    function updateLoadLabel(load) {
      if (!loadLabel) {
        return;
      }
      if (!load) {
        loadLabel.textContent = '';
        loadLabel.removeAttribute('title');
        return;
      }
      const values = [load.load1, load.load5, load.load15].map((value) => Number(value));
      if (values.some((value) => !Number.isFinite(value))) {
        loadLabel.textContent = '';
        loadLabel.removeAttribute('title');
        return;
      }
      loadLabel.textContent = values.map(formatLoadValue).join(' ');
      loadLabel.title = 'System load average: 1m 5m 15m';
    }

    function updateWanLabel(stats) {
      if (!wanLabel) {
        return;
      }
      if (!stats) {
        wanLabel.textContent = '';
        return;
      }
      const wan = (stats.interfaces || []).find((iface) => iface.type === 'wan');
      if (!wan) {
        wanLabel.textContent = 'WAN interface not detected';
        return;
      }
      const downloadValue = Number.isFinite(wan.currentRxThroughput) ? wan.currentRxThroughput : 0;
      const uploadValue = Number.isFinite(wan.currentTxThroughput) ? wan.currentTxThroughput : 0;
      const combined = Number.isFinite(wan.currentThroughput)
        ? wan.currentThroughput
        : stats.wanCorrectedThroughput || 0;
      const throughputLabel = downloadValue > 0 || uploadValue > 0
        ? `↓ ${formatThroughput(downloadValue)} • ↑ ${formatThroughput(uploadValue)}`
        : formatThroughput(combined);
      const total = formatBytes(wan.totalBytes || stats.wanCorrectedBytes || 0);
      wanLabel.textContent = `${wan.name || 'WAN'} (${wan.interface || 'n/a'}) • ${throughputLabel} • ${total}`;
    }

    function sortInterfaces(interfaces = []) {
      return [...interfaces].sort((a, b) => {
        const typeA = a?.type || '';
        const typeB = b?.type || '';
        if (typeA !== typeB) {
          if (typeA === 'wan') {
            return -1;
          }
          if (typeB === 'wan') {
            return 1;
          }
        }
        const nameA = (a?.name || '').toLowerCase();
        const nameB = (b?.name || '').toLowerCase();
        return nameA.localeCompare(nameB);
      });
    }

    return {
      updateTimestamp,
      updateLoadLabel,
      updateWanLabel,
      sortInterfaces,
    };
  }

  window.SplitVPNUI.formatCPUUsage = formatCPUUsage;
  window.SplitVPNUI.createStatsUI = createStatsUI;
})();
