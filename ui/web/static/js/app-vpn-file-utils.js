(() => {
  window.SplitVPNUI = window.SplitVPNUI || {};

  window.SplitVPNUI.encodeSupportingFiles = async function encodeSupportingFiles(files) {
    if (!Array.isArray(files) || files.length === 0) {
      return [];
    }
    const encoded = [];
    for (const file of files) {
      if (!file || !file.name) {
        continue;
      }
      const arrayBuffer = await file.arrayBuffer();
      const bytes = new Uint8Array(arrayBuffer);
      let binary = '';
      const chunkSize = 0x8000;
      for (let i = 0; i < bytes.length; i += chunkSize) {
        const slice = bytes.subarray(i, Math.min(i + chunkSize, bytes.length));
        binary += String.fromCharCode(...slice);
      }
      encoded.push({ name: file.name, contentBase64: btoa(binary) });
    }
    return encoded;
  };

  window.SplitVPNUI.normalizeVPNType = function normalizeVPNType(value) {
    const raw = String(value || '').trim().toLowerCase();
    if (raw === 'wireguard' || raw === 'wg' || raw === 'external') {
      return 'wireguard';
    }
    if (raw === 'openvpn' || raw === 'ovpn') {
      return 'openvpn';
    }
    if (raw === 'amneziawg' || raw === 'awg' || raw === 'amnezia') {
      return 'amneziawg';
    }
    return '';
  };

  window.SplitVPNUI.hasAmneziaWGKeys = function hasAmneziaWGKeys(content) {
    return /^\s*(?:jc|jmin|jmax|s[1-4]|h[1-4]|i[1-5]|j[1-3]|itime)\s*=/im.test(String(content || ''));
  };

  window.SplitVPNUI.detectVPNType = function detectVPNType(fileName, content) {
    const name = String(fileName || '').toLowerCase();
    if (name.endsWith('.ovpn')) {
      return 'openvpn';
    }
    const text = String(content || '').toLowerCase();
    const wgLike = text.includes('[interface]') && text.includes('[peer]');
    if (wgLike && window.SplitVPNUI.hasAmneziaWGKeys(content)) {
      return 'amneziawg';
    }
    if (name.endsWith('.wg') || name.endsWith('.conf') || wgLike) {
      return 'wireguard';
    }
    if (text.includes('\nremote ') || text.includes('\nclient') || text.includes('<ca>')) {
      return 'openvpn';
    }
    return '';
  };
})();
