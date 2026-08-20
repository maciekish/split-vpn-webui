(() => {
  const tableBody = document.querySelector('#remote-lists-table tbody');
  const emptyLabel = document.getElementById('remote-lists-empty');
  const statusBox = document.getElementById('remote-lists-status');
  const addButton = document.getElementById('open-add-remote-list');
  const refreshAllButton = document.getElementById('refresh-all-remote-lists');
  const modalElement = document.getElementById('remoteListModal');
  const entriesModalElement = document.getElementById('remoteListEntriesModal');
  const deleteModalElement = document.getElementById('deleteRemoteListModal');
  if (
    !tableBody ||
    !emptyLabel ||
    !statusBox ||
    !addButton ||
    !refreshAllButton ||
    !modalElement ||
    !entriesModalElement ||
    !deleteModalElement
  ) {
    return;
  }

  const modal = new bootstrap.Modal(modalElement);
  const entriesModal = new bootstrap.Modal(entriesModalElement);
  const deleteModal = new bootstrap.Modal(deleteModalElement);
  const modalTitle = document.getElementById('remote-list-modal-title');
  const modalStatus = document.getElementById('remote-list-modal-status');
  const nameInput = document.getElementById('remote-list-name');
  const kindSelect = document.getElementById('remote-list-kind');
  const urlInput = document.getElementById('remote-list-url');
  const intervalInput = document.getElementById('remote-list-interval-minutes');
  const enabledInput = document.getElementById('remote-list-enabled');
  const saveButton = document.getElementById('save-remote-list');
  const entriesTitle = document.getElementById('remote-list-entries-title');
  const entriesSummary = document.getElementById('remote-list-entries-summary');
  const entriesBody = document.getElementById('remote-list-entries-body');
  const deleteName = document.getElementById('delete-remote-list-name');
  const confirmDeleteButton = document.getElementById('confirm-delete-remote-list');
  const refreshConfigsButton = document.getElementById('refresh-configs');

  const escapeHTML = window.SplitVPNDomainRoutingUtils
    ? window.SplitVPNDomainRoutingUtils.escapeHTML
    : (value) => String(value || '');

  const KIND_LABELS = {
    cidr: 'CIDRs',
    asn: 'ASNs',
    domain: 'Domains',
    wildcard: 'Wildcards',
  };

  const state = {
    lists: [],
    editingID: null,
    pendingDeleteID: null,
  };

  addButton.addEventListener('click', () => openModal(null));
  refreshAllButton.addEventListener('click', async () => {
    refreshAllButton.disabled = true;
    try {
      const data = await fetchJSON('/api/remote-lists/refresh', { method: 'POST' });
      showStatus(summarizeRefresh(Array.isArray(data.refresh) ? data.refresh : []), false);
      await loadLists();
    } catch (err) {
      showStatus(err.message, true);
    } finally {
      refreshAllButton.disabled = false;
    }
  });

  saveButton.addEventListener('click', async () => {
    saveButton.disabled = true;
    try {
      await saveList();
    } catch (err) {
      showModalStatus(err.message, true);
    } finally {
      saveButton.disabled = false;
    }
  });

  tableBody.addEventListener('click', async (event) => {
    const target = event.target.closest('[data-action]');
    if (!target) {
      return;
    }
    const id = Number(target.getAttribute('data-list-id') || 0);
    const list = state.lists.find((entry) => Number(entry.id) === id);
    if (!list) {
      return;
    }
    const action = target.getAttribute('data-action');
    if (action === 'edit') {
      openModal(list);
      return;
    }
    if (action === 'delete') {
      state.pendingDeleteID = id;
      deleteName.textContent = list.name || '';
      deleteModal.show();
      return;
    }
    if (action === 'entries') {
      await openEntries(list);
      return;
    }
    if (action === 'refresh') {
      target.disabled = true;
      try {
        const data = await fetchJSON(`/api/remote-lists/${id}/refresh`, { method: 'POST' });
        showStatus(summarizeRefresh(data.refresh ? [data.refresh] : []), false);
        await loadLists();
      } catch (err) {
        showStatus(err.message, true);
      } finally {
        target.disabled = false;
      }
    }
  });

  confirmDeleteButton.addEventListener('click', async () => {
    const id = state.pendingDeleteID;
    if (!id) {
      return;
    }
    confirmDeleteButton.disabled = true;
    try {
      await fetchJSON(`/api/remote-lists/${id}`, { method: 'DELETE' });
      deleteModal.hide();
      state.pendingDeleteID = null;
      showStatus('Remote list deleted.', false);
      await loadLists();
    } catch (err) {
      deleteModal.hide();
      showStatus(err.message, true);
    } finally {
      confirmDeleteButton.disabled = false;
    }
  });

  if (refreshConfigsButton) {
    refreshConfigsButton.addEventListener('click', () => {
      loadLists().catch((err) => showStatus(err.message, true));
    });
  }

  function openModal(list) {
    state.editingID = list ? Number(list.id) : null;
    modalStatus.classList.add('d-none');
    modalTitle.innerHTML = list
      ? '<i class="bi bi-pencil-square me-2"></i>Edit Remote List'
      : '<i class="bi bi-cloud-download me-2"></i>Add Remote List';
    nameInput.value = list ? list.name || '' : '';
    kindSelect.value = list ? list.kind || 'cidr' : 'cidr';
    urlInput.value = list ? list.url || '' : '';
    intervalInput.value = list ? Math.round(Number(list.refreshIntervalSeconds || 0) / 60) || '' : 360;
    enabledInput.checked = list ? list.enabled !== false : true;
    modal.show();
  }

  async function saveList() {
    const name = String(nameInput.value || '').trim();
    const url = String(urlInput.value || '').trim();
    const minutes = Number(intervalInput.value || 0);
    if (!name) {
      throw new Error('List name is required.');
    }
    if (!url) {
      throw new Error('List URL is required.');
    }
    if (!Number.isFinite(minutes) || minutes < 5) {
      throw new Error('Refresh interval must be at least 5 minutes.');
    }
    const payload = {
      name,
      url,
      kind: String(kindSelect.value || 'cidr'),
      refreshIntervalSeconds: Math.round(minutes * 60),
      enabled: !!enabledInput.checked,
    };
    const data = state.editingID
      ? await fetchJSON(`/api/remote-lists/${state.editingID}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })
      : await fetchJSON('/api/remote-lists', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
    modal.hide();
    const refreshError = data.refresh && data.refresh.error ? data.refresh.error : '';
    if (refreshError) {
      showStatus(`Saved, but the first fetch failed: ${refreshError}`, true);
    } else {
      showStatus(state.editingID ? 'Remote list updated.' : 'Remote list created.', false);
    }
    await loadLists();
  }

  async function openEntries(list) {
    try {
      const data = await fetchJSON(`/api/remote-lists/${list.id}/entries`);
      const entries = Array.isArray(data.entries) ? data.entries : [];
      entriesTitle.innerHTML = `<i class="bi bi-list-ul me-2"></i>${escapeHTML(list.name)}`;
      entriesSummary.textContent = entries.length
        ? `${entries.length} entries (${KIND_LABELS[list.kind] || list.kind}).`
        : 'No entries cached yet.';
      entriesBody.textContent = entries.join('\n');
      entriesModal.show();
    } catch (err) {
      showStatus(err.message, true);
    }
  }

  async function loadLists() {
    const data = await fetchJSON('/api/remote-lists');
    const lists = Array.isArray(data.lists) ? data.lists : [];
    state.lists = lists;
    renderLists(lists);
    document.dispatchEvent(new CustomEvent('splitvpn:remote-lists', { detail: { lists } }));
  }

  function renderLists(lists) {
    tableBody.innerHTML = '';
    if (!lists.length) {
      emptyLabel.classList.remove('d-none');
      return;
    }
    emptyLabel.classList.add('d-none');
    lists.forEach((list) => {
      const row = document.createElement('tr');
      row.innerHTML = `
        <td class="fw-semibold">${escapeHTML(list.name)}${list.enabled === false ? ' <span class="badge text-bg-secondary">disabled</span>' : ''}</td>
        <td><span class="badge text-bg-primary">${escapeHTML(KIND_LABELS[list.kind] || list.kind)}</span></td>
        <td class="small font-monospace text-truncate" style="max-width: 22rem;" title="${escapeHTML(list.url)}">${escapeHTML(list.url)}</td>
        <td class="small">${formatInterval(list.refreshIntervalSeconds)}</td>
        <td class="text-end">${Number(list.entryCount || 0)}${Number(list.skippedCount || 0) > 0 ? ` <span class="text-warning small" title="Unparsable lines skipped">(+${Number(list.skippedCount)} skipped)</span>` : ''}</td>
        <td class="small">${formatTimestamp(list.lastSuccessAt)}</td>
        <td class="small">${renderStatus(list)}</td>
        <td class="text-end">
          <div class="btn-group btn-group-sm" role="group">
            <button class="btn btn-outline-info" data-action="entries" data-list-id="${list.id}" title="View entries">
              <i class="bi bi-list-ul"></i>
            </button>
            <button class="btn btn-outline-success" data-action="refresh" data-list-id="${list.id}" title="Refresh now">
              <i class="bi bi-arrow-repeat"></i>
            </button>
            <button class="btn btn-outline-light" data-action="edit" data-list-id="${list.id}" title="Edit list">
              <i class="bi bi-pencil"></i>
            </button>
            <button class="btn btn-outline-danger" data-action="delete" data-list-id="${list.id}" title="Delete list">
              <i class="bi bi-trash"></i>
            </button>
          </div>
        </td>`;
      tableBody.appendChild(row);
    });
  }

  function renderStatus(list) {
    if (list.lastError) {
      return `<span class="text-danger" title="${escapeHTML(list.lastError)}"><i class="bi bi-exclamation-triangle me-1"></i>Error</span>`;
    }
    if (!list.lastSuccessAt) {
      return '<span class="text-body-secondary">Pending</span>';
    }
    return '<span class="text-success">OK</span>';
  }

  function summarizeRefresh(results) {
    if (!results.length) {
      return 'Nothing to refresh.';
    }
    const failed = results.filter((entry) => entry && entry.error);
    const changed = results.filter((entry) => entry && entry.changed);
    if (failed.length) {
      return `${failed.length} of ${results.length} list(s) failed: ${failed[0].error}`;
    }
    if (!changed.length) {
      return `${results.length} list(s) refreshed, no content changes.`;
    }
    return `${changed.length} of ${results.length} list(s) changed; routing reapplied.`;
  }

  function formatInterval(seconds) {
    const total = Number(seconds || 0);
    if (total <= 0) {
      return '–';
    }
    if (total % 3600 === 0) {
      return `${total / 3600} h`;
    }
    return `${Math.round(total / 60)} min`;
  }

  function formatTimestamp(value) {
    const seconds = Number(value || 0);
    if (!seconds) {
      return 'Never';
    }
    return new Date(seconds * 1000).toLocaleString();
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
      throw new Error(response.statusText || 'Request failed');
    }
    return parsed || {};
  }

  function showStatus(message, isError) {
    statusBox.classList.remove('d-none', 'alert-success', 'alert-danger');
    statusBox.classList.add(isError ? 'alert-danger' : 'alert-success');
    statusBox.textContent = message || '';
    if (!isError) {
      setTimeout(() => statusBox.classList.add('d-none'), 4000);
    }
  }

  function showModalStatus(message, isError) {
    modalStatus.classList.remove('d-none', 'alert-success', 'alert-danger');
    modalStatus.classList.add(isError ? 'alert-danger' : 'alert-success');
    modalStatus.textContent = message || '';
  }

  loadLists().catch((err) => showStatus(err.message, true));
})();
