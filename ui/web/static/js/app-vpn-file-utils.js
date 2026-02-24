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
})();
