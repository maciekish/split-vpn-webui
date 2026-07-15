(() => {
  window.SplitVPNUI = window.SplitVPNUI || {};

  window.SplitVPNUI.createSpeedtestController = function createSpeedtestController(ctx) {
    const {
      speedtestModalElement,
      speedtestModal,
      speedtestTitle,
      speedtestStatus,
      speedtestServer,
      speedtestPhase,
      speedtestPing,
      speedtestJitter,
      speedtestDownloadValue,
      speedtestUploadValue,
      speedtestProgress,
      speedtestChartCanvas,
    } = ctx || {};

    if (
      !speedtestModalElement ||
      !speedtestModal ||
      !speedtestTitle ||
      !speedtestStatus ||
      !speedtestServer ||
      !speedtestPhase ||
      !speedtestPing ||
      !speedtestJitter ||
      !speedtestDownloadValue ||
      !speedtestUploadValue ||
      !speedtestProgress
    ) {
      return null;
    }

    const state = {
      source: null,
      chart: null,
      finished: false,
      sampleIndex: 0,
    };

    speedtestModalElement.addEventListener('hidden.bs.modal', () => {
      stop();
      if (state.chart) {
        state.chart.destroy();
        state.chart = null;
      }
    });

    function formatMbps(value) {
      const numeric = Number(value || 0);
      if (!Number.isFinite(numeric) || numeric <= 0) {
        return '0.0';
      }
      // Values share a fixed "Mbps" unit label, so render the full magnitude
      // rather than a "k" suffix (a 2085 Mbps link reads as "2085", not "2.09k").
      return numeric >= 100 ? numeric.toFixed(0) : numeric.toFixed(1);
    }

    function formatMs(value) {
      const numeric = Number(value || 0);
      if (!Number.isFinite(numeric) || numeric <= 0) {
        return '–';
      }
      return `${numeric.toFixed(numeric >= 100 ? 0 : 1)} ms`;
    }

    function open(iface, label) {
      const device = String(iface || '').trim();
      if (!device) {
        return;
      }
      stop();
      resetUI(label || device);
      speedtestModal.show();
      buildChart();
      setPhase('Selecting server…', 0);
      const url = `/api/speedtest/stream?iface=${encodeURIComponent(device)}&label=${encodeURIComponent(label || device)}`;
      const source = new EventSource(url);
      state.source = source;
      state.finished = false;
      source.onmessage = (event) => {
        let payload;
        try {
          payload = JSON.parse(event.data);
        } catch (err) {
          return;
        }
        handleEvent(payload);
      };
      source.onerror = () => {
        if (state.finished) {
          return;
        }
        setStatus('Connection to the speed test stream was lost.', true);
        stop();
      };
    }

    function stop() {
      if (state.source) {
        state.source.close();
        state.source = null;
      }
    }

    function handleEvent(evt) {
      switch (evt.phase) {
        case 'server':
          renderServer(evt);
          setPhase('Measuring latency…', 0.05);
          break;
        case 'ping':
          speedtestPing.textContent = `Ping ${formatMs(evt.pingMs)}`;
          speedtestJitter.textContent = `Jitter ${formatMs(evt.jitterMs)}`;
          setPhase('Testing download…', 0.1);
          break;
        case 'download':
          speedtestDownloadValue.textContent = formatMbps(evt.downloadMbps);
          setPhase('Testing download…', 0.1 + 0.45 * Number(evt.progress || 0));
          pushSample(Number(evt.downloadMbps || 0), null);
          break;
        case 'upload':
          speedtestUploadValue.textContent = formatMbps(evt.uploadMbps);
          setPhase('Testing upload…', 0.55 + 0.45 * Number(evt.progress || 0));
          pushSample(null, Number(evt.uploadMbps || 0));
          break;
        case 'done':
          renderServer(evt);
          state.finished = true;
          speedtestDownloadValue.textContent = formatMbps(evt.downloadMbps);
          speedtestUploadValue.textContent = formatMbps(evt.uploadMbps);
          speedtestPing.textContent = `Ping ${formatMs(evt.pingMs)}`;
          speedtestJitter.textContent = `Jitter ${formatMs(evt.jitterMs)}`;
          setPhase('Complete', 1);
          setStatus('Speed test complete.', false);
          stop();
          break;
        case 'error':
          state.finished = true;
          setStatus(evt.message || 'Speed test failed.', true);
          setPhase('Failed', 0);
          stop();
          break;
        default:
          break;
      }
    }

    function renderServer(evt) {
      const parts = [];
      if (evt.serverSponsor) {
        parts.push(evt.serverSponsor);
      }
      const location = [evt.serverName, evt.serverCountry].filter(Boolean).join(', ');
      let text = parts.join(' ');
      if (location) {
        text = text ? `${text} — ${location}` : location;
      }
      if (evt.serverDistanceKm) {
        text += ` (${Math.round(evt.serverDistanceKm)} km)`;
      }
      speedtestServer.textContent = text || 'Speed test server';
    }

    function setPhase(label, progress) {
      speedtestPhase.textContent = label;
      const pct = Math.max(0, Math.min(100, Math.round(Number(progress || 0) * 100)));
      speedtestProgress.style.width = `${pct}%`;
      speedtestProgress.setAttribute('aria-valuenow', String(pct));
    }

    function setStatus(message, isError) {
      speedtestStatus.classList.remove('d-none', 'alert-success', 'alert-danger');
      speedtestStatus.classList.add(isError ? 'alert-danger' : 'alert-success');
      speedtestStatus.textContent = message || '';
    }

    function resetUI(label) {
      state.sampleIndex = 0;
      speedtestTitle.innerHTML = `<i class="bi bi-speedometer2 me-2"></i>Speed Test — ${escapeHTML(label)}`;
      speedtestServer.textContent = 'Selecting nearest server…';
      speedtestPhase.textContent = '';
      speedtestPing.textContent = 'Ping –';
      speedtestJitter.textContent = 'Jitter –';
      speedtestDownloadValue.textContent = '0.0';
      speedtestUploadValue.textContent = '0.0';
      speedtestProgress.style.width = '0%';
      speedtestStatus.classList.add('d-none');
    }

    function buildChart() {
      if (!speedtestChartCanvas || typeof Chart === 'undefined') {
        return;
      }
      if (state.chart) {
        state.chart.destroy();
        state.chart = null;
      }
      state.chart = new Chart(speedtestChartCanvas.getContext('2d'), {
        type: 'line',
        data: {
          labels: [],
          datasets: [
            {
              label: 'Download',
              data: [],
              borderColor: '#60a5fa',
              backgroundColor: 'rgba(96, 165, 250, 0.15)',
              tension: 0.3,
              pointRadius: 0,
              fill: true,
              spanGaps: false,
            },
            {
              label: 'Upload',
              data: [],
              borderColor: '#f87171',
              backgroundColor: 'rgba(248, 113, 113, 0.15)',
              tension: 0.3,
              pointRadius: 0,
              fill: true,
              spanGaps: false,
            },
          ],
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          animation: false,
          plugins: { legend: { display: true, position: 'bottom' } },
          scales: {
            x: { display: false },
            y: { beginAtZero: true, title: { display: true, text: 'Mbps' } },
          },
        },
      });
    }

    function pushSample(download, upload) {
      if (!state.chart) {
        return;
      }
      state.sampleIndex += 1;
      state.chart.data.labels.push(state.sampleIndex);
      state.chart.data.datasets[0].data.push(download);
      state.chart.data.datasets[1].data.push(upload);
      state.chart.update('none');
    }

    function escapeHTML(value) {
      return String(value || '')
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    return { open, stop };
  };
})();
