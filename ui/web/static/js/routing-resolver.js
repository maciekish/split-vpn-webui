(() => {
  const runResolverButton = document.getElementById('run-resolver-now');
  const clearResolverCacheButton = document.getElementById('clear-resolver-cache');
  const resolverLastRunAt = document.getElementById('resolver-last-run-at');
  const resolverLastDuration = document.getElementById('resolver-last-duration');
  const resolverLastSelectors = document.getElementById('resolver-last-selectors');
  const resolverLastPrefixes = document.getElementById('resolver-last-prefixes');
  const resolverRunningBadge = document.getElementById('resolver-running-badge');
  const resolverProgressWrap = document.getElementById('resolver-progress-wrap');
  const resolverProgressBar = document.getElementById('resolver-progress-bar');
  const resolverProgressLabel = document.getElementById('resolver-progress-label');
  const resolverProgressMeta = document.getElementById('resolver-progress-meta');
  const resolverProviderProgress = document.getElementById('resolver-provider-progress');
  const resolverIntervalMinutes = document.getElementById('resolver-interval-minutes');
  const resolverTimeoutSeconds = document.getElementById('resolver-timeout-seconds');
  const resolverParallelism = document.getElementById('resolver-parallelism');
  const resolverDomainTimeoutSeconds = document.getElementById('resolver-domain-timeout-seconds');
  const resolverAsnTimeoutSeconds = document.getElementById('resolver-asn-timeout-seconds');
  const resolverWildcardTimeoutSeconds = document.getElementById('resolver-wildcard-timeout-seconds');
  const resolverDomainEnabled = document.getElementById('resolver-domain-enabled');
  const resolverAsnEnabled = document.getElementById('resolver-asn-enabled');
  const resolverWildcardEnabled = document.getElementById('resolver-wildcard-enabled');
  const saveResolverSettingsButton = document.getElementById('save-resolver-settings');
  const groupsStatus = document.getElementById('domain-groups-status');
  const refreshButton = document.getElementById('refresh-configs');

  if (
    !runResolverButton ||
    !clearResolverCacheButton ||
    !resolverLastRunAt ||
    !resolverLastDuration ||
    !resolverLastSelectors ||
    !resolverLastPrefixes ||
    !resolverRunningBadge ||
    !resolverProgressWrap ||
    !resolverProgressBar ||
    !resolverProgressLabel ||
    !resolverProgressMeta ||
    !resolverProviderProgress ||
    !resolverIntervalMinutes ||
    !resolverTimeoutSeconds ||
    !resolverParallelism ||
    !resolverDomainTimeoutSeconds ||
    !resolverAsnTimeoutSeconds ||
    !resolverWildcardTimeoutSeconds ||
    !resolverDomainEnabled ||
    !resolverAsnEnabled ||
    !resolverWildcardEnabled ||
    !saveResolverSettingsButton
  ) {
    return;
  }

  let stream = null;
  let pollTimer = null;

  runResolverButton.addEventListener('click', async () => {
    runResolverButton.disabled = true;
    try {
      await fetchJSON('/api/resolver/run', { method: 'POST' });
      showStatus('Resolver run started.', false);
      await loadResolverStatus();
    } catch (err) {
      showStatus(err.message, true);
    } finally {
      runResolverButton.disabled = false;
    }
  });

  clearResolverCacheButton.addEventListener('click', async () => {
    if (!window.confirm('Clear resolver cache and immediately run resolver again?')) {
      return;
    }
    clearResolverCacheButton.disabled = true;
    try {
      await fetchJSON('/api/resolver/clear-run', { method: 'POST' });
      showStatus('Resolver cache cleared. Resolver run started.', false);
      await loadResolverStatus();
    } catch (err) {
      showStatus(err.message, true);
    } finally {
      clearResolverCacheButton.disabled = false;
    }
  });

  if (refreshButton) {
    refreshButton.addEventListener('click', async () => {
      await Promise.all([loadResolverStatus(), loadResolverSettings()]);
    });
  }

  saveResolverSettingsButton.addEventListener('click', async () => {
    saveResolverSettingsButton.disabled = true;
    try {
      await saveResolverSettings();
      showStatus('Resolver settings saved.', false);
    } catch (err) {
      showStatus(err.message, true);
    } finally {
      saveResolverSettingsButton.disabled = false;
    }
  });

  document.addEventListener('visibilitychange', () => {
    if (document.hidden) {
      disconnectStream();
    } else {
      connectStream();
    }
  });

  async function initialize() {
    await Promise.all([loadResolverStatus(), loadResolverSettings()]);
    connectStream();
  }

  async function loadResolverStatus() {
    const status = await fetchJSON('/api/resolver/status');
    renderResolverStatus(status);
    schedulePoll(Boolean(status.running));
  }

  function renderResolverStatus(status) {
    const run = status && status.lastRun ? status.lastRun : null;
    const running = Boolean(status && status.running);
    if (run && run.startedAt) {
      resolverLastRunAt.textContent = formatTimestamp(run.startedAt);
      resolverLastDuration.textContent = run.durationMs ? `${Number(run.durationMs).toFixed(0)} ms` : '–';
      resolverLastSelectors.textContent = `${run.selectorsDone || 0}/${run.selectorsTotal || 0}`;
      resolverLastPrefixes.textContent = String(run.prefixesResolved || 0);
      if (run.error) {
        showStatus(`Resolver error: ${run.error}`, true);
      }
    } else {
      resolverLastRunAt.textContent = 'Never';
      resolverLastDuration.textContent = '–';
      resolverLastSelectors.textContent = '–';
      resolverLastPrefixes.textContent = '–';
    }

    resolverRunningBadge.classList.toggle('d-none', !running);
    const progress = status && status.progress ? status.progress : null;
    if (!running || !progress) {
      resolverProgressWrap.classList.add('d-none');
      resolverProgressBar.style.width = '0%';
      resolverProgressBar.textContent = '';
      resolverProgressMeta.textContent = '';
      resolverProviderProgress.innerHTML = '';
      resolverProviderProgress.classList.add('d-none');
      return;
    }
    renderResolverProgress(progress);
  }

  function renderResolverProgress(progress) {
    const total = Number(progress.selectorsTotal || 0);
    const done = Number(progress.selectorsDone || 0);
    const percent = total > 0 ? Math.max(0, Math.min(100, Math.round((done / total) * 100))) : 0;
    resolverProgressWrap.classList.remove('d-none');
    resolverProgressBar.style.width = `${percent}%`;
    resolverProgressBar.textContent = `${percent}%`;
    resolverProgressLabel.textContent = progress.currentSelector || 'Resolving selectors...';
    resolverProgressMeta.textContent = `${done}/${total} selectors • ${progress.prefixesResolved || 0} prefixes`;
    renderProviderProgress(progress.perProvider || {});
  }

  function renderProviderProgress(perProvider) {
    const entries = Object.entries(perProvider || {});
    if (entries.length === 0) {
      resolverProviderProgress.innerHTML = '';
      resolverProviderProgress.classList.add('d-none');
      return;
    }
    const labels = {
      domain: 'Domains',
      asn: 'ASNs',
      wildcard: 'Wildcards',
    };
    resolverProviderProgress.classList.remove('d-none');
    resolverProviderProgress.innerHTML = entries
      .sort((a, b) => a[0].localeCompare(b[0]))
      .map(([key, value]) => {
        const total = Number(value?.selectorsTotal || 0);
        const done = Number(value?.selectorsDone || 0);
        const pct = total > 0 ? Math.max(0, Math.min(100, Math.round((done / total) * 100))) : 0;
        const prefixes = Number(value?.prefixesResolved || 0);
        return `
          <div class="col-12 col-md-4">
            <div class="small text-body-secondary mb-1">${labels[key] || key}</div>
            <div class="progress mb-1" role="progressbar" aria-label="${labels[key] || key} progress">
              <div class="progress-bar bg-secondary" style="width: ${pct}%">${pct}%</div>
            </div>
            <div class="small text-body-secondary">${done}/${total} selectors • ${prefixes} prefixes</div>
          </div>
        `;
      })
      .join('');
  }

  function schedulePoll(running) {
    if (pollTimer) {
      clearTimeout(pollTimer);
      pollTimer = null;
    }
    if (!running) {
      return;
    }
    pollTimer = setTimeout(async () => {
      try {
        await loadResolverStatus();
      } catch (err) {
        showStatus(err.message, true);
      }
    }, 4000);
  }

  function connectStream() {
    if (document.hidden) {
      return;
    }
    disconnectStream();
    stream = new EventSource('/api/stream');
    stream.addEventListener('resolver', (event) => {
      try {
        const progress = JSON.parse(event.data);
        resolverRunningBadge.classList.remove('d-none');
        renderResolverProgress(progress);
      } catch (err) {
        // Ignore malformed events and rely on polling.
      }
    });
    stream.onerror = () => {
      disconnectStream();
      setTimeout(connectStream, 4000);
    };
  }

  function disconnectStream() {
    if (stream) {
      stream.close();
      stream = null;
    }
  }

  async function loadResolverSettings() {
    const data = await fetchJSON('/api/settings');
    const current = data && data.settings ? data.settings : {};
    const intervalSeconds = Number(current.resolverIntervalSeconds || 0);
    const timeout = Number(current.resolverTimeoutSeconds || 0);
    const parallelism = Number(current.resolverParallelism || 0);
    const domainTimeout = Number(current.resolverDomainTimeoutSeconds || 0);
    const asnTimeout = Number(current.resolverAsnTimeoutSeconds || 0);
    const wildcardTimeout = Number(current.resolverWildcardTimeoutSeconds || 0);
    resolverIntervalMinutes.value = intervalSeconds > 0 ? Math.max(1, Math.round(intervalSeconds / 60)) : 60;
    resolverTimeoutSeconds.value = timeout > 0 ? timeout : 10;
    resolverParallelism.value = parallelism > 0 ? parallelism : 6;
    resolverDomainTimeoutSeconds.value = domainTimeout > 0 ? domainTimeout : (timeout > 0 ? timeout : 10);
    resolverAsnTimeoutSeconds.value = asnTimeout > 0 ? asnTimeout : (timeout > 0 ? timeout : 10);
    resolverWildcardTimeoutSeconds.value = wildcardTimeout > 0 ? wildcardTimeout : (timeout > 0 ? timeout : 10);
    resolverDomainEnabled.checked = current.resolverDomainEnabled !== false;
    resolverAsnEnabled.checked = current.resolverAsnEnabled !== false;
    resolverWildcardEnabled.checked = current.resolverWildcardEnabled !== false;
  }

  async function saveResolverSettings() {
    const intervalMinutes = Number(resolverIntervalMinutes.value || 0);
    const timeout = Number(resolverTimeoutSeconds.value || 0);
    const parallelism = Number(resolverParallelism.value || 0);
    const domainTimeout = Number(resolverDomainTimeoutSeconds.value || 0);
    const asnTimeout = Number(resolverAsnTimeoutSeconds.value || 0);
    const wildcardTimeout = Number(resolverWildcardTimeoutSeconds.value || 0);
    if (!Number.isFinite(intervalMinutes) || intervalMinutes <= 0) {
      throw new Error('Resolver interval must be a positive number of minutes.');
    }
    if (!Number.isFinite(timeout) || timeout <= 0) {
      throw new Error('Resolver timeout must be a positive number of seconds.');
    }
    if (!Number.isFinite(parallelism) || parallelism <= 0) {
      throw new Error('Resolver parallelism must be a positive number.');
    }
    if (!Number.isFinite(domainTimeout) || domainTimeout <= 0) {
      throw new Error('Domain resolver timeout must be a positive number of seconds.');
    }
    if (!Number.isFinite(asnTimeout) || asnTimeout <= 0) {
      throw new Error('ASN resolver timeout must be a positive number of seconds.');
    }
    if (!Number.isFinite(wildcardTimeout) || wildcardTimeout <= 0) {
      throw new Error('Wildcard resolver timeout must be a positive number of seconds.');
    }

    const data = await fetchJSON('/api/settings');
    const current = data && data.settings ? data.settings : {};
    const payload = {
      listenInterface: current.listenInterface || '',
      wanInterface: current.wanInterface || '',
      prewarmParallelism: Number(current.prewarmParallelism || 0),
      prewarmDoHTimeoutSeconds: Number(current.prewarmDoHTimeoutSeconds || 0),
      prewarmIntervalSeconds: Number(current.prewarmIntervalSeconds || 0),
      resolverParallelism: Math.round(parallelism),
      resolverTimeoutSeconds: Math.round(timeout),
      resolverIntervalSeconds: Math.round(intervalMinutes * 60),
      resolverDomainTimeoutSeconds: Math.round(domainTimeout),
      resolverAsnTimeoutSeconds: Math.round(asnTimeout),
      resolverWildcardTimeoutSeconds: Math.round(wildcardTimeout),
      resolverDomainEnabled: resolverDomainEnabled.checked,
      resolverAsnEnabled: resolverAsnEnabled.checked,
      resolverWildcardEnabled: resolverWildcardEnabled.checked,
      debugLogEnabled: current.debugLogEnabled === true,
      debugLogLevel: String(current.debugLogLevel || 'info').toLowerCase(),
    };
    await fetchJSON('/api/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    await loadResolverStatus();
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
    if (!groupsStatus) {
      return;
    }
    groupsStatus.classList.remove('d-none', 'alert-success', 'alert-danger');
    groupsStatus.classList.add(isError ? 'alert-danger' : 'alert-success');
    groupsStatus.textContent = message || '';
    if (!isError) {
      setTimeout(() => {
        groupsStatus.classList.add('d-none');
      }, 3500);
    }
  }

  function formatTimestamp(unixSeconds) {
    const value = Number(unixSeconds || 0);
    if (!Number.isFinite(value) || value <= 0) {
      return 'Never';
    }
    return new Date(value * 1000).toLocaleString();
  }

  initialize().catch((err) => {
    showStatus(err.message, true);
  });
})();
