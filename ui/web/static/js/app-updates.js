(() => {
  window.SplitVPNUI = window.SplitVPNUI || {};

  window.SplitVPNUI.createUpdateController = function createUpdateController(options = {}) {
    const settingsModalElement = options.settingsModalElement;
    const fetchJSON = options.fetchJSON;
    const setStatus = options.setStatus;

    const currentVersionEl = document.getElementById('update-current-version');
    const latestVersionEl = document.getElementById('update-latest-version');
    const lastCheckedEl = document.getElementById('update-last-checked');
    const stateEl = document.getElementById('update-state');
    const targetVersionInput = document.getElementById('update-target-version');
    const checkButton = document.getElementById('check-updates');
    const applyButton = document.getElementById('apply-update');

    if (
      !settingsModalElement ||
      !fetchJSON ||
      !setStatus ||
      !currentVersionEl ||
      !latestVersionEl ||
      !lastCheckedEl ||
      !stateEl ||
      !targetVersionInput ||
      !checkButton ||
      !applyButton
    ) {
      return null;
    }

    let currentStatus = null;
    let pollTimer = null;

    settingsModalElement.addEventListener('hidden.bs.modal', () => {
      stopPolling();
    });

    checkButton.addEventListener('click', async () => {
      checkButton.disabled = true;
      try {
        const payload = buildVersionPayload();
        const status = await fetchJSON('/api/update/check', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        currentStatus = status;
        renderStatus(status);
        setStatus('Release metadata refreshed.', false);
      } catch (err) {
        setStatus(err.message, true);
      } finally {
        checkButton.disabled = false;
      }
    });

    applyButton.addEventListener('click', async () => {
      const confirmed = window.confirm('Apply update now? The web UI service will restart.');
      if (!confirmed) {
        return;
      }
      applyButton.disabled = true;
      checkButton.disabled = true;
      try {
        const payload = buildVersionPayload();
        const status = await fetchJSON('/api/update/apply', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        currentStatus = status;
        renderStatus(status);
        setStatus('Update job scheduled. The UI may reconnect shortly.', false);
        schedulePolling();
      } catch (err) {
        setStatus(err.message, true);
      } finally {
        checkButton.disabled = false;
      }
    });

    async function refreshStatus() {
      try {
        const status = await fetchJSON('/api/update/status');
        currentStatus = status;
        renderStatus(status);
        if (status && status.inProgress) {
          schedulePolling();
        } else {
          stopPolling();
        }
      } catch (err) {
        renderErrorState(err.message);
      }
    }

    function buildVersionPayload() {
      const version = String(targetVersionInput.value || '').trim();
      return version ? { version } : {};
    }

    function renderStatus(status) {
      const currentVersion = status?.current?.version || 'unknown';
      const latestVersion = status?.latestVersion || 'Not checked';
      const state = String(status?.state || 'idle');
      const message = String(status?.message || '').trim();
      const lastError = String(status?.lastError || '').trim();
      const inProgress = Boolean(status?.inProgress);
      const updateAvailable = Boolean(status?.updateAvailable);
      const explicitTarget = String(targetVersionInput.value || '').trim() !== '';

      currentVersionEl.textContent = currentVersion;
      latestVersionEl.textContent = latestVersion;
      lastCheckedEl.textContent = formatTimestamp(status?.lastCheckedAt) || 'Never';

      let stateText = capitalize(state);
      if (message) {
        stateText += `: ${message}`;
      } else if (lastError) {
        stateText += `: ${lastError}`;
      }
      stateEl.textContent = stateText;

      const canApply = !inProgress && (updateAvailable || explicitTarget || currentVersion === 'dev');
      applyButton.disabled = !canApply;
      checkButton.disabled = inProgress;
      applyButton.innerHTML = inProgress
        ? '<i class="bi bi-hourglass-split me-1"></i>In Progress'
        : '<i class="bi bi-arrow-repeat me-1"></i>Update';
    }

    function renderErrorState(message) {
      currentVersionEl.textContent = 'unknown';
      latestVersionEl.textContent = 'Unavailable';
      lastCheckedEl.textContent = 'Never';
      stateEl.textContent = `Failed: ${message}`;
      applyButton.disabled = true;
    }

    function schedulePolling() {
      stopPolling();
      pollTimer = setTimeout(async () => {
        await refreshStatus();
      }, 3500);
    }

    function stopPolling() {
      if (pollTimer) {
        clearTimeout(pollTimer);
        pollTimer = null;
      }
    }

    function formatTimestamp(value) {
      if (!value) {
        return '';
      }
      const date = new Date(value);
      if (Number.isNaN(date.getTime())) {
        return '';
      }
      return date.toLocaleString();
    }

    function capitalize(value) {
      const text = String(value || '').trim();
      if (!text) {
        return '';
      }
      return text.charAt(0).toUpperCase() + text.slice(1);
    }

    return {
      refreshStatus,
    };
  };
})();
