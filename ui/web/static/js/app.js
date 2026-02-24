(() => {
  const interfaceGrid = document.getElementById('interface-grid');
  const vpnTableBody = document.querySelector('#vpn-table tbody');
  const wanLabel = document.getElementById('wan-label');
  const updatedAt = document.getElementById('updated-at');
  const errorIndicator = document.getElementById('error-indicator');
  const refreshButton = document.getElementById('refresh-configs');
  const settingsButton = document.getElementById('open-settings');
  const addVPNButton = document.getElementById('open-add-vpn');
  const vpnEditorModalElement = document.getElementById('vpnEditorModal');
  const vpnEditorModal = new bootstrap.Modal(vpnEditorModalElement);
  const vpnEditorTitle = document.getElementById('vpn-editor-title');
  const vpnTypeSelect = document.getElementById('vpn-type');
  const vpnNameInput = document.getElementById('vpn-name');
  const vpnConfigFileInput = document.getElementById('vpn-config-file');
  const vpnSupportingFilesInput = document.getElementById('vpn-supporting-files');
  const vpnSupportingFilesMeta = document.getElementById('vpn-supporting-files-meta');
  const vpnConfigEditor = document.getElementById('vpn-config-editor');
  const vpnEditorMeta = document.getElementById('vpn-editor-meta');
  const saveVPNButton = document.getElementById('save-vpn');
  const saveVPNLabel = document.getElementById('save-vpn-label');
  const deleteVPNModalElement = document.getElementById('deleteVpnModal');
  const deleteVPNModal = new bootstrap.Modal(deleteVPNModalElement);
  const deleteVPNName = document.getElementById('delete-vpn-name');
  const confirmDeleteVPNButton = document.getElementById('confirm-delete-vpn');
  const routingInspectorModalElement = document.getElementById('routingInspectorModal');
  const routingInspectorModal = routingInspectorModalElement
    ? new bootstrap.Modal(routingInspectorModalElement)
    : null;
  const routingInspectorTitle = document.getElementById('routing-inspector-title');
  const routingInspectorStatus = document.getElementById('routing-inspector-status');
  const routingInspectorSummaryVPN = document.getElementById('routing-inspector-summary-vpn');
  const routingInspectorSummaryV4 = document.getElementById('routing-inspector-summary-v4');
  const routingInspectorSummaryV6 = document.getElementById('routing-inspector-summary-v6');
  const routingInspectorUpdatedAt = document.getElementById('routing-inspector-updated-at');
  const routingInspectorSearch = document.getElementById('routing-inspector-search');
  const routingInspectorSearchRegex = document.getElementById('routing-inspector-search-regex');
  const routingInspectorSearchMeta = document.getElementById('routing-inspector-search-meta');
  const routingInspectorContent = document.getElementById('routing-inspector-content');
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
  const statusSuccessClasses = ['bg-success-subtle', 'text-success', 'border', 'border-success-subtle'];
  const statusErrorClasses = ['bg-danger-subtle', 'text-danger', 'border', 'border-danger-subtle'];
  let stream;
  let reconnectTimer;
  const state = {
    interfaceCharts: new Map(),
    throughputGauge: null,
    totalGauge: null,
    availableInterfaces: [],
    settings: null,
    gaugeColors: new Map(),
    statusLockUntil: 0,
    vpnEditor: {
      mode: 'create',
      originalName: '',
      configFileName: '',
      supportingFiles: [],
      existingSupportingFiles: [],
    },
    pendingDeleteVPN: '',
  };
  const chartHelpersFactory = window.SplitVPNUI && typeof window.SplitVPNUI.createChartHelpers === 'function'
    ? window.SplitVPNUI.createChartHelpers
    : null;
  const chartHelpers = chartHelpersFactory
    ? chartHelpersFactory({
      interfaceGrid,
      state,
      palette,
      downloadColor,
      downloadFill,
      uploadColor,
      uploadFill,
    })
    : null;
  const formatThroughput = chartHelpers?.formatThroughput || ((value) => `${Number(value || 0)} bps`);
  const formatBytes = chartHelpers?.formatBytes || ((value) => `${Number(value || 0)} B`);
  const formatLatency = chartHelpers?.formatLatency || ((value) => `${Number(value || 0)} ms`);
  const capitalizeWord = chartHelpers?.capitalizeWord || ((value) => {
    const raw = String(value || '');
    if (!raw) {
      return '';
    }
    return raw.charAt(0).toUpperCase() + raw.slice(1);
  });

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
      prewarmParallelism: Number(state.settings?.prewarmParallelism || 0),
      prewarmDoHTimeoutSeconds: Number(state.settings?.prewarmDoHTimeoutSeconds || 0),
      prewarmIntervalSeconds: Number(state.settings?.prewarmIntervalSeconds || 0),
      resolverParallelism: Number(state.settings?.resolverParallelism || 0),
      resolverTimeoutSeconds: Number(state.settings?.resolverTimeoutSeconds || 0),
      resolverIntervalSeconds: Number(state.settings?.resolverIntervalSeconds || 0),
      resolverDomainTimeoutSeconds: Number(state.settings?.resolverDomainTimeoutSeconds || 0),
      resolverAsnTimeoutSeconds: Number(state.settings?.resolverAsnTimeoutSeconds || 0),
      resolverWildcardTimeoutSeconds: Number(state.settings?.resolverWildcardTimeoutSeconds || 0),
      resolverDomainEnabled: state.settings?.resolverDomainEnabled !== false,
      resolverAsnEnabled: state.settings?.resolverAsnEnabled !== false,
      resolverWildcardEnabled: state.settings?.resolverWildcardEnabled !== false,
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
  const routingInspectorFactory = window.SplitVPNUI && typeof window.SplitVPNUI.createRoutingInspectorController === 'function'
    ? window.SplitVPNUI.createRoutingInspectorController
    : null;
  const routingInspectorController = routingInspectorFactory
    ? routingInspectorFactory({
      routingInspectorModal,
      routingInspectorTitle,
      routingInspectorStatus,
      routingInspectorSummaryVPN,
      routingInspectorSummaryV4,
      routingInspectorSummaryV6,
      routingInspectorUpdatedAt,
      routingInspectorSearch,
      routingInspectorSearchRegex,
      routingInspectorSearchMeta,
      routingInspectorContent,
      fetchJSON,
      setStatus,
    })
    : null;
  const vpnControllerFactory = window.SplitVPNUI && typeof window.SplitVPNUI.createVPNController === 'function'
    ? window.SplitVPNUI.createVPNController
    : null;
  const vpnController = vpnControllerFactory
    ? vpnControllerFactory({
      state,
      vpnTableBody,
      addVPNButton,
      vpnEditorModal,
      vpnEditorTitle,
      vpnTypeSelect,
      vpnNameInput,
      vpnConfigFileInput,
      vpnSupportingFilesInput,
      vpnSupportingFilesMeta,
      vpnConfigEditor,
      vpnEditorMeta,
      saveVPNButton,
      saveVPNLabel,
      deleteVPNModal,
      deleteVPNName,
      confirmDeleteVPNButton,
      routingInspectorController,
      fetchJSON,
      setStatus,
      formatLatency,
      capitalizeWord,
    })
    : null;
  const updateControllerFactory = window.SplitVPNUI && typeof window.SplitVPNUI.createUpdateController === 'function'
    ? window.SplitVPNUI.createUpdateController
    : null;
  const updateController = updateControllerFactory
    ? updateControllerFactory({
      settingsModalElement,
      fetchJSON,
      setStatus,
    })
    : null;

  async function openSettingsModal() {
    try {
      const data = await fetchJSON('/api/settings');
      state.settings = data.settings || { listenInterface: '', wanInterface: '' };
      state.availableInterfaces = Array.isArray(data.interfaces) ? data.interfaces : [];
      populateSettingsForm();
      if (updateController?.refreshStatus) {
        await updateController.refreshStatus();
      }
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
      'Use default bind address (127.0.0.1)'
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
    if (chartHelpers?.updateInterfaceCards) {
      chartHelpers.updateInterfaceCards(interfaces, payload.configs || [], payload.latency || []);
    }
    if (vpnController?.render) {
      vpnController.render(payload.configs || [], payload.latency || [], interfaces);
    }
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
    if (entries.length === 0 && Date.now() < state.statusLockUntil) {
      return;
    }
    if (entries.length === 0) {
      errorIndicator.classList.add('d-none');
      errorIndicator.textContent = '';
      return;
    }
    applyStatusToneClasses(true);
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
    if (!chartHelpers?.createGauge || !chartHelpers?.updateGaugeChart) {
      return;
    }
    if (!state.throughputGauge) {
      state.throughputGauge = chartHelpers.createGauge('throughput-gauge', 'throughput-legend', formatThroughput);
      state.totalGauge = chartHelpers.createGauge('total-gauge', 'total-legend', formatBytes);
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
    chartHelpers.updateGaugeChart(state.throughputGauge, labels, throughputData);
    chartHelpers.updateGaugeChart(state.totalGauge, labels, totalData);
  }

  function setStatus(message, isError) {
    if (!message) {
      return;
    }
    applyStatusToneClasses(Boolean(isError));
    errorIndicator.textContent = message;
    errorIndicator.classList.remove('d-none');
    state.statusLockUntil = Date.now() + 4500;
    if (!isError) {
      setTimeout(() => {
        if (Date.now() < state.statusLockUntil) {
          return;
        }
        errorIndicator.classList.add('d-none');
      }, 4600);
    }
  }

  async function fetchJSON(url, options = {}) {
    const response = await fetch(url, options);
    const contentType = response.headers.get('content-type') || '';
    let parsed = null;
    if (contentType.includes('application/json')) {
      try {
        parsed = await response.json();
      } catch (err) {
        parsed = null;
      }
    }
    if (!response.ok) {
      if (parsed && typeof parsed.error === 'string' && parsed.error) {
        throw new Error(parsed.error);
      }
      let text = '';
      try {
        text = await response.text();
      } catch (err) {
        text = '';
      }
      throw new Error(text || response.statusText || 'Request failed');
    }
    if (parsed !== null) {
      return parsed;
    }
    return {};
  }

  function applyStatusToneClasses(isError) {
    const remove = isError ? statusSuccessClasses : statusErrorClasses;
    const add = isError ? statusErrorClasses : statusSuccessClasses;
    remove.forEach((className) => errorIndicator.classList.remove(className));
    add.forEach((className) => errorIndicator.classList.add(className));
  }

  connectStream();
})();
