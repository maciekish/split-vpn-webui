(() => {
  const interfaceGrid = document.getElementById('interface-grid');
  const vpnTableBody = document.querySelector('#vpn-table tbody');
  const wanLabel = document.getElementById('wan-label');
  const updatedAt = document.getElementById('updated-at');
  const errorIndicator = document.getElementById('error-indicator');
  const refreshButton = document.getElementById('refresh-configs');
  const settingsButton = document.getElementById('open-settings');
  const configModalElement = document.getElementById('configModal');
  const configModal = new bootstrap.Modal(configModalElement);
  const configEditor = document.getElementById('config-editor');
  const configModalName = document.getElementById('config-modal-name');
  const configModalPath = document.getElementById('config-modal-path');
  const configModalStatus = document.getElementById('config-modal-status');
  const saveConfigButton = document.getElementById('save-config');
  const settingsModalElement = document.getElementById('settingsModal');
  const settingsModal = new bootstrap.Modal(settingsModalElement);
  const listenSelect = document.getElementById('listen-interface');
  const wanSelect = document.getElementById('wan-interface');
  const saveSettingsButton = document.getElementById('save-settings');

  const palette = ['#3b82f6', '#22d3ee', '#f97316', '#a855f7', '#f43f5e', '#14b8a6', '#eab308'];
  const downloadColor = '#60a5fa';
  const downloadFill = 'rgba(96, 165, 250, 0.15)';
  const uploadColor = '#f87171';
  const uploadFill = 'rgba(248, 113, 113, 0.15)';
  const controlsEnabled = false;
  const CONTROL_DISABLED_MESSAGE = 'VPN control is coming in a future release. This build is monitoring only.';
  const controlDisabledAttributes = controlsEnabled ? '' : ` disabled title="${CONTROL_DISABLED_MESSAGE}"`;
  let stream;
  let reconnectTimer;
  const state = {
    interfaceCharts: new Map(),
    throughputGauge: null,
    totalGauge: null,
    currentConfig: null,
    availableInterfaces: [],
    settings: null,
    gaugeColors: new Map(),
  };

  if (!controlsEnabled) {
    configEditor.setAttribute('readonly', 'readonly');
    configEditor.readOnly = true;
    configEditor.title = CONTROL_DISABLED_MESSAGE;
    saveConfigButton.disabled = true;
    saveConfigButton.title = CONTROL_DISABLED_MESSAGE;
  }

  function connectStream() {
    if (document.hidden) {
      return;
    }
    clearTimeout(reconnectTimer);
    if (stream) {
      stream.close();
    }
    stream = new EventSource('/api/stream');
    stream.onmessage = (event) => {
      try {
        const payload = JSON.parse(event.data);
        updateUI(payload);
      } catch (err) {
        console.error('Failed to parse payload', err);
      }
    };
    stream.onerror = () => {
      if (stream) {
        stream.close();
        stream = null;
      }
      reconnectTimer = setTimeout(connectStream, 5000);
    };
  }

  document.addEventListener('visibilitychange', () => {
    if (document.hidden) {
      if (stream) {
        stream.close();
        stream = null;
      }
    } else {
      connectStream();
    }
  });

  refreshButton.addEventListener('click', async () => {
    try {
      await fetchJSON('/api/reload', { method: 'POST' });
      setStatus('Reloaded configuration from disk.', false);
    } catch (err) {
      setStatus(err.message, true);
    }
  });

  settingsButton.addEventListener('click', async () => {
    await openSettingsModal();
  });

  saveSettingsButton.addEventListener('click', async () => {
    const payload = {
      listenInterface: listenSelect.value || '',
      wanInterface: wanSelect.value || '',
    };
    saveSettingsButton.disabled = true;
    try {
      await fetchJSON('/api/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      state.settings = { ...state.settings, ...payload };
      setStatus('Settings saved.', false);
      settingsModal.hide();
    } catch (err) {
      setStatus(err.message, true);
    } finally {
      saveSettingsButton.disabled = false;
    }
  });

  saveConfigButton.addEventListener('click', async () => {
    if (!controlsEnabled) {
      setStatus(CONTROL_DISABLED_MESSAGE, false);
      return;
    }
    if (!state.currentConfig) {
      return;
    }
    saveConfigButton.disabled = true;
    try {
      await fetchJSON(`/api/configs/${encodeURIComponent(state.currentConfig)}/file`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: configEditor.value }),
      });
      setStatus('Configuration saved.', false);
      configModal.hide();
    } catch (err) {
      setStatus(err.message, true);
    } finally {
      saveConfigButton.disabled = false;
    }
  });

  vpnTableBody.addEventListener('click', async (event) => {
    const target = event.target.closest('[data-action]');
    if (!target) {
      return;
    }
    const name = target.getAttribute('data-name');
    if (!name) {
      return;
    }
    const action = target.getAttribute('data-action');
    if (action === 'start' || action === 'stop') {
      if (!controlsEnabled) {
        setStatus(CONTROL_DISABLED_MESSAGE, false);
        return;
      }
      target.disabled = true;
      try {
        await fetchJSON(`/api/configs/${encodeURIComponent(name)}/${action}`, { method: 'POST' });
        setStatus(`${action === 'start' ? 'Starting' : 'Stopping'} ${name}...`, false);
      } catch (err) {
        setStatus(err.message, true);
      } finally {
        target.disabled = false;
      }
      return;
    }
    if (action === 'edit') {
      openConfigModal(name, target.dataset.path || '');
    }
  });

  vpnTableBody.addEventListener('change', async (event) => {
    const target = event.target;
    if (target.matches('input[data-action="autostart"]')) {
      if (!controlsEnabled) {
        setStatus(CONTROL_DISABLED_MESSAGE, false);
        target.checked = !target.checked;
        return;
      }
      const name = target.getAttribute('data-name');
      try {
        await fetchJSON(`/api/configs/${encodeURIComponent(name)}/autostart`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ enabled: target.checked }),
        });
        setStatus(`Autostart ${target.checked ? 'enabled' : 'disabled'} for ${name}.`, false);
      } catch (err) {
        setStatus(err.message, true);
        target.checked = !target.checked;
      }
    }
  });

  async function openConfigModal(name, path) {
    try {
      const data = await fetchJSON(`/api/configs/${encodeURIComponent(name)}/file`);
      state.currentConfig = name;
      configModalName.textContent = name;
      configModalPath.textContent = path;
      const loadedAt = new Date().toLocaleTimeString();
      if (controlsEnabled) {
        configModalStatus.textContent = 'Loaded at ' + loadedAt;
      } else {
        configModalStatus.textContent = 'Read-only preview · loaded at ' + loadedAt;
      }
      configEditor.value = data.content || '';
      configModal.show();
    } catch (err) {
      setStatus(err.message, true);
    }
  }

  async function openSettingsModal() {
    try {
      const data = await fetchJSON('/api/settings');
      state.settings = data.settings || { listenInterface: '', wanInterface: '' };
      state.availableInterfaces = Array.isArray(data.interfaces) ? data.interfaces : [];
      populateSettingsForm();
      settingsModal.show();
    } catch (err) {
      setStatus(err.message, true);
    }
  }

  function populateSettingsForm() {
    populateInterfaceSelect(
      listenSelect,
      state.availableInterfaces,
      state.settings?.listenInterface || '',
      'All interfaces (0.0.0.0)'
    );
    populateInterfaceSelect(
      wanSelect,
      state.availableInterfaces,
      state.settings?.wanInterface || '',
      'Automatic detection'
    );
  }

  function populateInterfaceSelect(select, interfaces, selected, emptyLabel) {
    select.innerHTML = '';
    const autoOption = document.createElement('option');
    autoOption.value = '';
    autoOption.textContent = emptyLabel;
    select.appendChild(autoOption);

    const seen = new Set();
    interfaces.forEach((iface) => {
      if (!iface || !iface.name || seen.has(iface.name)) {
        return;
      }
      seen.add(iface.name);
      const option = document.createElement('option');
      option.value = iface.name;
      option.textContent = formatInterfaceLabel(iface);
      if (iface.name === selected) {
        option.selected = true;
      }
      select.appendChild(option);
    });

    if (selected && !seen.has(selected)) {
      const fallback = document.createElement('option');
      fallback.value = selected;
      fallback.textContent = `${selected} (unavailable)`;
      fallback.selected = true;
      select.appendChild(fallback);
    }

    select.value = selected || '';
  }

  function formatInterfaceLabel(iface) {
    const addresses = Array.isArray(iface.addresses) ? iface.addresses : [];
    if (addresses.length === 0) {
      return `${iface.name} (no address)`;
    }
    return `${iface.name} (${addresses.join(', ')})`;
  }

  function updateUI(payload) {
    updateTimestamp(payload.stats?.generatedAt);
    updateWanLabel(payload.stats);
    updateErrors(payload.errors);
    const interfaces = sortInterfaces(payload.stats?.interfaces || []);
    updateGauges(interfaces);
    updateInterfaceCards(interfaces, payload.configs || [], payload.latency || []);
    updateVpnTable(payload.configs || [], payload.latency || [], interfaces);
  }

  function updateTimestamp(timestamp) {
    if (!timestamp) {
      updatedAt.textContent = '–';
      return;
    }
    const time = new Date(timestamp);
    updatedAt.textContent = 'Updated ' + time.toLocaleTimeString();
  }

  function updateWanLabel(stats) {
    if (!stats) {
      wanLabel.textContent = '';
      return;
    }
    const wan = (stats.interfaces || []).find((iface) => iface.type === 'wan');
    if (!wan) {
      wanLabel.textContent = 'WAN interface not detected';
      return;
    }
    const downloadValue = Number.isFinite(wan.currentRxThroughput)
      ? wan.currentRxThroughput
      : 0;
    const uploadValue = Number.isFinite(wan.currentTxThroughput)
      ? wan.currentTxThroughput
      : 0;
    const combined = Number.isFinite(wan.currentThroughput)
      ? wan.currentThroughput
      : stats.wanCorrectedThroughput || 0;
    let throughputLabel;
    if (downloadValue > 0 || uploadValue > 0) {
      throughputLabel = `↓ ${formatThroughput(downloadValue)} • ↑ ${formatThroughput(uploadValue)}`;
    } else {
      throughputLabel = formatThroughput(combined);
    }
    const total = formatBytes(wan.totalBytes || stats.wanCorrectedBytes || 0);
    wanLabel.textContent = `${wan.name || 'WAN'} (${wan.interface || 'n/a'}) • ${throughputLabel} • ${total}`;
  }

  function updateErrors(errors = {}) {
    const entries = Object.entries(errors).filter(([, value]) => value);
    if (entries.length === 0) {
      errorIndicator.classList.add('d-none');
      errorIndicator.textContent = '';
      return;
    }
    const text = entries.map(([key, value]) => `${key}: ${value}`).join(' | ');
    errorIndicator.textContent = text;
    errorIndicator.classList.remove('d-none');
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

  function updateGauges(interfaces) {
    if (!state.throughputGauge) {
      state.throughputGauge = createGauge('throughput-gauge', 'throughput-legend', formatThroughput);
      state.totalGauge = createGauge('total-gauge', 'total-legend', formatBytes);
    }
    const relevant = interfaces.filter((iface) => iface.type === 'wan' || iface.type === 'vpn');
    const labels = [];
    const throughputData = [];
    const totalData = [];
    relevant.forEach((iface) => {
      labels.push(iface.name || iface.interface || '');
      throughputData.push(Math.max(iface.currentThroughput || 0, 0));
      totalData.push(Math.max(iface.totalBytes || 0, 0));
    });
    updateGaugeChart(state.throughputGauge, labels, throughputData);
    updateGaugeChart(state.totalGauge, labels, totalData);
  }

  function createGauge(canvasId, legendId, formatter) {
    const ctx = document.getElementById(canvasId).getContext('2d');
    const legend = legendId ? document.getElementById(legendId) : null;
    const chart = new Chart(ctx, {
      type: 'doughnut',
      data: {
        labels: [],
        datasets: [{
          label: '',
          data: [],
          backgroundColor: palette,
          borderWidth: 0,
        }],
      },
      options: {
        responsive: true,
        maintainAspectRatio: true,
        aspectRatio: 1,
        cutout: '70%',
        circumference: 180,
        rotation: -90,
        plugins: {
          legend: { display: false },
          tooltip: {
            callbacks: {
              label: (context) => {
                const value = context.raw || 0;
                return `${context.label}: ${formatter(value)}`;
              },
            },
          },
        },
      },
    });
    chart.$formatter = formatter;
    chart.$legend = legend;
    return chart;
  }

  function updateGaugeChart(chart, labels, data) {
    chart.data.labels = labels;
    chart.data.datasets[0].data = data;
    chart.data.datasets[0].backgroundColor = resolveGaugeColors(labels);
    chart.options.plugins.tooltip.callbacks.label = (context) => {
      const value = context.raw || 0;
      const formatter = chart.$formatter || ((val) => val.toString());
      return `${context.label}: ${formatter(value)}`;
    };
    chart.update('none');
    updateGaugeLegend(chart, labels, data);
  }

  function updateGaugeLegend(chart, labels, data) {
    if (!chart.$legend) {
      return;
    }
    const legend = chart.$legend;
    legend.innerHTML = '';
    const colors = chart.data.datasets[0].backgroundColor || [];
    labels.forEach((label, index) => {
      const row = document.createElement('div');
      row.className = 'gauge-legend-row';

      const labelEl = document.createElement('div');
      labelEl.className = 'gauge-legend-label';

      const swatch = document.createElement('span');
      swatch.className = 'gauge-legend-swatch';
      swatch.style.backgroundColor = colors[index] || palette[index % palette.length];
      labelEl.appendChild(swatch);

      const text = document.createElement('span');
      text.className = 'gauge-legend-text';
      text.textContent = label || '';
      if (label) {
        text.title = label;
      }
      labelEl.appendChild(text);

      const valueEl = document.createElement('div');
      valueEl.className = 'gauge-legend-value';
      const formatter = chart.$formatter || ((val) => val.toString());
      valueEl.textContent = formatter(data[index] || 0);

      row.appendChild(labelEl);
      row.appendChild(valueEl);
      legend.appendChild(row);
    });
  }

  function resolveGaugeColors(labels) {
    if (!(state.gaugeColors instanceof Map)) {
      state.gaugeColors = new Map();
    }
    const usedColors = new Set(state.gaugeColors.values());
    const seenLabels = new Set();
    const colors = labels.map((label) => {
      const key = label || '';
      seenLabels.add(key);
      if (!state.gaugeColors.has(key)) {
        let assigned = null;
        for (const candidate of palette) {
          if (!usedColors.has(candidate)) {
            assigned = candidate;
            break;
          }
        }
        if (!assigned) {
          assigned = palette[state.gaugeColors.size % palette.length];
        }
        state.gaugeColors.set(key, assigned);
        usedColors.add(assigned);
      }
      return state.gaugeColors.get(key);
    });
    for (const key of Array.from(state.gaugeColors.keys())) {
      if (!seenLabels.has(key)) {
        state.gaugeColors.delete(key);
      }
    }
    return colors;
  }

  function updateInterfaceCards(interfaces, configs, latency) {
    const existing = new Set(state.interfaceCharts.keys());
    const configMap = new Map(configs.map((cfg) => [cfg.interfaceName, cfg]));
    const latencyMap = new Map(latency.map((item) => [item.name, item]));

    interfaces.forEach((iface, index) => {
      const key = iface.name;
      existing.delete(key);
      let record = state.interfaceCharts.get(key);
      const cfg = configMap.get(iface.interface);
      const latencyInfo = cfg ? latencyMap.get(cfg.name) : undefined;
      if (!record) {
        record = createInterfaceCard(iface, cfg, index);
        interfaceGrid.appendChild(record.container);
        state.interfaceCharts.set(key, record);
      }
      updateInterfaceCard(iface, cfg, latencyInfo, index);
    });

    existing.forEach((name) => {
      const record = state.interfaceCharts.get(name);
      if (record) {
        record.container.remove();
        record.chart.destroy();
        state.interfaceCharts.delete(name);
      }
    });
  }

  function deriveInterfaceStatus(iface, cfg, latencyInfo) {
    if (!iface) {
      return { text: '', level: 'muted' };
    }
    const displayName = resolveInterfaceDisplayName(iface, cfg);
    if (!iface.available) {
      return { text: `${displayName} • Interface unavailable`, level: 'warning' };
    }
    const operState = String(iface.operState || cfg?.operState || '').toLowerCase();

    if (latencyInfo && latencyInfo.success) {
      const latencyLabel = formatLatency(latencyInfo.latencyMs);
      return { text: `${displayName} • ${latencyLabel}`, level: 'success' };
    }

    if (latencyInfo && latencyInfo.error) {
      const tone = operState === 'down' ? 'danger' : 'warning';
      return { text: `${displayName} • ${latencyInfo.error}`, level: tone };
    }

    if (latencyInfo && !latencyInfo.success) {
      return { text: `${displayName} • No response`, level: 'warning' };
    }

    if (operState === 'down') {
      return { text: `${displayName} • Down`, level: 'danger' };
    }

    if (operState === 'up') {
      return { text: `${displayName} • Up`, level: 'success' };
    }

    if (operState === '') {
      return { text: `${displayName} • Unknown`, level: 'muted' };
    }

    return { text: `${displayName} • ${capitalizeWord(operState)}`, level: 'muted' };
  }

  function resolveInterfaceDisplayName(iface, cfg) {
    if (!iface) {
      return '';
    }
    if (iface.type === 'wan') {
      return 'WAN';
    }
    if (cfg && cfg.name) {
      return cfg.name;
    }
    if (iface.name) {
      return iface.name;
    }
    return iface.interface || 'Interface';
  }

  function applyStatusTone(element, tone) {
    if (!element) {
      return;
    }
    const palette = ['text-success', 'text-warning', 'text-danger', 'text-body-secondary'];
    palette.forEach((cls) => element.classList.remove(cls));
    switch (tone) {
      case 'success':
        element.classList.add('text-success');
        break;
      case 'warning':
        element.classList.add('text-warning');
        break;
      case 'danger':
        element.classList.add('text-danger');
        break;
      default:
        element.classList.add('text-body-secondary');
        break;
    }
  }

  function createInterfaceCard(iface, cfg, index) {
    const col = document.createElement('div');
    col.className = 'col-12 col-lg-6';
    col.dataset.interface = iface.name;
    col.style.order = index;

    const card = document.createElement('div');
    card.className = 'card interface-card h-100 shadow-sm';

    const header = document.createElement('div');
    header.className = 'card-header d-flex justify-content-between align-items-center';
    header.innerHTML = `
      <div>
        <span class="fw-semibold" data-field="name">${iface.name}</span>
        <div class="small text-body-secondary" data-field="iface">${iface.interface || ''}</div>
      </div>`;

    const badge = document.createElement('span');
    badge.className = 'badge rounded-pill text-bg-primary badge-operstate';
    badge.textContent = iface.type === 'wan' ? 'WAN' : 'VPN';
    header.appendChild(badge);
    card.appendChild(header);

    const body = document.createElement('div');
    body.className = 'card-body d-flex flex-column gap-3';

    const statsRow = document.createElement('div');
    statsRow.className = 'stats-row';
    statsRow.innerHTML = `
      <div>
        <div class="text-body-secondary small">Throughput</div>
        <div class="fw-semibold" data-field="throughput">–</div>
      </div>
      <div>
        <div class="text-body-secondary small">Received</div>
        <div class="fw-semibold" data-field="rx">–</div>
      </div>
      <div>
        <div class="text-body-secondary small">Sent</div>
        <div class="fw-semibold" data-field="tx">–</div>
      </div>
      <div>
        <div class="text-body-secondary small">Total</div>
        <div class="fw-semibold" data-field="total">–</div>
      </div>`;

    body.appendChild(statsRow);

    const statusLine = document.createElement('div');
    statusLine.className = 'text-body-secondary small';
    statusLine.dataset.field = 'status';
    body.appendChild(statusLine);

    const chartWrapper = document.createElement('div');
    chartWrapper.className = 'chart-wrapper';
    const canvas = document.createElement('canvas');
    chartWrapper.appendChild(canvas);
    body.appendChild(chartWrapper);

    card.appendChild(body);
    col.appendChild(card);

    const chart = new Chart(canvas.getContext('2d'), {
      type: 'line',
      data: {
        labels: [],
        datasets: [
          {
            label: 'Download',
            data: [],
            fill: true,
            borderColor: downloadColor,
            backgroundColor: downloadFill,
            tension: 0.3,
            pointRadius: 0,
          },
          {
            label: 'Upload',
            data: [],
            fill: true,
            borderColor: uploadColor,
            backgroundColor: uploadFill,
            tension: 0.3,
            pointRadius: 0,
          },
        ],
      },
      options: {
        animation: false,
        maintainAspectRatio: false,
        scales: {
          x: {
            ticks: { color: '#9ca3af', maxRotation: 0 },
            grid: { color: 'rgba(148, 163, 184, 0.1)' },
          },
          y: {
            ticks: {
              color: '#9ca3af',
              callback: (value) => formatThroughput(value),
            },
            grid: { color: 'rgba(148, 163, 184, 0.1)' },
            suggestedMax: 100000,
          },
        },
        plugins: {
          legend: {
            display: false,
          },
          tooltip: {
            callbacks: {
              label: (context) => `${context.dataset.label}: ${formatThroughput(context.parsed.y)}`,
            },
          },
        },
      },
    });

    return {
      container: col,
      chart,
      header,
      body,
      badge,
      statusLine,
      nameEl: header.querySelector('[data-field="name"]'),
      ifaceEl: header.querySelector('[data-field="iface"]'),
    };
  }

  function updateInterfaceCard(iface, cfg, latencyInfo, index) {
    const record = state.interfaceCharts.get(iface.name);
    if (!record) {
      return;
    }
    record.container.style.order = index;
    record.container.classList.toggle('wan-card', iface.type === 'wan');
    record.container.classList.toggle('vpn-card', iface.type === 'vpn');
    if (record.nameEl) {
      record.nameEl.textContent = iface.name;
    }
    if (record.ifaceEl) {
      record.ifaceEl.textContent = iface.interface || '';
    }
    if (record.badge) {
      record.badge.textContent = iface.type === 'wan' ? 'WAN' : 'VPN';
    }

    const statsRow = record.body.querySelector('.stats-row');
    const downloadLabel = formatThroughput(iface.currentRxThroughput || 0);
    const uploadLabel = formatThroughput(iface.currentTxThroughput || 0);
    statsRow.querySelector('[data-field="throughput"]').innerHTML = `<span class="text-primary">↓ ${downloadLabel}</span><br><span class="text-danger">↑ ${uploadLabel}</span>`;
    statsRow.querySelector('[data-field="rx"]').textContent = formatBytes(iface.rxBytes);
    statsRow.querySelector('[data-field="tx"]').textContent = formatBytes(iface.txBytes);
    statsRow.querySelector('[data-field="total"]').textContent = formatBytes(iface.totalBytes);

    if (record.statusLine) {
      const status = deriveInterfaceStatus(iface, cfg, latencyInfo);
      record.statusLine.textContent = status.text;
      applyStatusTone(record.statusLine, status.level);
      if (latencyInfo && latencyInfo.target) {
        record.statusLine.title = `Gateway: ${latencyInfo.target}`;
      } else {
        record.statusLine.removeAttribute('title');
      }
    }

    const history = Array.isArray(iface.history) ? iface.history : [];
    const labels = buildTimeLabels(history);
    const downloads = history.map((point) => point.rxThroughput || 0);
    const uploads = history.map((point) => point.txThroughput || 0);
    record.chart.data.labels = labels;
    record.chart.data.datasets[0].data = downloads;
    record.chart.data.datasets[1].data = uploads;
    const peakDownload = downloads.length ? Math.max(...downloads) : 0;
    const peakUpload = uploads.length ? Math.max(...uploads) : 0;
    const peakValue = Math.max(peakDownload, peakUpload);
    record.chart.options.scales.y.suggestedMax = Math.max(100000, peakValue > 0 ? peakValue * 1.2 : 0);
    record.chart.update('none');
  }

  function updateVpnTable(configs, latency, interfaces = []) {
    const latencyMap = new Map(latency.map((item) => [item.name, item]));
    const interfaceMap = new Map(
      (interfaces || []).map((iface) => [iface.interface, iface])
    );
    vpnTableBody.innerHTML = '';
    configs.forEach((cfg) => {
      const row = document.createElement('tr');
      row.innerHTML = `
        <td>
          <div class="fw-semibold">${cfg.name}</div>
          <div class="text-body-secondary small">${cfg.path}</div>
        </td>
        <td>${cfg.interfaceName || '–'}</td>
        <td class="text-uppercase">${cfg.vpnType || 'n/a'}</td>
        <td>${cfg.gateway || '–'}</td>
        <td data-field="latency">–</td>
        <td>
          <span class="badge ${cfg.connected ? 'text-bg-success' : 'text-bg-secondary'}">${cfg.connected ? 'Connected' : 'Stopped'}</span>
        </td>
        <td>
          <div class="form-check form-switch">
            <input class="form-check-input" type="checkbox" role="switch" data-action="autostart" data-name="${cfg.name}" ${cfg.autostart ? 'checked' : ''}${controlDisabledAttributes}>
          </div>
        </td>
        <td class="text-end">
          <div class="btn-group btn-group-sm" role="group">
            <button class="btn btn-outline-success" data-action="start" data-name="${cfg.name}"${controlDisabledAttributes}><i class="bi bi-play-fill"></i></button>
            <button class="btn btn-outline-warning" data-action="stop" data-name="${cfg.name}"${controlDisabledAttributes}><i class="bi bi-stop-fill"></i></button>
            <button class="btn btn-outline-light" data-action="edit" data-name="${cfg.name}" data-path="${cfg.path}"><i class="bi bi-pencil"></i></button>
          </div>
        </td>`;
      const latencyCell = row.querySelector('[data-field="latency"]');
      const latencyInfo = latencyMap.get(cfg.name);
      const iface = cfg.interfaceName ? interfaceMap.get(cfg.interfaceName) : undefined;
      const state = String(iface?.operState || cfg.operState || '').toLowerCase();
      latencyCell.classList.remove(
        'text-danger',
        'text-warning',
        'text-success',
        'text-body-secondary'
      );
      let text = '–';
      let tone = 'text-body-secondary';
      if (latencyInfo && latencyInfo.success) {
        text = formatLatency(latencyInfo.latencyMs);
        tone = 'text-success';
      } else if (latencyInfo && latencyInfo.error) {
        text = latencyInfo.error;
        tone = 'text-danger';
      } else if (latencyInfo && !latencyInfo.success) {
        text = 'No response';
        tone = 'text-warning';
      } else if (state === 'down') {
        text = 'Down';
        tone = 'text-danger';
      } else if (state === 'up') {
        text = 'Up';
        tone = 'text-success';
      } else if (state) {
        text = capitalizeWord(state);
      } else if (cfg.connected) {
        text = 'Up';
        tone = 'text-success';
      }
      latencyCell.textContent = text;
      latencyCell.classList.add(tone);
      if (latencyInfo && latencyInfo.target) {
        latencyCell.title = `Gateway: ${latencyInfo.target}`;
      } else {
        latencyCell.removeAttribute('title');
      }
      vpnTableBody.appendChild(row);
    });
  }

  function setStatus(message, isError) {
    if (!message) {
      return;
    }
    if (isError) {
      errorIndicator.textContent = message;
      errorIndicator.classList.remove('d-none');
    } else {
      errorIndicator.textContent = message;
      errorIndicator.classList.remove('d-none');
      setTimeout(() => {
        errorIndicator.classList.add('d-none');
      }, 4000);
    }
  }

  async function fetchJSON(url, options = {}) {
    const response = await fetch(url, options);
    if (!response.ok) {
      const text = await response.text();
      throw new Error(text || response.statusText);
    }
    const contentType = response.headers.get('content-type') || '';
    if (contentType.includes('application/json')) {
      return response.json();
    }
    return {};
  }

  function formatThroughput(value) {
    const units = ['bps', 'Kbps', 'Mbps', 'Gbps', 'Tbps'];
    let val = value;
    let index = 0;
    while (val >= 1000 && index < units.length - 1) {
      val /= 1000;
      index++;
    }
    return `${val.toFixed(val >= 100 ? 0 : val >= 10 ? 1 : 2)} ${units[index]}`;
  }

  function capitalizeWord(value) {
    if (!value) {
      return '';
    }
    return value.charAt(0).toUpperCase() + value.slice(1);
  }

  function formatBytes(value) {
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let val = value;
    let index = 0;
    while (val >= 1024 && index < units.length - 1) {
      val /= 1024;
      index++;
    }
    return `${val.toFixed(val >= 100 ? 0 : val >= 10 ? 1 : 2)} ${units[index]}`;
  }

  function formatLatency(value) {
    if (!value && value !== 0) {
      return '–';
    }
    if (value >= 1000) {
      return (value / 1000).toFixed(2) + ' s';
    }
    return value.toFixed(0) + ' ms';
  }

  function formatTime(timestamp) {
    const date = new Date(timestamp);
    if (Number.isNaN(date.getTime())) {
      return '';
    }
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }

  function buildTimeLabels(history) {
    let lastLabel = '';
    return history.map((point) => {
      const label = formatTime(point.timestamp);
      if (!label) {
        return '';
      }
      if (label === lastLabel) {
        return '';
      }
      lastLabel = label;
      return label;
    });
  }

  connectStream();
})();
