(() => {
  window.SplitVPNDomainRoutingASNPreview = {
    createController(ctx) {
      const {
        modal,
        titleElement,
        statusElement,
        summaryElement,
        tableBodyElement,
        fetchJSON,
      } = ctx || {};

      if (
        !modal ||
        !titleElement ||
        !statusElement ||
        !summaryElement ||
        !tableBodyElement ||
        typeof fetchJSON !== 'function'
      ) {
        return null;
      }

      function open(request) {
        const asns = normalizeASNInputs(request?.asns || []);
        const title = String(request?.title || 'ASN Entry Preview');
        titleElement.textContent = title;
        clearStatus();
        renderLoading();
        modal.show();
        if (asns.length === 0) {
          renderEmpty('Enter at least one ASN to preview ipset entries.');
          return;
        }
        runPreview(asns);
      }

      async function runPreview(asns) {
        try {
          const payload = await fetchJSON('/api/routing/asn-preview', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ asns }),
          });
          renderResult(payload?.result || {});
        } catch (err) {
          renderError(err?.message || 'ASN preview failed.');
        }
      }

      function renderResult(result) {
        const items = Array.isArray(result.items) ? result.items : [];
        const resolvedCount = items.filter((item) => !String(item?.error || '').trim()).length;
        const totalCount = items.length;
        const totalEntriesV4 = Number(result.totalEntriesV4 || 0);
        const totalEntriesV6 = Number(result.totalEntriesV6 || 0);
        const totalPrefixesV4 = Number(result.totalPrefixesV4 || 0);
        const totalPrefixesV6 = Number(result.totalPrefixesV6 || 0);
        summaryElement.textContent = `Resolved ${resolvedCount}/${totalCount} • Entries v4/v6: ${totalEntriesV4}/${totalEntriesV6} • Prefixes v4/v6: ${totalPrefixesV4}/${totalPrefixesV6}`;
        tableBodyElement.innerHTML = items
          .map((item) => {
            const error = String(item?.error || '').trim();
            return `
              <tr>
                <td class="font-monospace">${escapeHTML(item?.asn || '')}</td>
                <td class="text-end">${Number(item?.prefixesV4 || 0)}</td>
                <td class="text-end">${Number(item?.prefixesV6 || 0)}</td>
                <td class="text-end fw-semibold">${Number(item?.entriesV4 || 0)}</td>
                <td class="text-end fw-semibold">${Number(item?.entriesV6 || 0)}</td>
                <td class="${error ? 'text-danger' : 'text-success'}">${escapeHTML(error || 'ok')}</td>
              </tr>
            `;
          })
          .join('');
        if (items.length === 0) {
          renderEmpty('No ASN preview results returned.');
        }
      }

      function renderLoading() {
        summaryElement.textContent = 'Loading…';
        tableBodyElement.innerHTML = '<tr><td colspan="6" class="text-body-secondary small">Resolving ASNs and computing collapsed ipset entries…</td></tr>';
      }

      function renderEmpty(message) {
        summaryElement.textContent = 'No preview data.';
        tableBodyElement.innerHTML = `<tr><td colspan="6" class="text-body-secondary small">${escapeHTML(message)}</td></tr>`;
      }

      function renderError(message) {
        showStatus(message, true);
        summaryElement.textContent = 'Preview failed.';
        tableBodyElement.innerHTML = '<tr><td colspan="6" class="text-body-secondary small">No preview data.</td></tr>';
      }

      function clearStatus() {
        statusElement.classList.add('d-none');
        statusElement.classList.remove('alert-danger', 'alert-success');
        statusElement.textContent = '';
      }

      function showStatus(message, isError) {
        statusElement.classList.remove('d-none', 'alert-danger', 'alert-success');
        statusElement.classList.add(isError ? 'alert-danger' : 'alert-success');
        statusElement.textContent = message;
      }

      return { open };
    },
  };

  function normalizeASNInputs(values) {
    const seen = new Set();
    const out = [];
    values.forEach((value) => {
      const trimmed = String(value || '').trim();
      if (!trimmed) {
        return;
      }
      const key = trimmed.toLowerCase();
      if (seen.has(key)) {
        return;
      }
      seen.add(key);
      out.push(trimmed);
    });
    return out;
  }

  function escapeHTML(value) {
    return String(value || '')
      .replaceAll('&', '&amp;')
      .replaceAll('<', '&lt;')
      .replaceAll('>', '&gt;')
      .replaceAll('"', '&quot;')
      .replaceAll("'", '&#39;');
  }
})();
