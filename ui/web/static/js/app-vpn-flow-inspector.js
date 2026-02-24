(() => {
  window.SplitVPNUI = window.SplitVPNUI || {};

  window.SplitVPNUI.createFlowInspectorController = function createFlowInspectorController(ctx) {
    const {
      flowInspectorModalElement,
      flowInspectorModal,
      flowInspectorTitle,
      flowInspectorStatus,
      flowInspectorSummaryVPN,
      flowInspectorSummaryFlows,
      flowInspectorSummaryTotal,
      flowInspectorUpdatedAt,
      flowInspectorTableBody,
      fetchJSON,
      setStatus,
      formatThroughput,
      formatBytes,
    } = ctx || {};

    if (
      !flowInspectorModalElement ||
      !flowInspectorModal ||
      !flowInspectorTitle ||
      !flowInspectorStatus ||
      !flowInspectorSummaryVPN ||
      !flowInspectorSummaryFlows ||
      !flowInspectorSummaryTotal ||
      !flowInspectorUpdatedAt ||
      !flowInspectorTableBody ||
      typeof fetchJSON !== 'function'
    ) {
      return null;
    }

    const fallbackThroughput = (value) => {
      const numeric = Number(value || 0);
      if (!Number.isFinite(numeric) || numeric <= 0) {
        return '0 bps';
      }
      if (numeric >= 1_000_000_000) {
        return `${(numeric / 1_000_000_000).toFixed(2)} Gbps`;
      }
      if (numeric >= 1_000_000) {
        return `${(numeric / 1_000_000).toFixed(2)} Mbps`;
      }
      if (numeric >= 1_000) {
        return `${(numeric / 1_000).toFixed(2)} Kbps`;
      }
      return `${numeric.toFixed(0)} bps`;
    };
    const fallbackBytes = (value) => {
      const numeric = Number(value || 0);
      if (!Number.isFinite(numeric) || numeric <= 0) {
        return '0 B';
      }
      const units = ['B', 'KB', 'MB', 'GB', 'TB'];
      let idx = 0;
      let scaled = numeric;
      while (scaled >= 1024 && idx < units.length - 1) {
        scaled /= 1024;
        idx += 1;
      }
      return `${scaled.toFixed(scaled >= 10 ? 1 : 2)} ${units[idx]}`;
    };

    const toThroughput = typeof formatThroughput === 'function' ? formatThroughput : fallbackThroughput;
    const toBytes = typeof formatBytes === 'function' ? formatBytes : fallbackBytes;
    const state = {
      vpnName: '',
      sessionID: '',
      pollIntervalMS: 2000,
      pollTimer: null,
      pollInFlight: false,
      recoveryInFlight: false,
      running: false,
      rows: [],
      sortKey: 'total',
      sortDirection: 'desc',
    };

    flowInspectorModalElement.addEventListener('hidden.bs.modal', () => {
      stop();
    });

    flowInspectorModalElement.addEventListener('click', (event) => {
      const target = event.target.closest('.flow-sort');
      if (!target) {
        return;
      }
      const sortKey = String(target.getAttribute('data-sort-key') || '').trim();
      if (!sortKey) {
        return;
      }
      if (state.sortKey === sortKey) {
        state.sortDirection = state.sortDirection === 'asc' ? 'desc' : 'asc';
      } else {
        state.sortKey = sortKey;
        state.sortDirection = sortKey === 'source' || sortKey === 'destination' ? 'asc' : 'desc';
      }
      renderRows(state.rows);
    });

    async function open(vpnName) {
      const name = String(vpnName || '').trim();
      if (!name) {
        return;
      }
      await stop();
      resetUI(name);
      state.running = true;
      state.vpnName = name;
      flowInspectorModal.show();
      setInspectorStatus('Starting flow inspection session…', false);
      try {
        const response = await fetchJSON(`/api/vpns/${encodeURIComponent(name)}/flow-inspector/start`, {
          method: 'POST',
        });
        const startedSessionID = String(response?.sessionId || '').trim();
        if (!state.running || state.vpnName !== name) {
          if (startedSessionID) {
            try {
              await fetchJSON(`/api/vpns/${encodeURIComponent(name)}/flow-inspector/${encodeURIComponent(startedSessionID)}/stop`, {
                method: 'POST',
              });
            } catch (err) {
              // Ignore stop races when modal closes during startup.
            }
          }
          return;
        }
        state.sessionID = String(response?.sessionId || '').trim();
        const intervalSeconds = Number(response?.pollIntervalSeconds || 2);
        state.pollIntervalMS = Math.max(1, Math.trunc(intervalSeconds)) * 1000;
        renderSnapshot(response?.snapshot || {});
        startPolling();
      } catch (err) {
        state.running = false;
        const message = err.message || 'Failed to start flow inspector.';
        setInspectorStatus(message, true);
        if (typeof setStatus === 'function') {
          setStatus(message, true);
        }
      }
    }

    async function stop() {
      if (state.pollTimer) {
        clearInterval(state.pollTimer);
        state.pollTimer = null;
      }
      const activeSession = state.sessionID;
      const activeVPN = state.vpnName;
      state.running = false;
      state.sessionID = '';
      state.vpnName = '';
      state.rows = [];
      state.pollInFlight = false;
      state.recoveryInFlight = false;
      if (activeSession && activeVPN) {
        try {
          await fetchJSON(`/api/vpns/${encodeURIComponent(activeVPN)}/flow-inspector/${encodeURIComponent(activeSession)}/stop`, {
            method: 'POST',
          });
        } catch (err) {
          // Ignore stop failures during modal close.
        }
      }
      flowInspectorTableBody.innerHTML = '<tr><td class="text-body-secondary small" colspan="5">No active flow inspection session.</td></tr>';
      flowInspectorUpdatedAt.textContent = 'Updated: –';
      flowInspectorSummaryFlows.textContent = 'Flows: 0';
      flowInspectorSummaryTotal.textContent = 'Session Data: 0 B';
      flowInspectorStatus.classList.add('d-none');
    }

    function resetUI(vpnName) {
      state.rows = [];
      state.sortKey = 'total';
      state.sortDirection = 'desc';
      flowInspectorTitle.innerHTML = `<i class=\"bi bi-search me-2\"></i>VPN Flow Inspector — ${escapeHTML(vpnName)}`;
      flowInspectorSummaryVPN.textContent = `VPN: ${vpnName}`;
      flowInspectorSummaryFlows.textContent = 'Flows: 0';
      flowInspectorSummaryTotal.textContent = 'Session Data: 0 B';
      flowInspectorUpdatedAt.textContent = 'Updated: loading…';
      flowInspectorTableBody.innerHTML = '<tr><td class="text-body-secondary small" colspan="5">Loading flow data…</td></tr>';
    }

    function startPolling() {
      if (!state.running || !state.vpnName || !state.sessionID) {
        return;
      }
      if (state.pollTimer) {
        clearInterval(state.pollTimer);
      }
      state.pollTimer = setInterval(() => {
        poll().catch(() => {});
      }, state.pollIntervalMS);
    }

    async function poll() {
      if (!state.running || !state.vpnName || !state.sessionID || state.pollInFlight) {
        return;
      }
      state.pollInFlight = true;
      try {
        const response = await fetchJSON(`/api/vpns/${encodeURIComponent(state.vpnName)}/flow-inspector/${encodeURIComponent(state.sessionID)}`);
        renderSnapshot(response?.snapshot || {});
      } catch (err) {
        const message = err.message || 'Failed to refresh flow inspector.';
        if (message.toLowerCase().includes('session not found')) {
          await recoverSession();
          return;
        }
        setInspectorStatus(message, true);
        if (typeof setStatus === 'function') {
          setStatus(message, true);
        }
      } finally {
        state.pollInFlight = false;
      }
    }

    async function recoverSession() {
      if (state.recoveryInFlight || !state.running || !state.vpnName) {
        return;
      }
      state.recoveryInFlight = true;
      try {
        const response = await fetchJSON(`/api/vpns/${encodeURIComponent(state.vpnName)}/flow-inspector/start`, {
          method: 'POST',
        });
        const newSessionID = String(response?.sessionId || '').trim();
        if (!newSessionID) {
          throw new Error('Flow inspector recovery did not return a session.');
        }
        state.sessionID = newSessionID;
        const intervalSeconds = Number(response?.pollIntervalSeconds || 2);
        state.pollIntervalMS = Math.max(1, Math.trunc(intervalSeconds)) * 1000;
        renderSnapshot(response?.snapshot || {});
        startPolling();
        setInspectorStatus('Flow inspector session recovered.', false);
      } catch (err) {
        const message = err.message || 'Flow inspector recovery failed.';
        setInspectorStatus(message, true);
        if (typeof setStatus === 'function') {
          setStatus(message, true);
        }
      } finally {
        state.recoveryInFlight = false;
      }
    }

    function renderSnapshot(snapshot) {
      const generatedAt = snapshot?.generatedAt ? new Date(snapshot.generatedAt) : null;
      if (generatedAt && !Number.isNaN(generatedAt.getTime())) {
        flowInspectorUpdatedAt.textContent = `Updated: ${generatedAt.toLocaleTimeString()}`;
      } else {
        flowInspectorUpdatedAt.textContent = 'Updated: now';
      }
      const totals = snapshot?.totals || {};
      const uploadBytes = Number(totals.uploadBytes || 0);
      const downloadBytes = Number(totals.downloadBytes || 0);
      const totalBytes = Number(totals.totalBytes || (uploadBytes + downloadBytes));
      state.rows = Array.isArray(snapshot?.flows) ? snapshot.flows : [];
      flowInspectorSummaryFlows.textContent = `Flows: ${state.rows.length}`;
      flowInspectorSummaryTotal.textContent = `Session Data: ${toBytes(totalBytes)} (↓ ${toBytes(downloadBytes)} / ↑ ${toBytes(uploadBytes)})`;
      renderRows(state.rows);
      if (state.rows.length > 0) {
        setInspectorStatus(`Flow inspector session active for ${snapshot?.vpnName || 'VPN'}.`, false);
      } else {
        setInspectorStatus('No matching VPN flows at the moment.', false);
      }
    }

    function renderRows(rows) {
      const list = Array.isArray(rows) ? [...rows] : [];
      list.sort((left, right) => compareRows(left, right, state.sortKey, state.sortDirection));
      flowInspectorTableBody.innerHTML = '';
      if (list.length === 0) {
        flowInspectorTableBody.innerHTML = '<tr><td class="text-body-secondary small" colspan="5">No matching VPN flows at this time.</td></tr>';
        return;
      }
      list.forEach((row) => {
        const tr = document.createElement('tr');
        tr.innerHTML = `
          <td>${renderSourceCell(row)}</td>
          <td>${renderDestinationCell(row)}</td>
          <td class=\"text-end text-primary fw-semibold\">${toThroughput(Number(row.downloadBps || 0))}</td>
          <td class=\"text-end text-danger fw-semibold\">${toThroughput(Number(row.uploadBps || 0))}</td>
          <td class=\"text-end\">${toBytes(Number(row.totalBytes || 0))}</td>
        `;
        flowInspectorTableBody.appendChild(tr);
      });
    }

    function renderSourceCell(row) {
      const sourceName = String(row?.sourceDeviceName || '').trim();
      const sourceMAC = String(row?.sourceMac || '').trim();
      const sourceIP = String(row?.sourceIp || '').trim();
      const sourcePort = Number(row?.sourcePort || 0);
      const sourceInterface = String(row?.sourceInterface || '').trim();
      const headingParts = [];
      if (sourceName) {
        headingParts.push(sourceName);
      }
      if (sourceMAC) {
        headingParts.push(sourceMAC);
      }
      const heading = headingParts.length ? headingParts.join(' • ') : (sourceIP || 'Unknown source');
      const endpoint = sourcePort > 0 ? `${sourceIP}:${sourcePort}` : sourceIP;
      const ifaceMeta = sourceInterface ? ` via ${sourceInterface}` : '';
      return `
        <div class=\"fw-semibold\">${escapeHTML(heading)}</div>
        <div class=\"small text-body-secondary\">${escapeHTML(endpoint || 'n/a')}${escapeHTML(ifaceMeta)}</div>
      `;
    }

    function renderDestinationCell(row) {
      const destinationDomain = String(row?.destinationDomain || '').trim();
      const destinationIP = String(row?.destinationIp || '').trim();
      const destinationPort = Number(row?.destinationPort || 0);
      const destinationEndpoint = destinationPort > 0
        ? `${destinationIP}:${destinationPort}`
        : destinationIP;
      const heading = destinationDomain
        ? `${destinationDomain}${destinationPort > 0 ? `:${destinationPort}` : ''}`
        : destinationEndpoint;
      if (destinationDomain) {
        return `
          <div class=\"fw-semibold\">${escapeHTML(heading)}</div>
          <div class=\"small text-body-secondary\">${escapeHTML(destinationEndpoint || 'n/a')}</div>
        `;
      }
      return `<div class=\"fw-semibold\">${escapeHTML(heading || 'n/a')}</div>`;
    }

    function compareRows(left, right, sortKey, direction) {
      const order = direction === 'asc' ? 1 : -1;
      switch (sortKey) {
        case 'source': {
          const a = `${String(left?.sourceDeviceName || '').toLowerCase()} ${String(left?.sourceIp || '').toLowerCase()}:${Number(left?.sourcePort || 0)}`;
          const b = `${String(right?.sourceDeviceName || '').toLowerCase()} ${String(right?.sourceIp || '').toLowerCase()}:${Number(right?.sourcePort || 0)}`;
          return a.localeCompare(b) * order;
        }
        case 'destination': {
          const a = `${String(left?.destinationDomain || '').toLowerCase()} ${String(left?.destinationIp || '').toLowerCase()}:${Number(left?.destinationPort || 0)}`;
          const b = `${String(right?.destinationDomain || '').toLowerCase()} ${String(right?.destinationIp || '').toLowerCase()}:${Number(right?.destinationPort || 0)}`;
          return a.localeCompare(b) * order;
        }
        case 'download':
          return compareNumber(Number(left?.downloadBps || 0), Number(right?.downloadBps || 0), order);
        case 'upload':
          return compareNumber(Number(left?.uploadBps || 0), Number(right?.uploadBps || 0), order);
        case 'total':
        default:
          return compareNumber(Number(left?.totalBytes || 0), Number(right?.totalBytes || 0), order);
      }
    }

    function compareNumber(left, right, order) {
      if (left === right) {
        return 0;
      }
      return left > right ? order : -order;
    }

    function setInspectorStatus(message, isError) {
      flowInspectorStatus.classList.remove('d-none', 'alert-success', 'alert-danger');
      flowInspectorStatus.classList.add(isError ? 'alert-danger' : 'alert-success');
      flowInspectorStatus.textContent = message || '';
    }

    function escapeHTML(value) {
      return String(value || '')
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    return { open, stop };
  };
})();
