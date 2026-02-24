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
      routingInspectorSearch,
      routingInspectorSearchRegex,
      routingInspectorSearchMeta,
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

    const lineSearchFactory = window.SplitVPNUI && typeof window.SplitVPNUI.createLineSearchController === 'function'
      ? window.SplitVPNUI.createLineSearchController
      : null;
    const lineSearch = lineSearchFactory
      ? lineSearchFactory({
        root: routingInspectorContent,
        input: routingInspectorSearch,
        regexToggle: routingInspectorSearchRegex,
        meta: routingInspectorSearchMeta,
      })
      : { apply: () => {}, refreshTargets: () => {}, reset: () => {} };

    async function open(name) {
      routingInspectorTitle.innerHTML = `<i class="bi bi-diagram-2 me-2"></i>Routing Set Inspector — ${escapeHTML(name)}`;
      routingInspectorSummaryVPN.textContent = `VPN: ${name}`;
      routingInspectorSummaryV4.textContent = 'IPv4: …';
      routingInspectorSummaryV6.textContent = 'IPv6: …';
      routingInspectorUpdatedAt.textContent = 'Updated: loading…';
      lineSearch.reset();
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

      routingInspectorContent.innerHTML = '';
      if (groups.length === 0) {
        const empty = document.createElement('div');
        empty.className = 'text-body-secondary small';
        empty.textContent = 'No routing policy groups currently assigned to this VPN.';
        routingInspectorContent.appendChild(empty);
        lineSearch.refreshTargets();
        setInspectorStatus('No routing groups assigned to this VPN.', false);
        return;
      }

      const fragment = document.createDocumentFragment();
      groups.forEach((group) => {
        fragment.appendChild(renderGroupBlock(group));
      });
      routingInspectorContent.appendChild(fragment);
      lineSearch.refreshTargets();
      lineSearch.apply();
      setInspectorStatus(`Loaded ${groups.length} group${groups.length === 1 ? '' : 's'} for ${inspector?.vpnName || 'VPN'}.`, false);
    }

    function renderGroupBlock(group) {
      const rules = Array.isArray(group?.rules) ? group.rules : [];

      const wrapper = document.createElement('div');
      wrapper.className = 'border rounded p-3 mb-3 routing-inspector-group';
      wrapper.dataset.searchScope = 'group';

      const header = document.createElement('div');
      header.className = 'd-flex align-items-center justify-content-between mb-2';

      const title = createSearchLine(String(group?.name || 'Group'), {
        className: 'fw-semibold mb-0',
      });
      const badge = document.createElement('span');
      badge.className = 'badge text-bg-secondary';
      badge.textContent = `${rules.length} rule${rules.length === 1 ? '' : 's'}`;

      header.appendChild(title);
      header.appendChild(badge);
      wrapper.appendChild(header);

      if (rules.length === 0) {
        wrapper.appendChild(createSearchLine('No rules in this group.', {
          className: 'text-body-secondary small',
        }));
        return wrapper;
      }

      rules.forEach((rule) => {
        wrapper.appendChild(renderRoutingRuleBlock(rule));
      });
      return wrapper;
    }

    function renderRoutingRuleBlock(rule) {
      const sourceInterfaces = joinValues(rule?.sourceInterfaces);
      const ports = formatPorts(rule?.destinationPorts);
      const asns = joinValues(rule?.destinationAsns);
      const domains = joinValues(rule?.domains);
      const wildcards = joinValues(rule?.wildcardDomains);
      const sourceMACs = formatSourceMACs(rule?.sourceMacs);

      const wrapper = document.createElement('div');
      wrapper.className = 'routing-inspector-rule border rounded p-2 mb-3';
      wrapper.dataset.searchScope = 'rule';

      const titleRow = document.createElement('div');
      titleRow.className = 'd-flex flex-wrap align-items-center gap-2 mb-2';
      titleRow.appendChild(createSearchLine(ruleLabel(rule), {
        className: 'fw-semibold mb-0',
        tag: 'span',
      }));
      appendSetBadge(titleRow, rule?.sourceSetV4, 'src4');
      appendSetBadge(titleRow, rule?.sourceSetV6, 'src6');
      appendSetBadge(titleRow, rule?.destinationSetV4, 'dst4');
      appendSetBadge(titleRow, rule?.destinationSetV6, 'dst6');
      wrapper.appendChild(titleRow);

      const meta = document.createElement('div');
      meta.className = 'small text-body-secondary mb-2';
      meta.appendChild(createSearchLine(`Source interfaces: ${sourceInterfaces}`));
      meta.appendChild(createSearchLine(`Source MACs: ${sourceMACs}`));
      meta.appendChild(createSearchLine(`Destination ports: ${ports}`));
      meta.appendChild(createSearchLine(`Destination ASNs: ${asns}`));
      meta.appendChild(createSearchLine(`Domains: ${domains}`));
      meta.appendChild(createSearchLine(`Wildcard domains: ${wildcards}`));
      wrapper.appendChild(meta);

      const blocks = [
        renderSetDetails('Source IPv4 Set', rule?.sourceSetV4),
        renderSetDetails('Source IPv6 Set', rule?.sourceSetV6),
        renderSetDetails('Destination IPv4 Set', rule?.destinationSetV4),
        renderSetDetails('Destination IPv6 Set', rule?.destinationSetV6),
      ];
      blocks.forEach((block) => {
        if (block) {
          wrapper.appendChild(block);
        }
      });
      return wrapper;
    }

    function appendSetBadge(parent, setInfo, label) {
      const setName = String(setInfo?.name || '').trim();
      if (!setName) {
        return;
      }
      const count = Number(setInfo?.entryCount || 0);
      const badge = document.createElement('span');
      badge.className = 'badge rounded-pill text-bg-info-subtle text-info-emphasis';
      badge.textContent = `${label}: ${Number.isFinite(count) ? Math.max(0, Math.trunc(count)) : 0}`;
      parent.appendChild(badge);
    }

    function renderSetDetails(title, setInfo) {
      const setName = String(setInfo?.name || '').trim();
      if (!setName) {
        return null;
      }

      const entries = Array.isArray(setInfo?.entries) ? setInfo.entries : [];
      const count = Number(setInfo?.entryCount || 0);
      const safeCount = Number.isFinite(count) ? Math.max(0, Math.trunc(count)) : 0;

      const details = document.createElement('details');
      details.className = 'routing-inspector-set mb-2';
      details.dataset.searchScope = 'set';

      const summary = document.createElement('summary');
      summary.className = 'small';
      summary.appendChild(createSearchLine(`${title} ${setName} (${safeCount})`, {
        className: 'routing-inspector-summary-line',
        tag: 'span',
      }));
      details.appendChild(summary);

      const pre = document.createElement('div');
      pre.className = 'routing-inspector-pre mb-0 mt-2';
      if (entries.length === 0) {
        pre.appendChild(createSearchLine('(empty set)', {
          className: 'routing-inspector-pre-line',
        }));
      } else {
        entries.forEach((entry) => {
          pre.appendChild(createSearchLine(formatSetEntry(entry), {
            className: 'routing-inspector-pre-line',
          }));
        });
      }
      details.appendChild(pre);
      return details;
    }

    function createSearchLine(text, opts = {}) {
      const value = String(text || '');
      const tag = opts.tag || 'div';
      const line = document.createElement(tag);
      line.dataset.searchLine = '1';
      line.dataset.searchRaw = value;
      line.className = `routing-inspector-line ${opts.className || ''}`.trim();
      line.textContent = value;
      return line;
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
