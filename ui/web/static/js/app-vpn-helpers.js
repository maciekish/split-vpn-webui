(() => {
  window.SplitVPNUI = window.SplitVPNUI || {};

  window.SplitVPNUI.createVPNController = function createVPNController(ctx) {
    const {
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
      flowInspectorController,
      fetchJSON,
      setStatus,
      formatLatency,
      capitalizeWord,
    } = ctx || {};

    if (
      !state ||
      !vpnTableBody ||
      !addVPNButton ||
      !vpnEditorModal ||
      !vpnEditorTitle ||
      !vpnTypeSelect ||
      !vpnNameInput ||
      !vpnConfigFileInput ||
      !vpnSupportingFilesInput ||
      !vpnSupportingFilesMeta ||
      !vpnConfigEditor ||
      !vpnEditorMeta ||
      !saveVPNButton ||
      !saveVPNLabel ||
      !deleteVPNModal ||
      !deleteVPNName ||
      !confirmDeleteVPNButton ||
      typeof fetchJSON !== 'function' ||
      typeof setStatus !== 'function'
    ) {
      return { render: () => {} };
    }
    const inspectorEnabled = Boolean(routingInspectorController && typeof routingInspectorController.open === 'function');
    const flowInspectorEnabled = Boolean(flowInspectorController && typeof flowInspectorController.open === 'function');
    const encodeFiles = window.SplitVPNUI && typeof window.SplitVPNUI.encodeSupportingFiles === 'function'
      ? window.SplitVPNUI.encodeSupportingFiles
      : encodeSupportingFiles;

    addVPNButton.addEventListener('click', () => {
      openAddVPNModal();
    });

    saveVPNButton.addEventListener('click', async () => {
      if (!state.vpnEditor) {
        return;
      }
      saveVPNButton.disabled = true;
      try {
        await saveVPN();
      } catch (err) {
        setStatus(err.message, true);
      } finally {
        saveVPNButton.disabled = false;
      }
    });

    vpnConfigFileInput.addEventListener('change', async () => {
      const file = vpnConfigFileInput.files && vpnConfigFileInput.files[0];
      if (!file) {
        return;
      }
      try {
        const content = await file.text();
        vpnConfigEditor.value = content;
        state.vpnEditor.configFileName = file.name || '';
        const detected = detectVPNType(file.name, content);
        if (detected) {
          vpnTypeSelect.value = detected;
        }
        vpnEditorMeta.textContent = `Loaded file: ${file.name}`;
      } catch (err) {
        setStatus('Failed to read uploaded file.', true);
      }
    });

    vpnSupportingFilesInput.addEventListener('change', async () => {
      const files = Array.from(vpnSupportingFilesInput.files || []);
      try {
        const encoded = await encodeFiles(files);
        state.vpnEditor.supportingFiles = encoded;
        renderSupportingFilesMeta();
      } catch (err) {
        state.vpnEditor.supportingFiles = [];
        renderSupportingFilesMeta();
        setStatus('Failed to read one or more supporting files.', true);
      }
    });

    vpnTypeSelect.addEventListener('change', () => {
      renderSupportingFilesMeta();
    });

    confirmDeleteVPNButton.addEventListener('click', async () => {
      const name = state.pendingDeleteVPN;
      if (!name) {
        return;
      }
      confirmDeleteVPNButton.disabled = true;
      try {
        await deleteVPN(name);
        deleteVPNModal.hide();
        state.pendingDeleteVPN = '';
      } catch (err) {
        setStatus(err.message, true);
      } finally {
        confirmDeleteVPNButton.disabled = false;
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
      if (action === 'start' || action === 'stop' || action === 'restart') {
        target.disabled = true;
        try {
          if (action === 'start') {
            await startVPN(name);
          } else if (action === 'stop') {
            await stopVPN(name);
          } else {
            await restartVPN(name);
          }
        } catch (err) {
          setStatus(err.message, true);
        } finally {
          target.disabled = false;
        }
        return;
      }
      if (action === 'edit') {
        await openEditVPNModal(name);
        return;
      }
      if (action === 'inspect-routing') {
        if (!inspectorEnabled) {
          setStatus('Routing inspector is unavailable in this UI build.', true);
          return;
        }
        await routingInspectorController.open(name);
        return;
      }
      if (action === 'inspect-flows') {
        if (!flowInspectorEnabled) {
          setStatus('Flow inspector is unavailable in this UI build.', true);
          return;
        }
        await flowInspectorController.open(name);
        return;
      }
      if (action === 'delete') {
        openDeleteVPNModal(name);
      }
    });

    vpnTableBody.addEventListener('change', async (event) => {
      const target = event.target;
      if (!target.matches('input[data-action="autostart"]')) {
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
    });

    function render(configs, latency, interfaces = []) {
      const latencyMap = new Map(latency.map((item) => [item.name, item]));
      const interfaceMap = new Map((interfaces || []).map((iface) => [iface.interface, iface]));
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
          <td class="font-monospace">
            <button class="btn btn-link btn-sm p-0 font-monospace text-decoration-none" data-action="inspect-routing" data-name="${cfg.name}" title="Inspect routing sets" ${inspectorEnabled ? '' : 'disabled'}>
              ${formatRoutingSizes(cfg)}
            </button>
          </td>
          <td data-field="latency">–</td>
          <td>
            <span class="badge ${cfg.connected ? 'text-bg-success' : 'text-bg-secondary'}">${cfg.connected ? 'Connected' : 'Stopped'}</span>
          </td>
          <td>
            <div class="form-check form-switch">
              <input class="form-check-input" type="checkbox" role="switch" data-action="autostart" data-name="${cfg.name}" ${cfg.autostart ? 'checked' : ''}>
            </div>
          </td>
          <td class="text-end">
            <div class="btn-group btn-group-sm" role="group">
              <button class="btn btn-outline-success" data-action="start" data-name="${cfg.name}" ${cfg.connected ? 'disabled' : ''} title="Start">
                <i class="bi bi-play-fill"></i>
              </button>
              <button class="btn btn-outline-warning" data-action="stop" data-name="${cfg.name}" title="Stop">
                <i class="bi bi-stop-fill"></i>
              </button>
              <button class="btn btn-outline-info" data-action="restart" data-name="${cfg.name}" title="Restart">
                <i class="bi bi-arrow-repeat"></i>
              </button>
              <button class="btn btn-outline-primary" data-action="inspect-flows" data-name="${cfg.name}" title="Inspect live flows" ${flowInspectorEnabled ? '' : 'disabled'}>
                <i class="bi bi-search"></i>
              </button>
              <button class="btn btn-outline-light" data-action="edit" data-name="${cfg.name}" title="Edit">
                <i class="bi bi-pencil"></i>
              </button>
              <button class="btn btn-outline-danger" data-action="delete" data-name="${cfg.name}" title="Delete">
                <i class="bi bi-trash"></i>
              </button>
            </div>
          </td>`;
        const latencyCell = row.querySelector('[data-field="latency"]');
        const latencyInfo = latencyMap.get(cfg.name);
        const iface = cfg.interfaceName ? interfaceMap.get(cfg.interfaceName) : undefined;
        const operState = String(iface?.operState || cfg.operState || '').toLowerCase();
        latencyCell.classList.remove(
          'text-danger',
          'text-warning',
          'text-success',
          'text-body-secondary'
        );
        let text = '–';
        let tone = 'text-body-secondary';
        if (latencyInfo && latencyInfo.success) {
          text = formatLatency ? formatLatency(latencyInfo.latencyMs) : `${latencyInfo.latencyMs || 0} ms`;
          tone = 'text-success';
        } else if (latencyInfo && latencyInfo.error) {
          text = latencyInfo.error;
          tone = 'text-danger';
        } else if (latencyInfo && !latencyInfo.success) {
          text = 'No response';
          tone = 'text-warning';
        } else if (operState === 'down') {
          text = 'Down';
          tone = 'text-danger';
        } else if (operState === 'up') {
          text = 'Up';
          tone = 'text-success';
        } else if (operState) {
          text = capitalizeWord ? capitalizeWord(operState) : operState;
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

    function formatRoutingSizes(cfg) {
      const v4 = Number(cfg?.routingV4Size ?? 0);
      const v6 = Number(cfg?.routingV6Size ?? 0);
      if (!Number.isFinite(v4) || !Number.isFinite(v6)) {
        return '–';
      }
      return `${Math.max(0, Math.trunc(v4))} / ${Math.max(0, Math.trunc(v6))}`;
    }

    async function startVPN(name) {
      await fetchJSON(`/api/configs/${encodeURIComponent(name)}/start`, { method: 'POST' });
      setStatus(`Started ${name}.`, false);
    }

    async function stopVPN(name) {
      await fetchJSON(`/api/configs/${encodeURIComponent(name)}/stop`, { method: 'POST' });
      setStatus(`Stopped ${name}.`, false);
    }

    async function restartVPN(name) {
      await fetchJSON(`/api/vpns/${encodeURIComponent(name)}/restart`, { method: 'POST' });
      setStatus(`Restarted ${name}.`, false);
    }

    async function deleteVPN(name) {
      await fetchJSON(`/api/vpns/${encodeURIComponent(name)}`, { method: 'DELETE' });
      setStatus(`Deleted ${name}.`, false);
    }

    function openDeleteVPNModal(name) {
      state.pendingDeleteVPN = name;
      deleteVPNName.textContent = name;
      deleteVPNModal.show();
    }

    function openAddVPNModal() {
      state.vpnEditor = {
        mode: 'create',
        originalName: '',
        configFileName: '',
        supportingFiles: [],
        existingSupportingFiles: [],
      };
      vpnEditorTitle.innerHTML = '<i class="bi bi-plus-circle me-2"></i>Add VPN Profile';
      saveVPNLabel.textContent = 'Create VPN';
      vpnTypeSelect.value = 'wireguard';
      vpnTypeSelect.disabled = false;
      vpnNameInput.value = '';
      vpnNameInput.readOnly = false;
      vpnConfigFileInput.value = '';
      vpnSupportingFilesInput.value = '';
      vpnConfigEditor.value = '';
      vpnEditorMeta.textContent = '';
      renderSupportingFilesMeta();
      vpnEditorModal.show();
    }

    async function openEditVPNModal(name) {
      try {
        const data = await fetchJSON(`/api/vpns/${encodeURIComponent(name)}`);
        const profile = data.vpn || {};
        state.vpnEditor = {
          mode: 'edit',
          originalName: profile.name || name,
          configFileName: profile.configFile || '',
          supportingFiles: [],
          existingSupportingFiles: Array.isArray(profile.supportingFiles) ? profile.supportingFiles : [],
        };
        vpnEditorTitle.innerHTML = '<i class="bi bi-pencil-square me-2"></i>Edit VPN Profile';
        saveVPNLabel.textContent = 'Save Changes';
        vpnTypeSelect.value = normalizeVPNType(profile.type || 'wireguard');
        vpnTypeSelect.disabled = false;
        vpnNameInput.value = profile.name || name;
        vpnNameInput.readOnly = true;
        vpnConfigFileInput.value = '';
        vpnSupportingFilesInput.value = '';
        vpnConfigEditor.value = profile.rawConfig || '';
        vpnEditorMeta.textContent = `Config file: ${profile.configFile || 'auto'}`;
        renderSupportingFilesMeta();
        vpnEditorModal.show();
      } catch (err) {
        setStatus(err.message, true);
      }
    }

    async function saveVPN() {
      const mode = state.vpnEditor?.mode || 'create';
      const name = (vpnNameInput.value || '').trim();
      const type = normalizeVPNType(vpnTypeSelect.value || '');
      const config = vpnConfigEditor.value || '';
      if (!name) {
        throw new Error('VPN name is required.');
      }
      if (!type) {
        throw new Error('VPN type is required.');
      }
      if (!config.trim()) {
        throw new Error('VPN configuration content is required.');
      }
      const payload = { name, type, config };
      const explicitFile = (state.vpnEditor?.configFileName || '').trim();
      if (explicitFile) {
        payload.configFile = explicitFile;
      }
      if (Array.isArray(state.vpnEditor?.supportingFiles) && state.vpnEditor.supportingFiles.length > 0) {
        payload.supportingFiles = state.vpnEditor.supportingFiles;
      }
      if (mode === 'edit') {
        const result = await fetchJSON(`/api/vpns/${encodeURIComponent(state.vpnEditor.originalName || name)}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        const warnings = Array.isArray(result?.vpn?.warnings) ? result.vpn.warnings : [];
        if (warnings.length > 0) {
          setStatus(`Saved VPN ${name} with warnings: ${warnings.join(' | ')}`, true);
        } else {
          setStatus(`Saved VPN ${name}.`, false);
        }
      } else {
        const result = await fetchJSON('/api/vpns', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        const warnings = Array.isArray(result?.vpn?.warnings) ? result.vpn.warnings : [];
        if (warnings.length > 0) {
          setStatus(`Created VPN ${name} with warnings: ${warnings.join(' | ')}`, true);
        } else {
          setStatus(`Created VPN ${name}.`, false);
        }
      }
      vpnEditorModal.hide();
    }

    function normalizeVPNType(value) {
      const raw = String(value || '').trim().toLowerCase();
      if (raw === 'wireguard' || raw === 'wg' || raw === 'external') {
        return 'wireguard';
      }
      if (raw === 'openvpn' || raw === 'ovpn') {
        return 'openvpn';
      }
      return '';
    }

    function detectVPNType(fileName, content) {
      const name = String(fileName || '').toLowerCase();
      if (name.endsWith('.ovpn')) {
        return 'openvpn';
      }
      if (name.endsWith('.wg') || name.endsWith('.conf')) {
        return 'wireguard';
      }
      const text = String(content || '').toLowerCase();
      if (text.includes('[interface]') && text.includes('[peer]')) {
        return 'wireguard';
      }
      if (text.includes('\nremote ') || text.includes('\nclient') || text.includes('<ca>')) {
        return 'openvpn';
      }
      return '';
    }

    function renderSupportingFilesMeta() {
      const type = normalizeVPNType(vpnTypeSelect.value || '');
      if (type !== 'openvpn') {
        vpnSupportingFilesMeta.textContent = 'Optional for WireGuard. Used for OpenVPN external file references.';
        return;
      }
      const uploaded = Array.isArray(state.vpnEditor?.supportingFiles) ? state.vpnEditor.supportingFiles : [];
      const existing = Array.isArray(state.vpnEditor?.existingSupportingFiles) ? state.vpnEditor.existingSupportingFiles : [];
      if (uploaded.length > 0) {
        vpnSupportingFilesMeta.textContent = `Selected ${uploaded.length} supporting file(s): ${uploaded.map((item) => item.name).join(', ')}`;
        return;
      }
      if (existing.length > 0) {
        vpnSupportingFilesMeta.textContent = `Existing supporting file(s): ${existing.join(', ')}. Upload files to replace or add.`;
        return;
      }
      vpnSupportingFilesMeta.textContent = 'Upload files referenced by `.ovpn` directives (for example `ca`, `cert`, `key`, `auth-user-pass`).';
    }

    return { render };
  };
})();
