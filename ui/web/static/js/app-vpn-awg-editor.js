(() => {
  window.SplitVPNUI = window.SplitVPNUI || {};

  const params = [
    { key: 'Jc', type: 'number' },
    { key: 'Jmin', type: 'number' },
    { key: 'Jmax', type: 'number' },
    { key: 'S1', type: 'number' },
    { key: 'S2', type: 'number' },
    { key: 'S3', type: 'number' },
    { key: 'S4', type: 'number' },
    { key: 'H1', type: 'number' },
    { key: 'H2', type: 'number' },
    { key: 'H3', type: 'number' },
    { key: 'H4', type: 'number' },
    { key: 'Itime', type: 'number' },
    { key: 'I1', type: 'text' },
    { key: 'I2', type: 'text' },
    { key: 'I3', type: 'text' },
    { key: 'I4', type: 'text' },
    { key: 'I5', type: 'text' },
    { key: 'J1', type: 'text' },
    { key: 'J2', type: 'text' },
    { key: 'J3', type: 'text' },
  ];

  window.SplitVPNUI.createAmneziaWGEditor = function createAmneziaWGEditor(ctx) {
    const root = ctx?.root;
    const vpnTypeSelect = ctx?.vpnTypeSelect;
    const vpnConfigEditor = ctx?.vpnConfigEditor;
    if (!root || !vpnTypeSelect || !vpnConfigEditor) {
      return null;
    }

    root.innerHTML = renderPanel();

    const inputs = new Map();
    params.forEach(({ key }) => {
      const input = root.querySelector(`[data-awg-param="${key}"]`);
      if (input) {
        inputs.set(key, input);
      }
    });

    let syncing = false;

    inputs.forEach((input, key) => {
      input.addEventListener('input', () => {
        if (syncing) {
          return;
        }
        vpnConfigEditor.value = setInterfaceParam(vpnConfigEditor.value || '', key, input.value || '');
      });
    });

    function reset() {
      syncing = true;
      inputs.forEach((input) => {
        input.value = '';
      });
      syncing = false;
      syncVisibility();
    }

    function syncVisibility() {
      root.classList.toggle('d-none', vpnTypeSelect.value !== 'amneziawg');
    }

    function loadFromConfig() {
      syncing = true;
      const values = parseInterfaceParams(vpnConfigEditor.value || '');
      inputs.forEach((input, key) => {
        input.value = values.get(key.toLowerCase()) || '';
      });
      syncing = false;
      syncVisibility();
    }

    function applyToConfig() {
      if (vpnTypeSelect.value !== 'amneziawg') {
        return;
      }
      let raw = vpnConfigEditor.value || '';
      inputs.forEach((input, key) => {
        raw = setInterfaceParam(raw, key, input.value || '');
      });
      vpnConfigEditor.value = raw;
    }

    return { reset, syncVisibility, loadFromConfig, applyToConfig };
  };

  function parseInterfaceParams(raw) {
    const values = new Map();
    let inInterface = false;
    for (const line of String(raw || '').split(/\r?\n/)) {
      const section = line.match(/^\s*\[([^\]]+)\]\s*$/);
      if (section) {
        inInterface = section[1].trim().toLowerCase() === 'interface';
        continue;
      }
      if (!inInterface) {
        continue;
      }
      const match = line.match(/^\s*([A-Za-z][A-Za-z0-9]*)\s*=\s*(.*?)\s*$/);
      if (!match) {
        continue;
      }
      const key = match[1].toLowerCase();
      if (params.some((param) => param.key.toLowerCase() === key)) {
        values.set(key, match[2]);
      }
    }
    return values;
  }

  function setInterfaceParam(raw, key, value) {
    const lines = String(raw || '').split(/\r?\n/);
    const bounds = interfaceBounds(lines);
    if (!bounds) {
      return raw;
    }
    const trimmed = String(value || '').trim();
    const pattern = new RegExp(`^\\s*${escapeRegExp(key)}\\s*=`, 'i');
    for (let i = bounds.start + 1; i < bounds.end; i += 1) {
      if (!pattern.test(lines[i])) {
        continue;
      }
      if (trimmed) {
        lines[i] = `${key} = ${trimmed}`;
      } else {
        lines.splice(i, 1);
      }
      return joinConfigLines(lines);
    }
    if (trimmed) {
      lines.splice(bounds.end, 0, `${key} = ${trimmed}`);
    }
    return joinConfigLines(lines);
  }

  function interfaceBounds(lines) {
    let start = -1;
    let end = lines.length;
    for (let i = 0; i < lines.length; i += 1) {
      const section = lines[i].match(/^\s*\[([^\]]+)\]\s*$/);
      if (!section) {
        continue;
      }
      if (section[1].trim().toLowerCase() === 'interface') {
        start = i;
        continue;
      }
      if (start >= 0) {
        end = i;
        break;
      }
    }
    if (start < 0) {
      return null;
    }
    return { start, end };
  }

  function joinConfigLines(lines) {
    const joined = lines.join('\n');
    return joined.endsWith('\n') ? joined : `${joined}\n`;
  }

  function escapeRegExp(value) {
    return String(value).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  }

  function renderPanel() {
    const fields = params.map(renderField).join('');
    return `
      <div class="border rounded-2 p-3 mb-3">
        <div class="d-flex justify-content-between align-items-center mb-3">
          <div class="fw-semibold"><i class="bi bi-sliders me-2"></i>AmneziaWG Parameters</div>
          <div class="small text-body-secondary">S3/S4 require the kernel module.</div>
        </div>
        <div class="row g-2">${fields}</div>
      </div>`;
  }

  function renderField(param) {
    const id = `awg-param-${param.key.toLowerCase()}`;
    const width = param.type === 'number' ? 'col-6 col-md-2' : 'col-12 col-md-4';
    const mono = param.type === 'number' ? '' : ' font-monospace';
    const type = param.type === 'number' ? ' type="number" min="0"' : '';
    return `
      <div class="${width}">
        <label class="form-label small" for="${id}">${param.key}</label>
        <input class="form-control form-control-sm${mono}" id="${id}"${type} data-awg-param="${param.key}">
      </div>`;
  }
})();
