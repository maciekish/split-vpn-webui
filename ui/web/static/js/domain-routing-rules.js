(() => {
  window.SplitVPNDomainRoutingRules = {
    createController(ctx) {
      const {
        rulesList,
        state,
        helper,
        showStatus,
        sourceMACDeviceDatalistID,
        onPreviewASN,
      } = ctx || {};

      if (!rulesList || !state || !helper || typeof showStatus !== 'function' || !sourceMACDeviceDatalistID) {
        return null;
      }

      const valueFrom = helper.valueFrom;
      const rawValueFrom = helper.rawValueFrom;
      const splitRawLines = helper.splitRawLines;
      const parseSelectorField = helper.parseSelectorField;
      const parseLines = helper.parseLines;
      const parsePorts = helper.parsePorts;
      const ruleHasEditableContent = helper.ruleHasEditableContent;
      const formatPorts = helper.formatPorts;
      const escapeHTML = helper.escapeHTML;

      function parseRuleCards() {
        const cards = Array.from(rulesList.querySelectorAll('.routing-rule-card'));
        const rules = [];
        cards.forEach((card) => {
          const sourceInterfaces = parseSelectorField(rawValueFrom(card, '.js-rule-source-interface'));
          const sourceCidrs = parseSelectorField(rawValueFrom(card, '.js-rule-source'));
          const excludedSourceCidrs = parseSelectorField(rawValueFrom(card, '.js-rule-source-excluded'));
          const sourceMacs = parseSelectorField(rawValueFrom(card, '.js-rule-source-mac'));
          const destinationCidrs = parseSelectorField(rawValueFrom(card, '.js-rule-destination'));
          const excludedDestinationCidrs = parseSelectorField(rawValueFrom(card, '.js-rule-destination-excluded'));
          const destinationPortsRaw = parseSelectorField(rawValueFrom(card, '.js-rule-ports'));
          const excludedDestinationPortsRaw = parseSelectorField(rawValueFrom(card, '.js-rule-ports-excluded'));
          const destinationAsns = parseSelectorField(rawValueFrom(card, '.js-rule-asn'));
          const excludedDestinationAsns = parseSelectorField(rawValueFrom(card, '.js-rule-asn-excluded'));
          const domains = parseSelectorField(rawValueFrom(card, '.js-rule-domains'));
          const wildcardDomains = parseSelectorField(rawValueFrom(card, '.js-rule-wildcards'));
          const excludeMulticast = !!card.querySelector('.js-rule-exclude-multicast')?.checked;
          const rule = {
            name: valueFrom(card, '.js-rule-name'),
            sourceInterfaces: sourceInterfaces.activeValues,
            sourceCidrs: sourceCidrs.activeValues,
            excludedSourceCidrs: excludedSourceCidrs.activeValues,
            sourceMacs: sourceMacs.activeValues,
            destinationCidrs: destinationCidrs.activeValues,
            excludedDestinationCidrs: excludedDestinationCidrs.activeValues,
            destinationPorts: parsePorts(destinationPortsRaw.activeValues.join('\n')),
            excludedDestinationPorts: parsePorts(excludedDestinationPortsRaw.activeValues.join('\n')),
            destinationAsns: destinationAsns.activeValues,
            excludedDestinationAsns: excludedDestinationAsns.activeValues,
            excludeMulticast,
            domains: domains.activeValues,
            wildcardDomains: wildcardDomains.activeValues,
            rawSelectors: {
              sourceInterfaces: sourceInterfaces.rawLines,
              sourceCidrs: sourceCidrs.rawLines,
              excludedSourceCidrs: excludedSourceCidrs.rawLines,
              sourceMacs: sourceMacs.rawLines,
              destinationCidrs: destinationCidrs.rawLines,
              excludedDestinationCidrs: excludedDestinationCidrs.rawLines,
              destinationPorts: destinationPortsRaw.rawLines,
              excludedDestinationPorts: excludedDestinationPortsRaw.rawLines,
              destinationAsns: destinationAsns.rawLines,
              excludedDestinationAsns: excludedDestinationAsns.rawLines,
              domains: domains.rawLines,
              wildcardDomains: wildcardDomains.rawLines,
            },
          };
          if (ruleHasEditableContent(rule)) {
            rules.push(rule);
          }
        });
        return rules;
      }

      function normalizeRules(group) {
        if (Array.isArray(group?.rules) && group.rules.length > 0) {
          return group.rules.map((rule, index) => {
            const raw = rule.rawSelectors || {};
            const sourceInterfaces = Array.isArray(rule.sourceInterfaces) ? rule.sourceInterfaces : [];
            const sourceCidrs = Array.isArray(rule.sourceCidrs) ? rule.sourceCidrs : [];
            const excludedSourceCidrs = Array.isArray(rule.excludedSourceCidrs) ? rule.excludedSourceCidrs : [];
            const sourceMacs = Array.isArray(rule.sourceMacs) ? rule.sourceMacs : [];
            const destinationCidrs = Array.isArray(rule.destinationCidrs) ? rule.destinationCidrs : [];
            const excludedDestinationCidrs = Array.isArray(rule.excludedDestinationCidrs) ? rule.excludedDestinationCidrs : [];
            const destinationPorts = Array.isArray(rule.destinationPorts) ? rule.destinationPorts : [];
            const excludedDestinationPorts = Array.isArray(rule.excludedDestinationPorts) ? rule.excludedDestinationPorts : [];
            const destinationAsns = Array.isArray(rule.destinationAsns) ? rule.destinationAsns : [];
            const excludedDestinationAsns = Array.isArray(rule.excludedDestinationAsns) ? rule.excludedDestinationAsns : [];
            const excludeMulticast = typeof rule.excludeMulticast === 'boolean' ? rule.excludeMulticast : true;
            const domains = Array.isArray(rule.domains) ? rule.domains : [];
            const wildcardDomains = Array.isArray(rule.wildcardDomains) ? rule.wildcardDomains : [];
            return {
              name: rule.name || `Rule ${index + 1}`,
              sourceInterfaces,
              sourceCidrs,
              excludedSourceCidrs,
              sourceMacs,
              destinationCidrs,
              excludedDestinationCidrs,
              destinationPorts,
              excludedDestinationPorts,
              destinationAsns,
              excludedDestinationAsns,
              excludeMulticast,
              domains,
              wildcardDomains,
              rawSelectors: {
                sourceInterfaces: normalizeRawLinesOrFallback(raw.sourceInterfaces, sourceInterfaces),
                sourceCidrs: normalizeRawLinesOrFallback(raw.sourceCidrs, sourceCidrs),
                excludedSourceCidrs: normalizeRawLinesOrFallback(raw.excludedSourceCidrs, excludedSourceCidrs),
                sourceMacs: normalizeRawLinesOrFallback(raw.sourceMacs, sourceMacs),
                destinationCidrs: normalizeRawLinesOrFallback(raw.destinationCidrs, destinationCidrs),
                excludedDestinationCidrs: normalizeRawLinesOrFallback(raw.excludedDestinationCidrs, excludedDestinationCidrs),
                destinationPorts: normalizeRawLinesOrFallback(
                  raw.destinationPorts,
                  formattedPortLines(destinationPorts),
                ),
                excludedDestinationPorts: normalizeRawLinesOrFallback(
                  raw.excludedDestinationPorts,
                  formattedPortLines(excludedDestinationPorts),
                ),
                destinationAsns: normalizeRawLinesOrFallback(raw.destinationAsns, destinationAsns),
                excludedDestinationAsns: normalizeRawLinesOrFallback(raw.excludedDestinationAsns, excludedDestinationAsns),
                domains: normalizeRawLinesOrFallback(raw.domains, domains),
                wildcardDomains: normalizeRawLinesOrFallback(raw.wildcardDomains, wildcardDomains),
              },
            };
          });
        }
        const legacyDomains = Array.isArray(group?.domains) ? group.domains : [];
        if (legacyDomains.length === 0) {
          return [];
        }
        return [{
          name: 'Rule 1',
          sourceInterfaces: [],
          sourceCidrs: [],
          excludedSourceCidrs: [],
          sourceMacs: [],
          destinationCidrs: [],
          excludedDestinationCidrs: [],
          destinationPorts: [],
          excludedDestinationPorts: [],
          destinationAsns: [],
          excludedDestinationAsns: [],
          excludeMulticast: true,
          domains: legacyDomains.filter((entry) => !String(entry).startsWith('*.' )),
          wildcardDomains: legacyDomains.filter((entry) => String(entry).startsWith('*.')),
          rawSelectors: {
            sourceInterfaces: [],
            sourceCidrs: [],
            excludedSourceCidrs: [],
            sourceMacs: [],
            destinationCidrs: [],
            excludedDestinationCidrs: [],
            destinationPorts: [],
            excludedDestinationPorts: [],
            destinationAsns: [],
            excludedDestinationAsns: [],
            domains: legacyDomains.filter((entry) => !String(entry).startsWith('*.' )),
            wildcardDomains: legacyDomains.filter((entry) => String(entry).startsWith('*.')),
          },
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
          sourceInterfaces: [],
          sourceCidrs: [],
          excludedSourceCidrs: [],
          sourceMacs: [],
          destinationCidrs: [],
          excludedDestinationCidrs: [],
          destinationPorts: [],
          excludedDestinationPorts: [],
          destinationAsns: [],
          excludedDestinationAsns: [],
          excludeMulticast: true,
          domains: [],
          wildcardDomains: [],
          rawSelectors: {
            sourceInterfaces: [],
            sourceCidrs: [],
            excludedSourceCidrs: [],
            sourceMacs: [],
            destinationCidrs: [],
            excludedDestinationCidrs: [],
            destinationPorts: [],
            excludedDestinationPorts: [],
            destinationAsns: [],
            excludedDestinationAsns: [],
            domains: [],
            wildcardDomains: [],
          },
        };
        const raw = payload.rawSelectors || {};
        const sourceInterfacesText = selectorText(raw.sourceInterfaces, payload.sourceInterfaces || []);
        const sourceCidrsText = selectorText(raw.sourceCidrs, payload.sourceCidrs || []);
        const excludedSourceCidrsText = selectorText(raw.excludedSourceCidrs, payload.excludedSourceCidrs || []);
        const sourceMacsText = selectorText(raw.sourceMacs, payload.sourceMacs || []);
        const destinationCidrsText = selectorText(raw.destinationCidrs, payload.destinationCidrs || []);
        const excludedDestinationCidrsText = selectorText(raw.excludedDestinationCidrs, payload.excludedDestinationCidrs || []);
        const destinationPortsText = selectorText(raw.destinationPorts, formattedPortLines(payload.destinationPorts || []));
        const excludedDestinationPortsText = selectorText(raw.excludedDestinationPorts, formattedPortLines(payload.excludedDestinationPorts || []));
        const destinationAsnsText = selectorText(raw.destinationAsns, payload.destinationAsns || []);
        const excludedDestinationAsnsText = selectorText(raw.excludedDestinationAsns, payload.excludedDestinationAsns || []);
        const domainsText = selectorText(raw.domains, payload.domains || []);
        const wildcardDomainsText = selectorText(raw.wildcardDomains, payload.wildcardDomains || []);
        const excludeMulticast = typeof payload.excludeMulticast === 'boolean' ? payload.excludeMulticast : true;
        const pickerInputID = `source-mac-picker-${ruleID}`;
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
        <div class="col-12 col-md-4">
          <label class="form-label small text-body-secondary mb-1">Source Interfaces</label>
          <textarea class="form-control form-control-sm font-monospace js-rule-source-interface" rows="4" placeholder="br0&#10;br6&#10;#Guest VLAN only">${escapeHTML(sourceInterfacesText)}</textarea>
        </div>
        <div class="col-12 col-md-4">
          <label class="form-label small text-body-secondary mb-1">Source CIDRs</label>
          <textarea class="form-control form-control-sm font-monospace js-rule-source" rows="4" placeholder="10.0.0.0/24&#10;2001:db8::/64&#10;#Temporary block">${escapeHTML(sourceCidrsText)}</textarea>
        </div>
        <div class="col-12 col-md-4">
          <label class="form-label small text-body-secondary mb-1">Source MACs</label>
          <div class="input-group input-group-sm mb-2">
            <input class="form-control js-source-mac-picker" type="text" list="${sourceMACDeviceDatalistID}" id="${pickerInputID}" placeholder="Search known devices">
            <button class="btn btn-outline-primary" type="button" data-action="source-mac-add" title="Add selected source MAC">
              <i class="bi bi-plus-lg"></i>
            </button>
          </div>
          <textarea class="form-control form-control-sm font-monospace js-rule-source-mac" rows="4" placeholder="00:30:93:10:0a:12#Apple TV&#10;#00:11:22:33:44:55">${escapeHTML(sourceMacsText)}</textarea>
        </div>
        <div class="col-12">
          <label class="form-label small text-body-secondary mb-1">Excluded Source CIDRs</label>
          <textarea class="form-control form-control-sm font-monospace js-rule-source-excluded" rows="3" placeholder="10.0.0.50/32&#10;2001:db8::50/128&#10;#Bypass this source">${escapeHTML(excludedSourceCidrsText)}</textarea>
        </div>
        <div class="col-12 col-md-6">
          <label class="form-label small text-body-secondary mb-1">Destination CIDRs</label>
          <textarea class="form-control form-control-sm font-monospace js-rule-destination" rows="4" placeholder="1.1.1.0/24&#10;2606:4700::/32&#10;#Bypass test prefix">${escapeHTML(destinationCidrsText)}</textarea>
        </div>
        <div class="col-12 col-md-6">
          <label class="form-label small text-body-secondary mb-1">Destination Ports</label>
          <textarea class="form-control form-control-sm font-monospace js-rule-ports" rows="4" placeholder="tcp:443&#10;both:53&#10;udp:5000-5100&#10;#tcp:22">${escapeHTML(destinationPortsText)}</textarea>
        </div>
        <div class="col-12 col-md-6">
          <label class="form-label small text-body-secondary mb-1">Excluded Destination CIDRs</label>
          <textarea class="form-control form-control-sm font-monospace js-rule-destination-excluded" rows="4" placeholder="17.0.0.0/8&#10;2a01:b740::/29&#10;#Bypass destination">${escapeHTML(excludedDestinationCidrsText)}</textarea>
        </div>
        <div class="col-12 col-md-6">
          <label class="form-label small text-body-secondary mb-1">Excluded Destination Ports</label>
          <textarea class="form-control form-control-sm font-monospace js-rule-ports-excluded" rows="4" placeholder="udp:5353&#10;tcp:1900&#10;#both:53">${escapeHTML(excludedDestinationPortsText)}</textarea>
        </div>
        <div class="col-12 col-md-4">
          <div class="d-flex justify-content-between align-items-center mb-1">
            <label class="form-label small text-body-secondary mb-0">Destination ASNs</label>
            <button class="btn btn-outline-info btn-sm py-0 px-2" type="button" data-action="preview-asn" data-asn-selector=".js-rule-asn" data-asn-title="Destination ASN ipset Entry Preview">Preview</button>
          </div>
          <textarea class="form-control form-control-sm font-monospace js-rule-asn" rows="4" placeholder="AS15169&#10;13335&#10;#AS32934">${escapeHTML(destinationAsnsText)}</textarea>
        </div>
        <div class="col-12 col-md-4">
          <div class="d-flex justify-content-between align-items-center mb-1">
            <label class="form-label small text-body-secondary mb-0">Excluded Destination ASNs</label>
            <button class="btn btn-outline-info btn-sm py-0 px-2" type="button" data-action="preview-asn" data-asn-selector=".js-rule-asn-excluded" data-asn-title="Excluded ASN ipset Entry Preview">Preview</button>
          </div>
          <textarea class="form-control form-control-sm font-monospace js-rule-asn-excluded" rows="4" placeholder="AS714&#10;#AS32934">${escapeHTML(excludedDestinationAsnsText)}</textarea>
        </div>
        <div class="col-12 col-md-4 d-flex align-items-end">
          <div class="form-check form-switch mt-2">
            <input class="form-check-input js-rule-exclude-multicast" type="checkbox" role="switch"${excludeMulticast ? ' checked' : ''}>
            <label class="form-check-label small text-body-secondary">Exclude multicast destinations (recommended)</label>
          </div>
        </div>
        <div class="col-12 col-md-4">
          <label class="form-label small text-body-secondary mb-1">Domains</label>
          <textarea class="form-control form-control-sm font-monospace js-rule-domains" rows="4" placeholder="api.example.com&#10;www.apple.com#All apple website traffic">${escapeHTML(domainsText)}</textarea>
        </div>
        <div class="col-12">
          <label class="form-label small text-body-secondary mb-1">Wildcard Domains</label>
          <textarea class="form-control form-control-sm font-monospace js-rule-wildcards" rows="3" placeholder="*.apple.com&#10;#*.example.net">${escapeHTML(wildcardDomainsText)}</textarea>
        </div>
        <div class="col-12">
          <div class="small text-body-secondary">
            Comments are supported in all selector boxes. Anything after <code>#</code> on a line is ignored for matching but saved as entered.
          </div>
          <div class="small text-body-secondary">
            Normal Domains match both the exact domain and its subdomains in dnsmasq, but pre-warm only queries domains explicitly listed here.
          </div>
          <div class="small text-danger mt-1">
            Wildcard Domains discover known subdomains from public data and pre-warm those discovered hosts. Use large top domains (for example <code>*.microsoft.com</code> / <code>microsoft.com</code>) with great care: they can expand into huge domain lists and create massive IPv4/IPv6 ipsets.
          </div>
        </div>
      </div>`;
        rulesList.appendChild(card);
        attachSourceMACPicker(card);
        refreshSourceMACDeviceDatalist();
      }

      function selectorText(rawLines, fallbackValues) {
        const values = normalizeRawLinesOrFallback(rawLines, fallbackValues);
        return values.join('\n');
      }

      function normalizeRawLinesOrFallback(rawLines, fallbackValues) {
        if (Array.isArray(rawLines) && rawLines.length > 0) {
          return rawLines.map((line) => String(line || '').replace(/\r/g, ''));
        }
        if (!Array.isArray(fallbackValues) || fallbackValues.length === 0) {
          return [];
        }
        return fallbackValues.map((value) => String(value || '').replace(/\r/g, ''));
      }
      function formattedPortLines(ports) {
        const text = formatPorts(Array.isArray(ports) ? ports : []);
        if (!text) {
          return [];
        }
        return splitRawLines(text);
      }

      function refreshSourceMACDeviceDatalist() {
        let datalist = document.getElementById(sourceMACDeviceDatalistID);
        if (!datalist) {
          datalist = document.createElement('datalist');
          datalist.id = sourceMACDeviceDatalistID;
          document.body.appendChild(datalist);
        }
        datalist.innerHTML = '';
        state.devices.forEach((device) => {
          const option = document.createElement('option');
          option.value = buildSourceMACComment(device.mac, device.name);
          const hints = Array.isArray(device.ipHints) ? device.ipHints.join(', ') : '';
          const labelParts = [];
          if (device.name) {
            labelParts.push(device.name);
          }
          if (hints) {
            labelParts.push(hints);
          }
          option.label = labelParts.join(' • ');
          option.setAttribute('data-search', device.searchText || '');
          datalist.appendChild(option);
        });
      }

      function attachSourceMACPicker(card) {
        const picker = card.querySelector('.js-source-mac-picker');
        if (!picker) {
          return;
        }
        picker.setAttribute('list', sourceMACDeviceDatalistID);
        picker.addEventListener('keydown', (event) => {
          if (event.key !== 'Enter') {
            return;
          }
          event.preventDefault();
          addSourceMACFromPicker(card);
        });
      }

      function addSourceMACFromPicker(card) {
        const picker = card.querySelector('.js-source-mac-picker');
        const textarea = card.querySelector('.js-rule-source-mac');
        if (!picker || !textarea) {
          return;
        }
        const raw = String(picker.value || '').trim();
        if (!raw) {
          return;
        }
        const candidate = normalizeSourceMACLine(raw);
        if (!candidate) {
          showStatus('Invalid source MAC picker value.', true);
          return;
        }
        const existing = splitRawLines(textarea.value || '');
        const alreadyExists = existing.some((line) => normalizeSourceMACLine(line) === candidate);
        if (!alreadyExists) {
          if (textarea.value && !textarea.value.endsWith('\n')) {
            textarea.value += '\n';
          }
          textarea.value += candidate;
        }
        picker.value = '';
        textarea.focus();
      }

      function normalizeSourceMACLine(line) {
        const raw = String(line || '').trim();
        if (!raw) {
          return '';
        }
        const macOnly = parseLines(raw).find((entry) => entry !== '');
        if (!macOnly) {
          return '';
        }
        const matchingDevice = state.devices.find((device) => device.mac === macOnly.toLowerCase());
        if (matchingDevice) {
          return buildSourceMACComment(matchingDevice.mac, matchingDevice.name);
        }
        const marker = raw.indexOf('#');
        if (marker >= 0) {
          return `${macOnly}#${raw.slice(marker + 1).trim()}`;
        }
        return macOnly;
      }

      function buildSourceMACComment(mac, name) {
        const normalizedMAC = String(mac || '').trim().toLowerCase();
        const normalizedName = String(name || '').trim();
        if (!normalizedMAC) {
          return '';
        }
        if (!normalizedName) {
          return normalizedMAC;
        }
        return `${normalizedMAC}#${normalizedName}`;
      }

      function handleAction(action, card, actionTarget) {
        if (!card) {
          return false;
        }
        if (action === 'remove-rule') {
          card.remove();
          if (rulesList.children.length === 0) {
            appendRuleCard();
          }
          return true;
        }
        if (action === 'source-mac-add') {
          addSourceMACFromPicker(card);
          return true;
        }
        if (action === 'preview-asn') {
          if (typeof onPreviewASN !== 'function') {
            showStatus('ASN preview is unavailable.', true);
            return true;
          }
          const selector = String(actionTarget?.getAttribute('data-asn-selector') || '.js-rule-asn');
          const title = String(actionTarget?.getAttribute('data-asn-title') || 'ASN ipset Entry Preview');
          const textArea = card.querySelector(selector);
          const asns = parseSelectorField(textArea ? textArea.value : '').activeValues;
          onPreviewASN({ title, asns });
          return true;
        }
        return false;
      }

      return {
        parseRuleCards,
        normalizeRules,
        resetRules,
        appendRuleCard,
        refreshSourceMACDeviceDatalist,
        handleAction,
      };
    },
  };
})();
