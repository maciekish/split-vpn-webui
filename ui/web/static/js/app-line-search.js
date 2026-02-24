(() => {
  window.SplitVPNUI = window.SplitVPNUI || {};

  window.SplitVPNUI.createLineSearchController = function createLineSearchController(ctx) {
    const {
      root,
      input,
      regexToggle,
      meta,
      scopeSelector = '[data-search-scope]',
      lineSelector = '[data-search-line="1"]',
    } = ctx || {};

    if (!root) {
      return {
        apply: () => {},
        refreshTargets: () => {},
        reset: () => {},
        setMeta: () => {},
      };
    }

    let lines = [];
    let scopes = [];

    if (input) {
      input.addEventListener('input', apply);
    }
    if (regexToggle) {
      regexToggle.addEventListener('change', apply);
    }

    function refreshTargets() {
      lines = Array.from(root.querySelectorAll(lineSelector));
      scopes = Array.from(root.querySelectorAll(scopeSelector));
      setMeta('');
    }

    function reset() {
      if (input) {
        input.value = '';
      }
      if (regexToggle) {
        regexToggle.checked = false;
      }
      restoreLines();
      setMeta('');
    }

    function apply() {
      if (!lines.length) {
        setMeta('');
        return;
      }

      const query = String(input?.value || '').trim();
      const regexMode = Boolean(regexToggle?.checked);
      if (!query) {
        restoreLines();
        setMeta('');
        return;
      }

      let matcher;
      try {
        matcher = buildMatcher(query, regexMode);
      } catch (err) {
        restoreLines();
        setMeta(`Invalid regex: ${err.message}`, true);
        return;
      }

      let matches = 0;
      lines.forEach((line) => {
        const text = String(line.dataset.searchRaw || '');
        const matched = matcher.test(text);
        line.classList.toggle('d-none', !matched);
        if (matched) {
          matches += 1;
          line.innerHTML = matcher.highlight(text);
        } else {
          line.textContent = text;
        }
      });

      filterScopes();
      if (matches === 0) {
        setMeta(`No matching lines for "${query}".`, false);
        return;
      }
      setMeta(`${matches} matching line${matches === 1 ? '' : 's'} of ${lines.length}.`, false);
    }

    function filterScopes() {
      const ordered = scopes
        .slice()
        .sort((left, right) => elementDepth(right) - elementDepth(left));
      ordered.forEach((scope) => {
        const hasVisibleLines = Boolean(scope.querySelector(`${lineSelector}:not(.d-none)`));
        scope.classList.toggle('d-none', !hasVisibleLines);
      });
    }

    function elementDepth(node) {
      let depth = 0;
      let current = node;
      while (current && current !== root) {
        depth += 1;
        current = current.parentElement;
      }
      return depth;
    }

    function restoreLines() {
      lines.forEach((line) => {
        const text = String(line.dataset.searchRaw || '');
        line.classList.remove('d-none');
        line.textContent = text;
      });
      scopes.forEach((scope) => {
        scope.classList.remove('d-none');
      });
    }

    function buildMatcher(query, regexMode) {
      if (!regexMode) {
        const needle = query.toLowerCase();
        return {
          test(text) {
            return String(text || '').toLowerCase().includes(needle);
          },
          highlight(text) {
            return highlightLiteral(String(text || ''), needle);
          },
        };
      }

      const regex = compileRegex(query);
      const testFlags = regex.flags.replace(/g/g, '');
      const testRegex = new RegExp(regex.source, testFlags);
      const highlightFlags = ensureRegexFlag(regex.flags, 'g');
      const highlightRegex = new RegExp(regex.source, highlightFlags);
      return {
        test(text) {
          return testRegex.test(String(text || ''));
        },
        highlight(text) {
          return highlightRegexMatches(String(text || ''), highlightRegex);
        },
      };
    }

    function compileRegex(query) {
      const raw = String(query || '').trim();
      let pattern = raw;
      let flags = 'i';

      if (raw.startsWith('/') && raw.lastIndexOf('/') > 0) {
        const tail = raw.lastIndexOf('/');
        pattern = raw.slice(1, tail);
        flags = raw.slice(tail + 1);
      }
      flags = dedupeRegexFlags(flags);
      return new RegExp(pattern, flags);
    }

    function dedupeRegexFlags(flags) {
      const unique = [];
      String(flags || '').split('').forEach((flag) => {
        if (!flag || unique.includes(flag)) {
          return;
        }
        unique.push(flag);
      });
      return unique.join('');
    }

    function ensureRegexFlag(flags, flag) {
      if (String(flags || '').includes(flag)) {
        return flags;
      }
      return `${flags || ''}${flag}`;
    }

    function highlightLiteral(text, needleLower) {
      if (!needleLower) {
        return escapeHTML(text);
      }
      const lower = text.toLowerCase();
      let cursor = 0;
      let html = '';
      while (cursor < text.length) {
        const index = lower.indexOf(needleLower, cursor);
        if (index < 0) {
          html += escapeHTML(text.slice(cursor));
          break;
        }
        html += escapeHTML(text.slice(cursor, index));
        html += `<mark>${escapeHTML(text.slice(index, index + needleLower.length))}</mark>`;
        cursor = index + needleLower.length;
      }
      return html;
    }

    function highlightRegexMatches(text, regex) {
      let html = '';
      let cursor = 0;
      let match = regex.exec(text);
      while (match) {
        const start = Number(match.index || 0);
        const value = String(match[0] || '');
        if (!value) {
          regex.lastIndex += 1;
          match = regex.exec(text);
          continue;
        }
        const end = start + value.length;
        html += escapeHTML(text.slice(cursor, start));
        html += `<mark>${escapeHTML(value)}</mark>`;
        cursor = end;
        match = regex.exec(text);
      }
      html += escapeHTML(text.slice(cursor));
      return html;
    }

    function setMeta(message, isError) {
      if (!meta) {
        return;
      }
      meta.classList.remove('d-none', 'text-danger', 'text-warning', 'text-body-secondary');
      if (!message) {
        meta.classList.add('d-none');
        meta.textContent = '';
        return;
      }
      meta.textContent = message;
      if (isError) {
        meta.classList.add('text-danger');
        return;
      }
      if (message.toLowerCase().startsWith('no matching lines')) {
        meta.classList.add('text-warning');
        return;
      }
      meta.classList.add('text-body-secondary');
    }

    function escapeHTML(value) {
      return String(value || '')
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
    }

    return {
      apply,
      refreshTargets,
      reset,
      setMeta,
    };
  };
})();
