(() => {
  const runNowButton = document.getElementById('run-prewarm-now');
  const clearPrewarmCacheButton = document.getElementById('clear-prewarm-cache');
  const saveScheduleButton = document.getElementById('save-prewarm-schedule');
  const prewarmStatus = document.getElementById('prewarm-status');
  const prewarmLastRunAt = document.getElementById('prewarm-last-run-at');
  const prewarmLastDuration = document.getElementById('prewarm-last-duration');
  const prewarmLastDomains = document.getElementById('prewarm-last-domains');
  const prewarmLastIPs = document.getElementById('prewarm-last-ips');
  const prewarmIntervalMinutes = document.getElementById('prewarm-interval-minutes');
  const prewarmExtraNameservers = document.getElementById('prewarm-extra-nameservers');
  const prewarmEcsProfiles = document.getElementById('prewarm-ecs-profiles');
  const prewarmProgressWrap = document.getElementById('prewarm-progress-wrap');
  const prewarmProgressBar = document.getElementById('prewarm-progress-bar');
  const prewarmProgressLabel = document.getElementById('prewarm-progress-label');
  const prewarmProgressMeta = document.getElementById('prewarm-progress-meta');
  const prewarmPerVPNProgress = document.getElementById('prewarm-per-vpn-progress');
  const settingsModalElement = document.getElementById('settingsModal');
  const currentPasswordInput = document.getElementById('current-password');
  const newPasswordInput = document.getElementById('new-password');
  const changePasswordButton = document.getElementById('change-password');
  const tokenInput = document.getElementById('api-token');
  const copyTokenButton = document.getElementById('copy-token');
  const regenerateTokenButton = document.getElementById('regenerate-token');
  const downloadBackupButton = document.getElementById('download-backup');
  const restoreBackupFileInput = document.getElementById('restore-backup-file');
  const restoreBackupButton = document.getElementById('restore-backup');
  if (
    !runNowButton ||
    !clearPrewarmCacheButton ||
    !saveScheduleButton ||
    !prewarmStatus ||
    !prewarmLastRunAt ||
    !prewarmLastDuration ||
    !prewarmLastDomains ||
    !prewarmLastIPs ||
    !prewarmIntervalMinutes ||
    !prewarmExtraNameservers ||
    !prewarmEcsProfiles ||
    !prewarmProgressWrap ||
    !prewarmProgressBar ||
    !prewarmProgressLabel ||
    !prewarmProgressMeta ||
    !prewarmPerVPNProgress ||
    !settingsModalElement ||
    !currentPasswordInput ||
    !newPasswordInput ||
    !changePasswordButton ||
    !tokenInput ||
    !copyTokenButton ||
    !regenerateTokenButton ||
    !downloadBackupButton ||
    !restoreBackupFileInput ||
    !restoreBackupButton
  ) {
    return;
  }
  let prewarmStream = null;
  let hideStatusTimer = null;
  runNowButton.addEventListener('click', async () => {
    runNowButton.disabled = true;
    try {
      await triggerPrewarm();
    } catch (err) {
      showPrewarmStatus(err.message, true);
    } finally {
      runNowButton.disabled = false;
    }
  });
  clearPrewarmCacheButton.addEventListener('click', async () => {
    if (!window.confirm('Clear pre-warm cache and immediately run pre-warm again?')) {
      return;
    }
    clearPrewarmCacheButton.disabled = true;
    try {
      await fetchJSON('/api/prewarm/clear-run', { method: 'POST' });
      showPrewarmStatus('Pre-warm cache cleared. Pre-warm run started.', false);
      prewarmProgressWrap.classList.remove('d-none');
    } catch (err) {
      showPrewarmStatus(err.message, true);
    } finally {
      clearPrewarmCacheButton.disabled = false;
    }
  });
  saveScheduleButton.addEventListener('click', async () => {
    saveScheduleButton.disabled = true;
    try {
      await saveSchedule();
    } catch (err) {
      showPrewarmStatus(err.message, true);
    } finally {
      saveScheduleButton.disabled = false;
    }
  });
  settingsModalElement.addEventListener('shown.bs.modal', async () => {
    try {
      await loadAuthToken();
    } catch (err) {
      showPrewarmStatus(err.message, true);
    }
  });
  changePasswordButton.addEventListener('click', async () => {
    changePasswordButton.disabled = true;
    try {
      await changePassword();
    } catch (err) {
      showPrewarmStatus(err.message, true);
    } finally {
      changePasswordButton.disabled = false;
    }
  });
  regenerateTokenButton.addEventListener('click', async () => {
    regenerateTokenButton.disabled = true;
    try {
      await regenerateToken();
    } catch (err) {
      showPrewarmStatus(err.message, true);
    } finally {
      regenerateTokenButton.disabled = false;
    }
  });
  copyTokenButton.addEventListener('click', async () => {
    try {
      await copyToken();
      showPrewarmStatus('API token copied.', false);
    } catch (err) {
      showPrewarmStatus('Failed to copy API token.', true);
    }
  });
  downloadBackupButton.addEventListener('click', async () => {
    downloadBackupButton.disabled = true;
    try {
      await downloadBackup();
      showPrewarmStatus('Backup downloaded.', false);
    } catch (err) {
      showPrewarmStatus(err.message, true);
    } finally {
      downloadBackupButton.disabled = false;
    }
  });
  restoreBackupButton.addEventListener('click', async () => {
    const file = restoreBackupFileInput.files && restoreBackupFileInput.files[0];
    if (!file) {
      showPrewarmStatus('Choose a backup file first.', true);
      return;
    }
    if (!window.confirm('Restore this backup? Current VPN and policy configuration will be replaced.')) {
      return;
    }
    restoreBackupButton.disabled = true;
    try {
      const payload = await restoreBackup(file);
      const warnings = Array.isArray(payload.warnings) ? payload.warnings : [];
      if (warnings.length > 0) {
        showPrewarmStatus(`Restore complete with warnings: ${warnings.join(' | ')}`, true);
      } else {
        showPrewarmStatus('Restore complete. Reloading session…', false);
      }
      restoreBackupFileInput.value = '';
      setTimeout(() => window.location.reload(), 1200);
    } catch (err) {
      showPrewarmStatus(err.message, true);
    } finally {
      restoreBackupButton.disabled = false;
    }
  });
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) {
      disconnectPrewarmStream();
    } else {
      connectPrewarmStream();
    }
  });
  async function initialize() {
    await loadPrewarmStatus();
    await loadScheduleFromSettings();
    connectPrewarmStream();
  }
  async function triggerPrewarm() {
    await fetchJSON('/api/prewarm/run', { method: 'POST' });
    showPrewarmStatus('Pre-warm started.', false);
    prewarmProgressWrap.classList.remove('d-none');
  }
  async function loadPrewarmStatus() {
    const status = await fetchJSON('/api/prewarm/status');
    renderPrewarmStatus(status);
  }
  function renderPrewarmStatus(status) {
    const lastRun = status?.lastRun || null;
    if (!lastRun) {
      prewarmLastRunAt.textContent = 'Never';
      prewarmLastDuration.textContent = '–';
      prewarmLastDomains.textContent = '–';
      prewarmLastIPs.textContent = '–';
    } else {
      prewarmLastRunAt.textContent = formatDateTime(lastRun.finishedAt || lastRun.startedAt);
      prewarmLastDuration.textContent = formatDuration(lastRun.durationMs);
      prewarmLastDomains.textContent = `${Number(lastRun.domainsDone || 0)}/${Number(lastRun.domainsTotal || 0)}`;
      prewarmLastIPs.textContent = `${Number(lastRun.ipsInserted || 0)}`;
      if (lastRun.error) {
        showPrewarmStatus(`Last run error: ${lastRun.error}`, true);
      }
    }
    if (status?.running && status.progress) {
      renderPrewarmProgress(status.progress);
      return;
    }
    prewarmProgressWrap.classList.add('d-none');
    prewarmPerVPNProgress.classList.add('d-none');
    prewarmPerVPNProgress.innerHTML = '';
  }
  function connectPrewarmStream() {
    if (document.hidden) {
      return;
    }
    disconnectPrewarmStream();
    prewarmStream = new EventSource('/api/stream');
    prewarmStream.addEventListener('prewarm', (event) => {
      try {
        const progress = JSON.parse(event.data);
        renderPrewarmProgress(progress);
      } catch (err) {
        // Ignore malformed event payloads.
      }
    });
    prewarmStream.onerror = () => {
      disconnectPrewarmStream();
      setTimeout(connectPrewarmStream, 4000);
    };
  }
  function disconnectPrewarmStream() {
    if (prewarmStream) {
      prewarmStream.close();
      prewarmStream = null;
    }
  }
  function renderPrewarmProgress(progress) {
    const total = Number(progress?.totalDomains || 0);
    const processed = Number(progress?.processedDomains || 0);
    const ips = Number(progress?.totalIps || 0);
    const pct = total > 0 ? Math.max(0, Math.min(100, Math.round((processed / total) * 100))) : 0;
    prewarmProgressWrap.classList.remove('d-none');
    prewarmProgressBar.style.width = `${pct}%`;
    prewarmProgressBar.textContent = `${pct}%`;
    prewarmProgressLabel.textContent = total > 0 ? `Domains ${processed}/${total}` : 'Preparing run...';
    prewarmProgressMeta.textContent = `IPs inserted: ${ips}`;
    renderPerVPNProgress(progress?.perVpn || {});
    const completed = total > 0 && processed >= total;
    if (completed) {
      prewarmProgressBar.classList.remove('progress-bar-animated', 'progress-bar-striped');
      setTimeout(async () => {
        prewarmProgressBar.classList.add('progress-bar-animated', 'progress-bar-striped');
        await loadPrewarmStatus();
      }, 1000);
    }
  }
  function renderPerVPNProgress(perVpn) {
    const entries = Object.entries(perVpn || {});
    if (entries.length === 0) {
      prewarmPerVPNProgress.classList.add('d-none');
      prewarmPerVPNProgress.innerHTML = '';
      return;
    }
    prewarmPerVPNProgress.classList.remove('d-none');
    prewarmPerVPNProgress.innerHTML = entries
      .sort((a, b) => a[0].localeCompare(b[0]))
      .map(([iface, item]) => {
        const total = Number(item?.totalDomains || 0);
        const done = Number(item?.domainsProcessed || 0);
        const inserted = Number(item?.ipsInserted || 0);
        const errors = Number(item?.errors || 0);
        const pct = total > 0 ? Math.max(0, Math.min(100, Math.round((done / total) * 100))) : 0;
        return `
          <div class="col-12 col-md-4">
            <div class="small text-body-secondary mb-1">${iface}</div>
            <div class="progress mb-1" role="progressbar" aria-label="${iface} progress">
              <div class="progress-bar bg-secondary" style="width: ${pct}%">${pct}%</div>
            </div>
            <div class="small text-body-secondary">${done}/${total} domains • ${inserted} IPs • ${errors} errors</div>
          </div>
        `;
      })
      .join('');
  }
  async function loadScheduleFromSettings() {
    const data = await fetchJSON('/api/settings');
    const settings = data?.settings || {};
    const intervalSeconds = Number(settings.prewarmIntervalSeconds || 0);
    const intervalMinutes = intervalSeconds > 0 ? Math.max(1, Math.round(intervalSeconds / 60)) : 120;
    prewarmIntervalMinutes.value = intervalMinutes;
    prewarmExtraNameservers.value = String(settings.prewarmExtraNameservers || '');
    prewarmEcsProfiles.value = String(settings.prewarmEcsProfiles || '');
  }
  async function saveSchedule() {
    const rawMinutes = Number(prewarmIntervalMinutes.value || 0);
    if (!Number.isFinite(rawMinutes) || rawMinutes <= 0) {
      throw new Error('Schedule must be a positive number of minutes.');
    }
    const data = await fetchJSON('/api/settings');
    const current = data?.settings || {};
    const nameservers = String(prewarmExtraNameservers.value || '');
    const ecsProfiles = String(prewarmEcsProfiles.value || '');
    const payload = {
      listenInterface: current.listenInterface || '',
      wanInterface: current.wanInterface || '',
      prewarmParallelism: Number(current.prewarmParallelism || 0),
      prewarmDoHTimeoutSeconds: Number(current.prewarmDoHTimeoutSeconds || 0),
      prewarmIntervalSeconds: Math.round(rawMinutes * 60),
      prewarmExtraNameservers: nameservers,
      prewarmEcsProfiles: ecsProfiles,
      resolverParallelism: Number(current.resolverParallelism || 0),
      resolverTimeoutSeconds: Number(current.resolverTimeoutSeconds || 0),
      resolverIntervalSeconds: Number(current.resolverIntervalSeconds || 0),
      resolverDomainTimeoutSeconds: Number(current.resolverDomainTimeoutSeconds || 0),
      resolverAsnTimeoutSeconds: Number(current.resolverAsnTimeoutSeconds || 0),
      resolverWildcardTimeoutSeconds: Number(current.resolverWildcardTimeoutSeconds || 0),
      resolverDomainEnabled: current.resolverDomainEnabled !== false,
      resolverAsnEnabled: current.resolverAsnEnabled !== false,
      resolverWildcardEnabled: current.resolverWildcardEnabled !== false,
      debugLogEnabled: current.debugLogEnabled === true,
      debugLogLevel: String(current.debugLogLevel || 'info').toLowerCase(),
    };
    await fetchJSON('/api/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    prewarmExtraNameservers.value = nameservers.replace(/\r\n/g, '\n').replace(/\n+$/g, '');
    prewarmEcsProfiles.value = ecsProfiles.replace(/\r\n/g, '\n').replace(/\n+$/g, '');
    showPrewarmStatus('Pre-warm settings saved.', false);
  }
  async function loadAuthToken() {
    const response = await fetchJSON('/api/auth/token');
    tokenInput.value = response?.token || '';
  }
  async function changePassword() {
    const currentPassword = currentPasswordInput.value || '';
    const newPassword = newPasswordInput.value || '';
    if (!currentPassword.trim() || !newPassword.trim()) {
      throw new Error('Both current and new password are required.');
    }
    await fetchJSON('/api/auth/password', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ currentPassword, newPassword }),
    });
    currentPasswordInput.value = '';
    newPasswordInput.value = '';
    showPrewarmStatus('Password updated.', false);
  }
  async function regenerateToken() {
    const response = await fetchJSON('/api/auth/token', { method: 'POST' });
    tokenInput.value = response?.token || '';
    showPrewarmStatus('API token regenerated.', false);
  }
  async function downloadBackup() {
    const response = await fetch('/api/backup/export');
    if (!response.ok) {
      throw await responseError(response);
    }
    const blob = await response.blob();
    const url = window.URL.createObjectURL(blob);
    try {
      const link = document.createElement('a');
      link.href = url;
      link.download = filenameFromContentDisposition(response.headers.get('content-disposition')) || 'split-vpn-webui-backup.json';
      document.body.appendChild(link);
      link.click();
      link.remove();
    } finally {
      window.URL.revokeObjectURL(url);
    }
  }
  async function restoreBackup(file) {
    const body = new FormData();
    body.append('file', file, file.name || 'backup.json');
    return fetchJSON('/api/backup/import', {
      method: 'POST',
      body,
    });
  }
  async function copyToken() {
    const token = tokenInput.value || '';
    if (!token) {
      throw new Error('Token unavailable.');
    }
    if (navigator.clipboard && navigator.clipboard.writeText) {
      await navigator.clipboard.writeText(token);
      return;
    }
    tokenInput.select();
    tokenInput.setSelectionRange(0, token.length);
    if (!document.execCommand('copy')) {
      throw new Error('copy failed');
    }
  }
  function showPrewarmStatus(message, isError) {
    prewarmStatus.classList.remove('d-none', 'alert-success', 'alert-danger');
    prewarmStatus.classList.add(isError ? 'alert-danger' : 'alert-success');
    prewarmStatus.textContent = message || '';
    if (hideStatusTimer) {
      clearTimeout(hideStatusTimer);
      hideStatusTimer = null;
    }
    if (!isError) {
      hideStatusTimer = setTimeout(() => {
        prewarmStatus.classList.add('d-none');
      }, 3500);
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
  async function responseError(response) {
    const contentType = response.headers.get('content-type') || '';
    if (contentType.includes('application/json')) {
      try {
        const payload = await response.json();
        if (payload && typeof payload.error === 'string' && payload.error) {
          return new Error(payload.error);
        }
      } catch (err) {
        // Fall back to text.
      }
    }
    try {
      const text = await response.text();
      if (text) {
        return new Error(text);
      }
    } catch (err) {
      // Ignore and fall back to status.
    }
    return new Error(response.statusText || 'Request failed');
  }
  function filenameFromContentDisposition(raw) {
    const value = String(raw || '');
    const match = value.match(/filename="([^"]+)"/i) || value.match(/filename=([^;]+)/i);
    if (!match || !match[1]) {
      return '';
    }
    return match[1].trim();
  }
  function formatDateTime(unixSeconds) {
    const value = Number(unixSeconds || 0);
    if (!value) {
      return 'Never';
    }
    const date = new Date(value * 1000);
    if (Number.isNaN(date.getTime())) {
      return 'Never';
    }
    return date.toLocaleString();
  }
  function formatDuration(ms) {
    const value = Number(ms || 0);
    if (!value || value < 0) {
      return '–';
    }
    if (value < 1000) {
      return `${Math.round(value)} ms`;
    }
    const sec = value / 1000;
    if (sec < 60) {
      return `${sec.toFixed(1)} s`;
    }
    const min = Math.floor(sec / 60);
    const remSec = Math.round(sec % 60);
    return `${min}m ${remSec}s`;
  }
  initialize().catch((err) => {
    showPrewarmStatus(err.message, true);
  });
})();
