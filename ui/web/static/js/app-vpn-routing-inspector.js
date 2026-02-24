(() => {
  window.SplitVPNUI = window.SplitVPNUI || {};

  window.SplitVPNUI.createRoutingInspectorController = function createRoutingInspectorController(ctx) {
    const {
      routingInspectorModal,
      routingInspectorTitle,
      routingInspectorStatus,
      routingInspectorSummaryVPN,
      routingInspectorSummaryV4,
      routingInspectorSummaryV6,
      routingInspectorUpdatedAt,
      routingInspectorContent,
      fetchJSON,
      setStatus,
    } = ctx || {};

    if (
      !routingInspectorModal ||
      !routingInspectorTitle ||
      !routingInspectorStatus ||
      !routingInspectorSummaryVPN ||
      !routingInspectorSummaryV4 ||
      !routingInspectorSummaryV6 ||
      !routingInspectorUpdatedAt ||
      !routingInspectorContent ||
      typeof fetchJSON !== 'function'
    ) {
      return null;
    }

    async function open(name) {
      routingInspectorTitle.innerHTML = `<i class="bi bi-diagram-2 me-2"></i>Routing Set Inspector — ${escapeHTML(name)}`;
      routingInspectorSummaryVPN.textContent = `VPN: ${name}`;
      routingInspectorSummaryV4.textContent = 'IPv4: …';
      routingInspectorSummaryV6.textContent = 'IPv6: …';
      routingInspectorUpdatedAt.textContent = 'Updated: loading…';
      routingInspectorContent.innerHTML = '';
      setInspectorStatus('Loading routing sets…', false);
      routingInspectorModal.show();
      try {
        const payload = await fetchJSON(`/api/vpns/${encodeURIComponent(name)}/routing-inspector`);
        const inspector = payload?.inspector || {};
        render(inspector);
      } catch (err) {
        const message = err.message || 'Failed to load routing inspector data.';
        setInspectorStatus(message, true);
        if (typeof setStatus === 'function') {
          setStatus(message, true);
        }
      }
    }

    function render(inspector) {
      const groups = Array.isArray(inspector?.groups) ? inspector.groups : [];
      const v4 = Number(inspector?.routingV4Size || 0);
      const v6 = Number(inspector?.routingV6Size || 0);
      routingInspectorSummaryV4.textContent = `IPv4: ${Number.isFinite(v4) ? Math.max(0, Math.trunc(v4)) : 0}`;
      routingInspectorSummaryV6.textContent = `IPv6: ${Number.isFinite(v6) ? Math.max(0, Math.trunc(v6)) : 0}`;
      if (inspector?.generatedAt) {
        const stamp = new Date(inspector.generatedAt);
        if (!Number.isNaN(stamp.getTime())) {
          routingInspectorUpdatedAt.textContent = `Updated: ${stamp.toLocaleString()}`;
        } else {
          routingInspectorUpdatedAt.textContent = 'Updated: now';
        }
      } else {
        routingInspectorUpdatedAt.textContent = 'Updated: now';
      }
      if (groups.length === 0) {
        routingInspectorContent.innerHTML = '<div class="text-body-secondary small">No routing policy groups currently assigned to this VPN.</div>';
        setInspectorStatus('No routing groups assigned to this VPN.', false);
        return;
      }

      const blocks = groups.map((group) => {
        const rules = Array.isArray(group?.rules) ? group.rules : [];
        const ruleBlocks = rules.map((rule) => renderRoutingRuleBlock(rule)).join('');
        return `
          <div class="border rounded p-3 mb-3 routing-inspector-group">
            <div class="d-flex align-items-center justify-content-between mb-2">
              <div class="fw-semibold">${escapeHTML(group?.name || 'Group')}</div>
              <span class="badge text-bg-secondary">${rules.length} rule${rules.length === 1 ? '' : 's'}</span>
            </div>
            ${ruleBlocks || '<div class="text-body-secondary small">No rules in this group.</div>'}
          </div>
        `;
      }).join('');
      routingInspectorContent.innerHTML = blocks;
      setInspectorStatus(`Loaded ${groups.length} group${groups.length === 1 ? '' : 's'} for ${inspector?.vpnName || 'VPN'}.`, false);
    }

    function renderRoutingRuleBlock(rule) {
      const sourceInterfaces = joinValues(rule?.sourceInterfaces);
      const ports = formatPorts(rule?.destinationPorts);
      const asns = joinValues(rule?.destinationAsns);
      const domains = joinValues(rule?.domains);
      const wildcards = joinValues(rule?.wildcardDomains);
      const sourceMACs = formatSourceMACs(rule?.sourceMacs);
      return `
        <div class="routing-inspector-rule border rounded p-2 mb-3">
          <div class="d-flex flex-wrap align-items-center gap-2 mb-2">
            <span class="fw-semibold">${escapeHTML(ruleLabel(rule))}</span>
            ${renderSetBadge(rule?.sourceSetV4, 'src4')}
            ${renderSetBadge(rule?.sourceSetV6, 'src6')}
            ${renderSetBadge(rule?.destinationSetV4, 'dst4')}
            ${renderSetBadge(rule?.destinationSetV6, 'dst6')}
          </div>
          <div class="small text-body-secondary mb-2">
            <div><span class="fw-semibold text-body">Source interfaces:</span> ${escapeHTML(sourceInterfaces)}</div>
            <div><span class="fw-semibold text-body">Source MACs:</span> ${escapeHTML(sourceMACs)}</div>
            <div><span class="fw-semibold text-body">Destination ports:</span> ${escapeHTML(ports)}</div>
            <div><span class="fw-semibold text-body">Destination ASNs:</span> ${escapeHTML(asns)}</div>
            <div><span class="fw-semibold text-body">Domains:</span> ${escapeHTML(domains)}</div>
            <div><span class="fw-semibold text-body">Wildcard domains:</span> ${escapeHTML(wildcards)}</div>
          </div>
          ${renderSetDetails('Source IPv4 Set', rule?.sourceSetV4)}
          ${renderSetDetails('Source IPv6 Set', rule?.sourceSetV6)}
          ${renderSetDetails('Destination IPv4 Set', rule?.destinationSetV4)}
          ${renderSetDetails('Destination IPv6 Set', rule?.destinationSetV6)}
        </div>
      `;
    }

    function renderSetBadge(setInfo, label) {
      const setName = String(setInfo?.name || '').trim();
      if (!setName) {
        return '';
      }
      const count = Number(setInfo?.entryCount || 0);
      return `<span class="badge rounded-pill text-bg-info-subtle text-info-emphasis">${escapeHTML(label)}: ${Number.isFinite(count) ? Math.max(0, Math.trunc(count)) : 0}</span>`;
    }

    function renderSetDetails(title, setInfo) {
      const setName = String(setInfo?.name || '').trim();
      if (!setName) {
        return '';
      }
      const entries = Array.isArray(setInfo?.entries) ? setInfo.entries : [];
      const lines = entries.map((entry) => formatSetEntry(entry)).join('\n');
      const count = Number(setInfo?.entryCount || 0);
      const safeCount = Number.isFinite(count) ? Math.max(0, Math.trunc(count)) : 0;
      return `
        <details class="routing-inspector-set mb-2">
          <summary class="small">
            <span class="fw-semibold">${escapeHTML(title)}</span>
            <span class="text-body-secondary ms-2">${escapeHTML(setName)} (${safeCount})</span>
          </summary>
          <pre class="routing-inspector-pre mb-0 mt-2">${escapeHTML(lines || '(empty set)')}</pre>
        </details>
      `;
    }

    function formatSetEntry(entry) {
      if (!entry) {
        return '';
      }
      const parts = [];
      const value = String(entry.value || '').trim();
      if (value) {
        parts.push(value);
      }
      const canonical = String(entry.canonical || '').trim();
      if (canonical && canonical !== value) {
        parts.push(`canonical=${canonical}`);
      }
      const device = String(entry.deviceName || '').trim();
      if (device) {
        parts.push(`device=${device}`);
      }
      const provenance = Array.isArray(entry.provenance) ? entry.provenance.filter(Boolean) : [];
      if (provenance.length > 0) {
        parts.push(`via=${provenance.join('; ')}`);
      }
      return parts.join(' | ');
    }

    function formatSourceMACs(values) {
      const macs = Array.isArray(values) ? values : [];
      if (macs.length === 0) {
        return '—';
      }
      return macs.map((entry) => {
        const mac = String(entry?.mac || '').trim();
        if (!mac) {
          return '';
        }
        const name = String(entry?.deviceName || '').trim();
        const hints = Array.isArray(entry?.ipHints) ? entry.ipHints.filter(Boolean) : [];
        if (name && hints.length > 0) {
          return `${mac} (${name}; ${hints.join(', ')})`;
        }
        if (name) {
          return `${mac} (${name})`;
        }
        if (hints.length > 0) {
          return `${mac} (${hints.join(', ')})`;
        }
        return mac;
      }).filter(Boolean).join(', ');
    }

    function formatPorts(values) {
      const ports = Array.isArray(values) ? values : [];
      if (ports.length === 0) {
        return '—';
      }
      return ports.map((item) => {
        const proto = String(item?.protocol || '').trim() || 'both';
        const start = Number(item?.start || 0);
        const end = Number(item?.end || 0);
        if (!Number.isFinite(start) || start <= 0) {
          return proto;
        }
        if (Number.isFinite(end) && end > 0 && end !== start) {
          return `${proto}:${start}-${end}`;
        }
        return `${proto}:${start}`;
      }).join(', ');
    }

    function joinValues(values) {
      const list = Array.isArray(values) ? values.filter((value) => String(value || '').trim() !== '') : [];
      if (list.length === 0) {
        return '—';
      }
      return list.join(', ');
    }

    function ruleLabel(rule) {
      const index = Number(rule?.ruleIndex || 0);
      const name = String(rule?.ruleName || '').trim();
      if (name) {
        return `Rule ${index || '?'}: ${name}`;
      }
      return `Rule ${index || '?'}`;
    }

    function setInspectorStatus(message, isError) {
      routingInspectorStatus.classList.remove('d-none', 'alert-success', 'alert-danger');
      routingInspectorStatus.classList.add(isError ? 'alert-danger' : 'alert-success');
      routingInspectorStatus.textContent = message || '';
    }

    function escapeHTML(value) {
      return String(value || '')
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
    }

    return { open };
  };
})();
