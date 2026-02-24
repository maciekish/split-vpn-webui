(() => {
  function valueFrom(card, selector) {
    const input = card.querySelector(selector);
    return input ? String(input.value || '').trim() : '';
  }

  function rawValueFrom(card, selector) {
    const input = card.querySelector(selector);
    return input ? String(input.value || '') : '';
  }

  function splitRawLines(rawValue) {
    return String(rawValue || '')
      .replace(/\r/g, '')
      .split('\n');
  }

  function activeSelectorValue(line) {
    const trimmed = String(line || '').trim();
    if (!trimmed || trimmed.startsWith('#')) {
      return '';
    }
    const hashIndex = trimmed.indexOf('#');
    if (hashIndex < 0) {
      return trimmed;
    }
    return trimmed.slice(0, hashIndex).trim();
  }

  function parseSelectorField(rawValue) {
    const rawLines = splitRawLines(rawValue);
    const activeValues = rawLines
      .map((line) => activeSelectorValue(line))
      .filter((entry) => entry !== '');
    return { rawLines, activeValues };
  }

  function parseLines(rawValue) {
    return parseSelectorField(rawValue).activeValues;
  }

  function parsePorts(rawValue) {
    const parsed = [];
    parseSelectorField(rawValue).activeValues.forEach((entry) => {
      const compact = entry.replace(/\s+/g, '');
      const match = compact.match(/^(tcp|udp|both)[:/](\d{1,5})(?:-(\d{1,5}))?$/i);
      if (!match) {
        throw new Error(`Invalid port selector "${entry}". Use tcp:443, udp:5000-5100, or both:53.`);
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
      rule.sourceInterfaces.length > 0 ||
      rule.sourceCidrs.length > 0 ||
      rule.excludedSourceCidrs.length > 0 ||
      rule.sourceMacs.length > 0 ||
      rule.destinationCidrs.length > 0 ||
      rule.destinationPorts.length > 0 ||
      rule.excludedDestinationCidrs.length > 0 ||
      rule.excludedDestinationPorts.length > 0 ||
      rule.destinationAsns.length > 0 ||
      rule.excludedDestinationAsns.length > 0 ||
      rule.domains.length > 0 ||
      rule.wildcardDomains.length > 0
    );
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

  function escapeHTML(value) {
    return String(value || '')
      .replaceAll('&', '&amp;')
      .replaceAll('<', '&lt;')
      .replaceAll('>', '&gt;')
      .replaceAll('"', '&quot;')
      .replaceAll("'", '&#39;');
  }

  function fieldHasAnyLine(rawLines) {
    return Array.isArray(rawLines) && rawLines.some((line) => String(line || '').trim() !== '');
  }

  function ruleHasEditableContent(rule) {
    if (ruleHasSelectors(rule)) {
      return true;
    }
    const raw = rule.rawSelectors || {};
    return (
      fieldHasAnyLine(raw.sourceInterfaces) ||
      fieldHasAnyLine(raw.sourceCidrs) ||
      fieldHasAnyLine(raw.excludedSourceCidrs) ||
      fieldHasAnyLine(raw.sourceMacs) ||
      fieldHasAnyLine(raw.destinationCidrs) ||
      fieldHasAnyLine(raw.destinationPorts) ||
      fieldHasAnyLine(raw.excludedDestinationCidrs) ||
      fieldHasAnyLine(raw.excludedDestinationPorts) ||
      fieldHasAnyLine(raw.destinationAsns) ||
      fieldHasAnyLine(raw.excludedDestinationAsns) ||
      fieldHasAnyLine(raw.domains) ||
      fieldHasAnyLine(raw.wildcardDomains)
    );
  }

  window.SplitVPNDomainRoutingUtils = {
    valueFrom,
    rawValueFrom,
    splitRawLines,
    activeSelectorValue,
    parseSelectorField,
    parseLines,
    parsePorts,
    ruleHasSelectors,
    ruleHasEditableContent,
    formatPorts,
    escapeHTML,
  };
})();
