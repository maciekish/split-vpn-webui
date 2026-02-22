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
  const state = {
    groups: [],
    vpns: [],
    editingGroupID: null,
    pendingDeleteID: null,
    nextRuleID: 1,
  };
  addGroupButton.addEventListener('click', async () => {
    await openAddGroupModal();
  });
  addRuleButton.addEventListener('click', () => {
    appendRuleCard();
  });
  rulesList.addEventListener('click', (event) => {
    const actionTarget = event.target.closest('[data-action]');
    if (!actionTarget) {
      return;
    }
    const action = actionTarget.getAttribute('data-action');
    if (action === 'remove-rule') {
      const card = actionTarget.closest('.routing-rule-card');
      if (card) {
        card.remove();
      }
      if (rulesList.children.length === 0) {
        appendRuleCard();
      }
    }
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
      await Promise.all([loadVPNs(), loadDomainGroups()]);
    });
  }
  async function initialize() {
    try {
      await Promise.all([loadVPNs(), loadDomainGroups()]);
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
  function renderDomainGroups(groups) {
    groupsList.innerHTML = '';
    if (!groups.length) {
      groupsEmpty.classList.remove('d-none');
      return;
    }
    groupsEmpty.classList.add('d-none');
    groups.forEach((group) => {
      const rules = normalizeRules(group);
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
        if (rule.sourceCidrs.length) {
          tokens.push(`src:${rule.sourceCidrs.length}`);
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
    await loadVPNs();
    groupModalTitle.innerHTML = '<i class="bi bi-diagram-3 me-2"></i>Add Policy Group';
    groupNameInput.value = '';
    groupNameInput.readOnly = false;
    selectDefaultEgressVPN();
    resetRules([]);
    groupModal.show();
  }
  async function openEditGroupModal(groupID) {
    const group = state.groups.find((entry) => Number(entry.id) === groupID);
    if (!group) {
      showStatus('Policy group not found.', true);
      return;
    }
    state.editingGroupID = groupID;
    await loadVPNs();
    groupModalTitle.innerHTML = '<i class="bi bi-pencil-square me-2"></i>Edit Policy Group';
    groupNameInput.value = group.name || '';
    groupNameInput.readOnly = false;
    groupEgressSelect.value = group.egressVpn || '';
    resetRules(normalizeRules(group));
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
    const rules = parseRuleCards();
    if (!name) {
      throw new Error('Group name is required.');
    }
    if (!egressVPN) {
      throw new Error('Egress VPN is required.');
    }
    if (rules.length === 0) {
      throw new Error('At least one rule with one selector is required.');
    }
    return { name, egressVpn: egressVPN, rules };
  }
  function parseRuleCards() {
    const cards = Array.from(rulesList.querySelectorAll('.routing-rule-card'));
    const rules = [];
    cards.forEach((card) => {
      const rule = {
        name: valueFrom(card, '.js-rule-name'),
        sourceCidrs: parseLines(valueFrom(card, '.js-rule-source')),
        destinationCidrs: parseLines(valueFrom(card, '.js-rule-destination')),
        destinationPorts: parsePorts(valueFrom(card, '.js-rule-ports')),
        destinationAsns: parseLines(valueFrom(card, '.js-rule-asn')),
        domains: parseLines(valueFrom(card, '.js-rule-domains')),
        wildcardDomains: parseLines(valueFrom(card, '.js-rule-wildcards')),
      };
      if (ruleHasSelectors(rule)) {
        rules.push(rule);
      }
    });
    return rules;
  }
  function valueFrom(card, selector) {
    const input = card.querySelector(selector);
    return input ? String(input.value || '').trim() : '';
  }
  function parseLines(rawValue) {
    return String(rawValue || '')
      .split('\n')
      .map((entry) => entry.trim())
      .filter((entry) => entry !== '');
  }
  function parsePorts(rawValue) {
    const parsed = [];
    parseLines(rawValue).forEach((entry) => {
      const compact = entry.replace(/\s+/g, '');
      const match = compact.match(/^(tcp|udp)[:/](\d{1,5})(?:-(\d{1,5}))?$/i);
      if (!match) {
        throw new Error(`Invalid port selector "${entry}". Use tcp:443 or udp:5000-5100.`);
      }
      const protocol = String(match[1] || '').toLowerCase();
      const start = Number(match[2] || 0);
      const end = match[3] ? Number(match[3]) : start;
      if (start <= 0 || start > 65535 || end <= 0 || end > 65535 || end < start) {
        throw new Error(`Invalid port selector "${entry}".`);
      }
      parsed.push({ protocol, start, end });
    });
    return parsed;
  }
  function ruleHasSelectors(rule) {
    return (
      rule.sourceCidrs.length > 0 ||
      rule.destinationCidrs.length > 0 ||
      rule.destinationPorts.length > 0 ||
      rule.destinationAsns.length > 0 ||
      rule.domains.length > 0 ||
      rule.wildcardDomains.length > 0
    );
  }
  function normalizeRules(group) {
    if (Array.isArray(group.rules) && group.rules.length > 0) {
      return group.rules.map((rule, index) => ({
        name: rule.name || `Rule ${index + 1}`,
        sourceCidrs: Array.isArray(rule.sourceCidrs) ? rule.sourceCidrs : [],
        destinationCidrs: Array.isArray(rule.destinationCidrs) ? rule.destinationCidrs : [],
        destinationPorts: Array.isArray(rule.destinationPorts) ? rule.destinationPorts : [],
        destinationAsns: Array.isArray(rule.destinationAsns) ? rule.destinationAsns : [],
        domains: Array.isArray(rule.domains) ? rule.domains : [],
        wildcardDomains: Array.isArray(rule.wildcardDomains) ? rule.wildcardDomains : [],
      }));
    }
    const legacyDomains = Array.isArray(group.domains) ? group.domains : [];
    if (legacyDomains.length === 0) {
      return [];
    }
    return [{
      name: 'Rule 1',
      sourceCidrs: [],
      destinationCidrs: [],
      destinationPorts: [],
      destinationAsns: [],
      domains: legacyDomains.filter((entry) => !String(entry).startsWith('*.' )),
      wildcardDomains: legacyDomains.filter((entry) => String(entry).startsWith('*.')),
    }];
  }
  function resetRules(rules) {
    rulesList.innerHTML = '';
    state.nextRuleID = 1;
    if (Array.isArray(rules) && rules.length > 0) {
      rules.forEach((rule) => appendRuleCard(rule));
      return;
    }
    appendRuleCard();
  }
  function appendRuleCard(rule = null) {
    const ruleID = state.nextRuleID++;
    const index = rulesList.children.length + 1;
    const payload = rule || {
      name: `Rule ${index}`,
      sourceCidrs: [],
      destinationCidrs: [],
      destinationPorts: [],
      destinationAsns: [],
      domains: [],
      wildcardDomains: [],
    };
    const card = document.createElement('div');
    card.className = 'routing-rule-card border rounded p-3 mb-3';
    card.setAttribute('data-rule-id', String(ruleID));
    card.innerHTML = `
      <div class="d-flex justify-content-between align-items-center mb-2">
        <label class="form-label mb-0">Rule</label>
        <button class="btn btn-outline-danger btn-sm" type="button" data-action="remove-rule">
          <i class="bi bi-trash"></i>
        </button>
      </div>
      <div class="row g-2">
        <div class="col-12">
          <input class="form-control form-control-sm js-rule-name" type="text" placeholder="Rule name" value="${escapeHTML(payload.name || '')}">
        </div>
        <div class="col-12 col-md-6">
          <label class="form-label small text-body-secondary mb-1">Source CIDRs</label>
          <textarea class="form-control form-control-sm font-monospace js-rule-source" rows="4" placeholder="10.0.0.0/24&#10;2001:db8::/64">${escapeHTML((payload.sourceCidrs || []).join('\n'))}</textarea>
        </div>
        <div class="col-12 col-md-6">
          <label class="form-label small text-body-secondary mb-1">Destination CIDRs</label>
          <textarea class="form-control form-control-sm font-monospace js-rule-destination" rows="4" placeholder="1.1.1.0/24&#10;2606:4700::/32">${escapeHTML((payload.destinationCidrs || []).join('\n'))}</textarea>
        </div>
        <div class="col-12 col-md-4">
          <label class="form-label small text-body-secondary mb-1">Destination Ports</label>
          <textarea class="form-control form-control-sm font-monospace js-rule-ports" rows="4" placeholder="tcp:443&#10;udp:5000-5100">${escapeHTML(formatPorts(payload.destinationPorts || []))}</textarea>
        </div>
        <div class="col-12 col-md-4">
          <label class="form-label small text-body-secondary mb-1">Destination ASNs</label>
          <textarea class="form-control form-control-sm font-monospace js-rule-asn" rows="4" placeholder="AS15169&#10;13335">${escapeHTML((payload.destinationAsns || []).join('\n'))}</textarea>
        </div>
        <div class="col-12 col-md-4">
          <label class="form-label small text-body-secondary mb-1">Domains</label>
          <textarea class="form-control form-control-sm font-monospace js-rule-domains" rows="4" placeholder="api.example.com">${escapeHTML((payload.domains || []).join('\n'))}</textarea>
        </div>
        <div class="col-12">
          <label class="form-label small text-body-secondary mb-1">Wildcard Domains</label>
          <textarea class="form-control form-control-sm font-monospace js-rule-wildcards" rows="3" placeholder="*.apple.com&#10;*.example.net">${escapeHTML((payload.wildcardDomains || []).join('\n'))}</textarea>
        </div>
      </div>
    `;
    rulesList.appendChild(card);
  }
  function formatPorts(ports) {
    if (!Array.isArray(ports)) {
      return '';
    }
    return ports
      .map((entry) => {
        const protocol = String(entry.protocol || '').toLowerCase();
        const start = Number(entry.start || 0);
        const end = Number(entry.end || start);
        if (!protocol || start <= 0) {
          return '';
        }
        return `${protocol}:${start}${end > start ? `-${end}` : ''}`;
      })
      .filter((entry) => entry !== '')
      .join('\n');
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
  function escapeHTML(value) {
    return String(value || '')
      .replaceAll('&', '&amp;')
      .replaceAll('<', '&lt;')
      .replaceAll('>', '&gt;')
      .replaceAll('"', '&quot;')
      .replaceAll("'", '&#39;');
  }
  initialize();
})();
