(() => {
  const groupsList = document.getElementById('domain-groups-list');
  const groupsEmpty = document.getElementById('domain-groups-empty');
  const groupsStatus = document.getElementById('domain-groups-status');
  const addGroupButton = document.getElementById('open-add-group');
  const groupModalElement = document.getElementById('domainGroupModal');
  const groupModal = new bootstrap.Modal(groupModalElement);
  const groupModalTitle = document.getElementById('domain-group-modal-title');
  const groupNameInput = document.getElementById('domain-group-name');
  const groupEgressSelect = document.getElementById('domain-group-egress');
  const addRuleButton = document.getElementById('add-routing-rule');
  const rulesList = document.getElementById('routing-rules-list');
  const saveGroupButton = document.getElementById('save-domain-group');
  const deleteGroupModalElement = document.getElementById('deleteGroupModal');
  const deleteGroupModal = new bootstrap.Modal(deleteGroupModalElement);
  const deleteGroupName = document.getElementById('delete-group-name');
  const confirmDeleteGroupButton = document.getElementById('confirm-delete-group');
  const refreshButton = document.getElementById('refresh-configs');
  if (
    !groupsList ||
    !groupsEmpty ||
    !groupsStatus ||
    !addGroupButton ||
    !groupModalElement ||
    !groupModalTitle ||
    !groupNameInput ||
    !groupEgressSelect ||
    !addRuleButton ||
    !rulesList ||
    !saveGroupButton ||
    !deleteGroupModalElement ||
    !deleteGroupName ||
    !confirmDeleteGroupButton
  ) {
    return;
  }

  const helper = window.SplitVPNDomainRoutingUtils;
  const rulesFactory = window.SplitVPNDomainRoutingRules
    && typeof window.SplitVPNDomainRoutingRules.createController === 'function'
    ? window.SplitVPNDomainRoutingRules.createController
    : null;
  if (!helper || !rulesFactory) {
    console.error('domain-routing helpers unavailable');
    return;
  }

  const escapeHTML = helper.escapeHTML;
  const sourceMACDeviceDatalistID = 'routing-source-mac-devices';
  const state = {
    groups: [],
    vpns: [],
    devices: [],
    editingGroupID: null,
    pendingDeleteID: null,
    nextRuleID: 1,
  };

  const rulesController = rulesFactory({
    rulesList,
    state,
    helper,
    showStatus,
    sourceMACDeviceDatalistID,
  });
  if (!rulesController) {
    console.error('domain-routing rule controller unavailable');
    return;
  }

  addGroupButton.addEventListener('click', async () => {
    await openAddGroupModal();
  });

  addRuleButton.addEventListener('click', () => {
    rulesController.appendRuleCard();
  });

  rulesList.addEventListener('click', (event) => {
    const actionTarget = event.target.closest('[data-action]');
    if (!actionTarget) {
      return;
    }
    const action = actionTarget.getAttribute('data-action');
    const card = actionTarget.closest('.routing-rule-card');
    rulesController.handleAction(action, card);
  });

  saveGroupButton.addEventListener('click', async () => {
    saveGroupButton.disabled = true;
    try {
      await saveGroup();
    } catch (err) {
      showStatus(err.message, true);
    } finally {
      saveGroupButton.disabled = false;
    }
  });

  groupsList.addEventListener('click', async (event) => {
    const actionTarget = event.target.closest('[data-action]');
    if (!actionTarget) {
      return;
    }
    const groupID = Number(actionTarget.getAttribute('data-group-id') || 0);
    if (!Number.isFinite(groupID) || groupID <= 0) {
      return;
    }
    const action = actionTarget.getAttribute('data-action');
    if (action === 'edit') {
      await openEditGroupModal(groupID);
      return;
    }
    if (action === 'delete') {
      openDeleteGroupModal(groupID);
    }
  });

  confirmDeleteGroupButton.addEventListener('click', async () => {
    const id = state.pendingDeleteID;
    if (!id) {
      return;
    }
    confirmDeleteGroupButton.disabled = true;
    try {
      await fetchJSON(`/api/groups/${id}`, { method: 'DELETE' });
      deleteGroupModal.hide();
      showStatus('Policy group deleted.', false);
      state.pendingDeleteID = null;
      await loadDomainGroups();
    } catch (err) {
      showStatus(err.message, true);
    } finally {
      confirmDeleteGroupButton.disabled = false;
    }
  });

  if (refreshButton) {
    refreshButton.addEventListener('click', async () => {
      await Promise.all([loadVPNs(), loadDomainGroups(), loadDevices()]);
    });
  }

  async function initialize() {
    try {
      await Promise.all([loadVPNs(), loadDomainGroups(), loadDevices()]);
    } catch (err) {
      showStatus(err.message, true);
    }
  }

  async function loadVPNs() {
    const data = await fetchJSON('/api/vpns');
    const vpns = Array.isArray(data.vpns) ? data.vpns : [];
    vpns.sort((a, b) => (a.name || '').localeCompare(b.name || ''));
    state.vpns = vpns;
    renderEgressOptions();
  }

  async function loadDomainGroups() {
    const data = await fetchJSON('/api/groups');
    const groups = Array.isArray(data.groups) ? data.groups : [];
    groups.sort((a, b) => (a.name || '').localeCompare(b.name || ''));
    state.groups = groups;
    renderDomainGroups(groups);
  }

  async function loadDevices() {
    const data = await fetchJSON('/api/devices');
    const devices = Array.isArray(data.devices) ? data.devices : [];
    state.devices = devices.map((device) => ({
      mac: String(device?.mac || '').trim().toLowerCase(),
      name: String(device?.name || '').trim(),
      ipHints: Array.isArray(device?.ipHints)
        ? device.ipHints.map((entry) => String(entry || '').trim()).filter((entry) => entry !== '')
        : [],
      searchText: String(device?.searchText || '').toLowerCase(),
    })).filter((entry) => entry.mac !== '');
    rulesController.refreshSourceMACDeviceDatalist();
  }

  function renderDomainGroups(groups) {
    groupsList.innerHTML = '';
    if (!groups.length) {
      groupsEmpty.classList.remove('d-none');
      return;
    }
    groupsEmpty.classList.add('d-none');
    groups.forEach((group) => {
      const rules = rulesController.normalizeRules(group);
      const card = document.createElement('div');
      card.className = 'domain-group-card';
      card.innerHTML = `
        <div class="d-flex justify-content-between align-items-start gap-2 mb-2">
          <div class="min-w-0">
            <div class="fw-semibold text-truncate">${escapeHTML(group.name || '')}</div>
            <div class="small text-body-secondary">
              <span class="badge text-bg-primary">${escapeHTML(group.egressVpn || 'n/a')}</span>
              <span class="ms-1">${rules.length} rules</span>
            </div>
          </div>
          <div class="btn-group btn-group-sm" role="group">
            <button class="btn btn-outline-light" data-action="edit" data-group-id="${group.id}" title="Edit group">
              <i class="bi bi-pencil"></i>
            </button>
            <button class="btn btn-outline-danger" data-action="delete" data-group-id="${group.id}" title="Delete group">
              <i class="bi bi-trash"></i>
            </button>
          </div>
        </div>
        <div class="domain-group-rules">${renderRuleBadges(rules)}</div>
      `;
      groupsList.appendChild(card);
    });
  }

  function renderRuleBadges(rules) {
    if (!Array.isArray(rules) || rules.length === 0) {
      return '<span class="text-body-secondary small">No rules</span>';
    }
    return rules
      .slice(0, 4)
      .map((rule, index) => {
        const tokens = [];
        if (rule.sourceInterfaces.length) {
          tokens.push(`iface:${rule.sourceInterfaces.length}`);
        }
        if (rule.sourceCidrs.length) {
          tokens.push(`src:${rule.sourceCidrs.length}`);
        }
        if (rule.sourceMacs.length) {
          tokens.push(`mac:${rule.sourceMacs.length}`);
        }
        if (rule.destinationCidrs.length) {
          tokens.push(`dst:${rule.destinationCidrs.length}`);
        }
        if (rule.destinationPorts.length) {
          tokens.push(`port:${rule.destinationPorts.length}`);
        }
        if (rule.destinationAsns.length) {
          tokens.push(`asn:${rule.destinationAsns.length}`);
        }
        if (rule.domains.length) {
          tokens.push(`domain:${rule.domains.length}`);
        }
        if (rule.wildcardDomains.length) {
          tokens.push(`wild:${rule.wildcardDomains.length}`);
        }
        return `<span class="domain-group-domain">R${index + 1} ${escapeHTML(tokens.join(' '))}</span>`;
      })
      .join('') + (rules.length > 4 ? '<span class="text-body-secondary small">+ more</span>' : '');
  }

  async function openAddGroupModal() {
    state.editingGroupID = null;
    await Promise.all([loadVPNs(), loadDevices()]);
    groupModalTitle.innerHTML = '<i class="bi bi-diagram-3 me-2"></i>Add Policy Group';
    groupNameInput.value = '';
    groupNameInput.readOnly = false;
    selectDefaultEgressVPN();
    rulesController.resetRules([]);
    groupModal.show();
  }

  async function openEditGroupModal(groupID) {
    const group = state.groups.find((entry) => Number(entry.id) === groupID);
    if (!group) {
      showStatus('Policy group not found.', true);
      return;
    }
    state.editingGroupID = groupID;
    await Promise.all([loadVPNs(), loadDevices()]);
    groupModalTitle.innerHTML = '<i class="bi bi-pencil-square me-2"></i>Edit Policy Group';
    groupNameInput.value = group.name || '';
    groupNameInput.readOnly = false;
    groupEgressSelect.value = group.egressVpn || '';
    rulesController.resetRules(rulesController.normalizeRules(group));
    groupModal.show();
  }

  function openDeleteGroupModal(groupID) {
    const group = state.groups.find((entry) => Number(entry.id) === groupID);
    if (!group) {
      showStatus('Policy group not found.', true);
      return;
    }
    state.pendingDeleteID = groupID;
    deleteGroupName.textContent = group.name || '';
    deleteGroupModal.show();
  }

  async function saveGroup() {
    const payload = buildGroupPayload();
    if (state.editingGroupID) {
      await fetchJSON(`/api/groups/${state.editingGroupID}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      showStatus('Policy group updated.', false);
    } else {
      await fetchJSON('/api/groups', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      showStatus('Policy group created.', false);
    }
    groupModal.hide();
    await loadDomainGroups();
  }

  function buildGroupPayload() {
    const name = (groupNameInput.value || '').trim();
    const egressVPN = (groupEgressSelect.value || '').trim();
    const rules = rulesController.parseRuleCards();
    if (!name) {
      throw new Error('Group name is required.');
    }
    if (!egressVPN) {
      throw new Error('Egress VPN is required.');
    }
    if (rules.length === 0) {
      throw new Error('At least one rule with selectors or comment lines is required.');
    }
    return { name, egressVpn: egressVPN, rules };
  }

  function renderEgressOptions() {
    const previousValue = groupEgressSelect.value;
    groupEgressSelect.innerHTML = '';
    const placeholder = document.createElement('option');
    placeholder.value = '';
    placeholder.textContent = state.vpns.length ? 'Select VPN' : 'No VPN profiles available';
    groupEgressSelect.appendChild(placeholder);
    state.vpns.forEach((vpn) => {
      const option = document.createElement('option');
      option.value = vpn.name || '';
      option.textContent = vpn.name || '';
      groupEgressSelect.appendChild(option);
    });
    if (previousValue && state.vpns.some((vpn) => vpn.name === previousValue)) {
      groupEgressSelect.value = previousValue;
    }
  }

  function selectDefaultEgressVPN() {
    if (state.vpns.length > 0) {
      groupEgressSelect.value = state.vpns[0].name || '';
    } else {
      groupEgressSelect.value = '';
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
    return parsed || {};
  }

  function showStatus(message, isError) {
    groupsStatus.classList.remove('d-none', 'alert-success', 'alert-danger');
    groupsStatus.classList.add(isError ? 'alert-danger' : 'alert-success');
    groupsStatus.textContent = message || '';
    if (!isError) {
      setTimeout(() => {
        groupsStatus.classList.add('d-none');
      }, 3500);
    }
  }

  initialize();
})();
