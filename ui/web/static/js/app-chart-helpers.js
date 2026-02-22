(() => {
  window.SplitVPNUI = window.SplitVPNUI || {};

  window.SplitVPNUI.createChartHelpers = function createChartHelpers(ctx) {
    const palette = Array.isArray(ctx?.palette) && ctx.palette.length > 0
      ? ctx.palette
      : ['#3b82f6', '#22d3ee', '#f97316', '#a855f7', '#f43f5e', '#14b8a6', '#eab308'];
    const downloadColor = ctx?.downloadColor || '#60a5fa';
    const downloadFill = ctx?.downloadFill || 'rgba(96, 165, 250, 0.15)';
    const uploadColor = ctx?.uploadColor || '#f87171';
    const uploadFill = ctx?.uploadFill || 'rgba(248, 113, 113, 0.15)';

    function createGauge(canvasId, legendId, formatter) {
      const canvas = document.getElementById(canvasId);
      if (!canvas) {
        return null;
      }
      const legend = legendId ? document.getElementById(legendId) : null;
      const chart = new Chart(canvas.getContext('2d'), {
        type: 'doughnut',
        data: {
          labels: [],
          datasets: [{
            label: '',
            data: [],
            backgroundColor: palette,
            borderWidth: 0,
          }],
        },
        options: {
          responsive: true,
          maintainAspectRatio: true,
          aspectRatio: 1,
          cutout: '70%',
          circumference: 180,
          rotation: -90,
          plugins: {
            legend: { display: false },
            tooltip: {
              callbacks: {
                label: (entry) => {
                  const value = entry.raw || 0;
                  return `${entry.label}: ${formatter(value)}`;
                },
              },
            },
          },
        },
      });
      chart.$formatter = formatter;
      chart.$legend = legend;
      return chart;
    }

    function updateGaugeChart(chart, labels, data) {
      if (!chart) {
        return;
      }
      chart.data.labels = labels;
      chart.data.datasets[0].data = data;
      chart.data.datasets[0].backgroundColor = resolveGaugeColors(labels);
      chart.options.plugins.tooltip.callbacks.label = (entry) => {
        const value = entry.raw || 0;
        const formatter = chart.$formatter || ((val) => val.toString());
        return `${entry.label}: ${formatter(value)}`;
      };
      chart.update('none');
      updateGaugeLegend(chart, labels, data);
    }

    function updateGaugeLegend(chart, labels, data) {
      if (!chart?.$legend) {
        return;
      }
      const legend = chart.$legend;
      legend.innerHTML = '';
      const colors = chart.data.datasets[0].backgroundColor || [];
      labels.forEach((label, index) => {
        const row = document.createElement('div');
        row.className = 'gauge-legend-row';

        const labelWrap = document.createElement('div');
        labelWrap.className = 'gauge-legend-label';

        const swatch = document.createElement('span');
        swatch.className = 'gauge-legend-swatch';
        swatch.style.backgroundColor = colors[index] || palette[index % palette.length];
        labelWrap.appendChild(swatch);

        const text = document.createElement('span');
        text.className = 'gauge-legend-text';
        text.textContent = label || '';
        if (label) {
          text.title = label;
        }
        labelWrap.appendChild(text);

        const value = document.createElement('div');
        value.className = 'gauge-legend-value';
        const formatter = chart.$formatter || ((val) => val.toString());
        value.textContent = formatter(data[index] || 0);

        row.appendChild(labelWrap);
        row.appendChild(value);
        legend.appendChild(row);
      });
    }

    function resolveGaugeColors(labels) {
      if (!(ctx?.state?.gaugeColors instanceof Map)) {
        return labels.map((_, index) => palette[index % palette.length]);
      }
      const used = new Set(ctx.state.gaugeColors.values());
      const seen = new Set();
      const colors = labels.map((label) => {
        const key = label || '';
        seen.add(key);
        if (!ctx.state.gaugeColors.has(key)) {
          let assigned = null;
          for (const candidate of palette) {
            if (!used.has(candidate)) {
              assigned = candidate;
              break;
            }
          }
          if (!assigned) {
            assigned = palette[ctx.state.gaugeColors.size % palette.length];
          }
          ctx.state.gaugeColors.set(key, assigned);
          used.add(assigned);
        }
        return ctx.state.gaugeColors.get(key);
      });
      for (const key of Array.from(ctx.state.gaugeColors.keys())) {
        if (!seen.has(key)) {
          ctx.state.gaugeColors.delete(key);
        }
      }
      return colors;
    }

    function updateInterfaceCards(interfaces, configs, latency) {
      if (!(ctx?.state?.interfaceCharts instanceof Map) || !ctx.interfaceGrid) {
        return;
      }
      const existing = new Set(ctx.state.interfaceCharts.keys());
      const configMap = new Map(configs.map((cfg) => [cfg.interfaceName, cfg]));
      const latencyMap = new Map(latency.map((item) => [item.name, item]));

      interfaces.forEach((iface, index) => {
        const key = iface.name;
        existing.delete(key);
        let record = ctx.state.interfaceCharts.get(key);
        const cfg = configMap.get(iface.interface);
        const latencyInfo = cfg ? latencyMap.get(cfg.name) : undefined;
        if (!record) {
          record = createInterfaceCard(iface, index);
          ctx.interfaceGrid.appendChild(record.container);
          ctx.state.interfaceCharts.set(key, record);
        }
        updateInterfaceCard(iface, cfg, latencyInfo, index);
      });

      existing.forEach((name) => {
        const record = ctx.state.interfaceCharts.get(name);
        if (!record) {
          return;
        }
        record.container.remove();
        record.chart.destroy();
        ctx.state.interfaceCharts.delete(name);
      });
    }

    function createInterfaceCard(iface, index) {
      const col = document.createElement('div');
      col.className = 'col-12 col-lg-6';
      col.dataset.interface = iface.name;
      col.style.order = index;

      const card = document.createElement('div');
      card.className = 'card interface-card h-100 shadow-sm';

      const header = document.createElement('div');
      header.className = 'card-header d-flex justify-content-between align-items-center';
      header.innerHTML = `
        <div>
          <span class="fw-semibold" data-field="name">${iface.name}</span>
          <div class="small text-body-secondary" data-field="iface">${iface.interface || ''}</div>
        </div>`;

      const badge = document.createElement('span');
      badge.className = 'badge rounded-pill text-bg-primary badge-operstate';
      badge.textContent = iface.type === 'wan' ? 'WAN' : 'VPN';
      header.appendChild(badge);
      card.appendChild(header);

      const body = document.createElement('div');
      body.className = 'card-body d-flex flex-column gap-3';

      const statsRow = document.createElement('div');
      statsRow.className = 'stats-row';
      statsRow.innerHTML = `
        <div>
          <div class="text-body-secondary small">Throughput</div>
          <div class="fw-semibold" data-field="throughput">–</div>
        </div>
        <div>
          <div class="text-body-secondary small">Received</div>
          <div class="fw-semibold" data-field="rx">–</div>
        </div>
        <div>
          <div class="text-body-secondary small">Sent</div>
          <div class="fw-semibold" data-field="tx">–</div>
        </div>
        <div>
          <div class="text-body-secondary small">Total</div>
          <div class="fw-semibold" data-field="total">–</div>
        </div>`;
      body.appendChild(statsRow);

      const statusLine = document.createElement('div');
      statusLine.className = 'text-body-secondary small';
      statusLine.dataset.field = 'status';
      body.appendChild(statusLine);

      const chartWrapper = document.createElement('div');
      chartWrapper.className = 'chart-wrapper';
      const canvas = document.createElement('canvas');
      chartWrapper.appendChild(canvas);
      body.appendChild(chartWrapper);

      card.appendChild(body);
      col.appendChild(card);

      const chart = new Chart(canvas.getContext('2d'), {
        type: 'line',
        data: {
          labels: [],
          datasets: [
            {
              label: 'Download',
              data: [],
              fill: true,
              borderColor: downloadColor,
              backgroundColor: downloadFill,
              tension: 0.3,
              pointRadius: 0,
            },
            {
              label: 'Upload',
              data: [],
              fill: true,
              borderColor: uploadColor,
              backgroundColor: uploadFill,
              tension: 0.3,
              pointRadius: 0,
            },
          ],
        },
        options: {
          animation: false,
          maintainAspectRatio: false,
          scales: {
            x: {
              ticks: { color: '#9ca3af', maxRotation: 0 },
              grid: { color: 'rgba(148, 163, 184, 0.1)' },
            },
            y: {
              ticks: {
                color: '#9ca3af',
                callback: (value) => formatThroughput(value),
              },
              grid: { color: 'rgba(148, 163, 184, 0.1)' },
              suggestedMax: 100000,
            },
          },
          plugins: {
            legend: { display: false },
            tooltip: {
              callbacks: {
                label: (entry) => `${entry.dataset.label}: ${formatThroughput(entry.parsed.y)}`,
              },
            },
          },
        },
      });

      return {
        container: col,
        chart,
        body,
        badge,
        statusLine,
        nameEl: header.querySelector('[data-field="name"]'),
        ifaceEl: header.querySelector('[data-field="iface"]'),
      };
    }

    function updateInterfaceCard(iface, cfg, latencyInfo, index) {
      const record = ctx?.state?.interfaceCharts?.get(iface.name);
      if (!record) {
        return;
      }
      record.container.style.order = index;
      record.container.classList.toggle('wan-card', iface.type === 'wan');
      record.container.classList.toggle('vpn-card', iface.type === 'vpn');
      if (record.nameEl) {
        record.nameEl.textContent = iface.name;
      }
      if (record.ifaceEl) {
        record.ifaceEl.textContent = iface.interface || '';
      }
      if (record.badge) {
        record.badge.textContent = iface.type === 'wan' ? 'WAN' : 'VPN';
      }

      const statsRow = record.body.querySelector('.stats-row');
      const downloadLabel = formatThroughput(iface.currentRxThroughput || 0);
      const uploadLabel = formatThroughput(iface.currentTxThroughput || 0);
      statsRow.querySelector('[data-field="throughput"]').innerHTML = `<span class="text-primary">↓ ${downloadLabel}</span><br><span class="text-danger">↑ ${uploadLabel}</span>`;
      statsRow.querySelector('[data-field="rx"]').textContent = formatBytes(iface.rxBytes);
      statsRow.querySelector('[data-field="tx"]').textContent = formatBytes(iface.txBytes);
      statsRow.querySelector('[data-field="total"]').textContent = formatBytes(iface.totalBytes);

      if (record.statusLine) {
        const status = deriveInterfaceStatus(iface, cfg, latencyInfo);
        record.statusLine.textContent = status.text;
        applyStatusTone(record.statusLine, status.level);
        if (latencyInfo && latencyInfo.target) {
          record.statusLine.title = `Gateway: ${latencyInfo.target}`;
        } else {
          record.statusLine.removeAttribute('title');
        }
      }

      const history = Array.isArray(iface.history) ? iface.history : [];
      const labels = buildTimeLabels(history);
      const downloads = history.map((point) => point.rxThroughput || 0);
      const uploads = history.map((point) => point.txThroughput || 0);
      record.chart.data.labels = labels;
      record.chart.data.datasets[0].data = downloads;
      record.chart.data.datasets[1].data = uploads;
      const peakDownload = downloads.length ? Math.max(...downloads) : 0;
      const peakUpload = uploads.length ? Math.max(...uploads) : 0;
      const peakValue = Math.max(peakDownload, peakUpload);
      record.chart.options.scales.y.suggestedMax = Math.max(100000, peakValue > 0 ? peakValue * 1.2 : 0);
      record.chart.update('none');
    }

    function deriveInterfaceStatus(iface, cfg, latencyInfo) {
      if (!iface) {
        return { text: '', level: 'muted' };
      }
      const displayName = resolveInterfaceDisplayName(iface, cfg);
      if (!iface.available) {
        return { text: `${displayName} • Interface unavailable`, level: 'warning' };
      }
      const operState = String(iface.operState || cfg?.operState || '').toLowerCase();
      if (latencyInfo && latencyInfo.success) {
        return { text: `${displayName} • ${formatLatency(latencyInfo.latencyMs)}`, level: 'success' };
      }
      if (latencyInfo && latencyInfo.error) {
        const tone = operState === 'down' ? 'danger' : 'warning';
        return { text: `${displayName} • ${latencyInfo.error}`, level: tone };
      }
      if (latencyInfo && !latencyInfo.success) {
        return { text: `${displayName} • No response`, level: 'warning' };
      }
      if (operState === 'down') {
        return { text: `${displayName} • Down`, level: 'danger' };
      }
      if (operState === 'up') {
        return { text: `${displayName} • Up`, level: 'success' };
      }
      if (operState === '') {
        return { text: `${displayName} • Unknown`, level: 'muted' };
      }
      return { text: `${displayName} • ${capitalizeWord(operState)}`, level: 'muted' };
    }

    function resolveInterfaceDisplayName(iface, cfg) {
      if (!iface) {
        return '';
      }
      if (iface.type === 'wan') {
        return 'WAN';
      }
      if (cfg && cfg.name) {
        return cfg.name;
      }
      if (iface.name) {
        return iface.name;
      }
      return iface.interface || 'Interface';
    }

    function applyStatusTone(element, tone) {
      if (!element) {
        return;
      }
      const tones = ['text-success', 'text-warning', 'text-danger', 'text-body-secondary'];
      tones.forEach((className) => element.classList.remove(className));
      switch (tone) {
        case 'success':
          element.classList.add('text-success');
          break;
        case 'warning':
          element.classList.add('text-warning');
          break;
        case 'danger':
          element.classList.add('text-danger');
          break;
        default:
          element.classList.add('text-body-secondary');
          break;
      }
    }

    function formatThroughput(value) {
      const units = ['bps', 'Kbps', 'Mbps', 'Gbps', 'Tbps'];
      let val = value;
      let index = 0;
      while (val >= 1000 && index < units.length - 1) {
        val /= 1000;
        index++;
      }
      return `${val.toFixed(val >= 100 ? 0 : val >= 10 ? 1 : 2)} ${units[index]}`;
    }

    function capitalizeWord(value) {
      if (!value) {
        return '';
      }
      return value.charAt(0).toUpperCase() + value.slice(1);
    }

    function formatBytes(value) {
      const units = ['B', 'KB', 'MB', 'GB', 'TB'];
      let val = value;
      let index = 0;
      while (val >= 1024 && index < units.length - 1) {
        val /= 1024;
        index++;
      }
      return `${val.toFixed(val >= 100 ? 0 : val >= 10 ? 1 : 2)} ${units[index]}`;
    }

    function formatLatency(value) {
      if (!value && value !== 0) {
        return '–';
      }
      if (value >= 1000) {
        return `${(value / 1000).toFixed(2)} s`;
      }
      return `${value.toFixed(0)} ms`;
    }

    function formatTime(timestamp) {
      const date = new Date(timestamp);
      if (Number.isNaN(date.getTime())) {
        return '';
      }
      return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    }

    function buildTimeLabels(history) {
      let lastLabel = '';
      return history.map((point) => {
        const label = formatTime(point.timestamp);
        if (!label) {
          return '';
        }
        if (label === lastLabel) {
          return '';
        }
        lastLabel = label;
        return label;
      });
    }

    return {
      createGauge,
      updateGaugeChart,
      updateInterfaceCards,
      formatThroughput,
      formatBytes,
      formatLatency,
      capitalizeWord,
    };
  };
})();
