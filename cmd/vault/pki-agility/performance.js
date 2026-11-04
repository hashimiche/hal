const performanceModes = [
  {name: 'traditional', url: 'https://traditional.agility.localhost:8094/'},
  {name: 'hybrid', url: 'https://hybrid.agility.localhost:8095/'},
  {name: 'pqc', url: 'https://pqc.agility.localhost:8096/'},
  {name: 'pure', url: 'https://pure.agility.localhost:8097/'}
];
const browserWarmups = 1;
const browserRounds = 8;

function roundModes(round) {
  return performanceModes.map((_, index) => performanceModes[(index + round) % performanceModes.length]);
}

function median(values) {
  if (!values.length) return null;
  const sorted = [...values].sort((left, right) => left - right);
  const middle = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
}

function navigationSample(entry) {
  if (!entry || !Number.isFinite(entry.connectStart) ||
      !Number.isFinite(entry.secureConnectionStart) ||
      !Number.isFinite(entry.connectEnd) ||
      entry.connectStart <= 0 ||
      entry.secureConnectionStart < entry.connectStart ||
      entry.connectEnd <= entry.secureConnectionStart ||
      entry.requestStart < entry.connectEnd ||
      entry.responseStart <= entry.requestStart) {
    return {connection: null, tls: null, ttfb: null, status: 'Fresh connection timing unavailable'};
  }
  return {
    connection: entry.connectEnd - entry.connectStart,
    tls: entry.connectEnd - entry.secureConnectionStart,
    ttfb: entry.responseStart - entry.requestStart,
    status: 'Fresh navigation'
  };
}

async function reportFramedNavigation() {
  const params = new URLSearchParams(location.search);
  const run = params.get('perf_run');
  const parentOrigin = params.get('perf_parent');
  const mode = location.hostname.split('.')[0];
  if (window.parent === window || !run || !parentOrigin ||
      !performanceModes.some(item => item.name === mode) ||
      !performanceModes.some(item => new URL(item.url).origin === parentOrigin)) return;
  const sample = navigationSample(performance.getEntriesByType('navigation')[0]);
  try {
    const response = await fetch('/diagnostics', {cache: 'no-store'});
    const evidence = await response.json();
    if (!response.ok || !evidence.server_policy_match || evidence.configured.mode !== mode) {
      sample.connection = sample.tls = sample.ttfb = null;
      sample.status = 'TLS evidence mismatch';
    }
  } catch {
    sample.connection = sample.tls = sample.ttfb = null;
    sample.status = 'TLS evidence unavailable';
  }
  window.parent.postMessage({type: 'hal-agility-performance', run, mode, sample}, parentOrigin);
}

window.addEventListener('load', reportFramedNavigation);

const tabButtons = [document.getElementById('evidence-tab'), document.getElementById('performance-tab')];
function selectTab(selected) {
  for (const button of tabButtons) {
    const active = button === selected;
    button.setAttribute('aria-selected', String(active));
    button.tabIndex = active ? 0 : -1;
    document.getElementById(button.getAttribute('aria-controls')).hidden = !active;
  }
}
for (const button of tabButtons) {
  button.addEventListener('click', () => selectTab(button));
  button.addEventListener('keydown', event => {
    const index = tabButtons.indexOf(button);
    const target = event.key === 'ArrowRight' ? tabButtons[(index + 1) % tabButtons.length] :
      event.key === 'ArrowLeft' ? tabButtons[(index + tabButtons.length - 1) % tabButtons.length] :
      event.key === 'Home' ? tabButtons[0] : event.key === 'End' ? tabButtons[tabButtons.length - 1] : null;
    if (target) {
      event.preventDefault();
      selectTab(target);
      target.focus();
    }
  });
}

function performanceCell(mode, field) {
  return document.querySelector(`[data-perf-mode="${mode}"] [data-field="${field}"]`);
}

fetch('/performance.json', {cache: 'no-store'}).then(async response => {
  if (!response.ok) throw new Error('chain sizes unavailable');
  const data = await response.json();
  for (const {name} of performanceModes) {
    const bytes = data.chain_der_bytes?.[name];
    if (Number.isSafeInteger(bytes) && bytes > 0) {
      performanceCell(name, 'chain').textContent = `${bytes.toLocaleString()} B (${(bytes / 1024).toFixed(1)} KiB)`;
    }
  }
}).catch(() => {
  document.getElementById('performance-status').textContent = 'Chain sizes unavailable; check the deployment.';
});

function showSample(mode, sample) {
  for (const field of ['connection', 'tls', 'ttfb']) {
    const value = sample?.[field];
    performanceCell(mode, field).textContent = Number.isFinite(value) && value > 0
      ? `${value.toFixed(2)} ms` : 'Unavailable';
  }
  performanceCell(mode, 'status').textContent = sample?.status || 'Unavailable';
}

function measureMode(mode) {
  return new Promise(resolve => {
    const frame = document.createElement('iframe');
    frame.className = 'perf-frame';
    frame.title = `${mode.name} timing sample`;
    frame.loading = 'eager';
    const nonce = crypto.randomUUID();
    const url = new URL(mode.url);
    url.searchParams.set('perf_run', nonce);
    url.searchParams.set('perf_parent', location.origin);
    let finished = false;
    const finish = sample => {
      if (finished) return;
      finished = true;
      clearTimeout(timeout);
      window.removeEventListener('message', onMessage);
      frame.remove();
      resolve(sample);
    };
    const onMessage = event => {
      if (event.source !== frame.contentWindow || event.origin !== url.origin ||
          event.data?.type !== 'hal-agility-performance' ||
          event.data.run !== nonce || event.data.mode !== mode.name) return;
      finish(event.data.sample);
    };
    const timeout = setTimeout(() => finish({status: 'No browser timing returned'}), 15000);
    window.addEventListener('message', onMessage);
    frame.src = url.href;
    document.body.append(frame);
  });
}

document.getElementById('measure-browser').addEventListener('click', async event => {
  const button = event.currentTarget;
  const status = document.getElementById('performance-status');
  button.disabled = true;
  try {
    for (const mode of performanceModes) {
      showSample(mode.name, {status: 'Warming up…'});
    }
    const skipped = new Map();
    for (let warmup = 0; warmup < browserWarmups; warmup++) {
      for (const mode of roundModes(warmup)) {
        status.textContent = `Warming up ${mode.name}…`;
        const sample = await measureMode(mode);
        if (['No browser timing returned', 'TLS evidence mismatch', 'TLS evidence unavailable'].includes(sample?.status)) {
          skipped.set(mode.name, sample.status);
        }
      }
    }
    const samples = Object.fromEntries(performanceModes.map(mode => [mode.name, []]));
    for (let round = 0; round < browserRounds; round++) {
      for (const mode of roundModes(round)) {
        if (skipped.has(mode.name)) continue;
        status.textContent = `Measuring round ${round + 1} of ${browserRounds}: ${mode.name}…`;
        const sample = await measureMode(mode);
        if (['No browser timing returned', 'TLS evidence mismatch', 'TLS evidence unavailable'].includes(sample?.status)) {
          skipped.set(mode.name, sample.status);
          continue;
        }
        if (['connection', 'tls', 'ttfb'].every(field => Number.isFinite(sample?.[field]) && sample[field] > 0)) {
          samples[mode.name].push(sample);
        }
      }
    }
    for (const mode of performanceModes) {
      const valid = samples[mode.name];
      showSample(mode.name, {
        ...Object.fromEntries(['connection', 'tls', 'ttfb'].map(field =>
          [field, median(valid.map(sample => sample[field]))])),
        status: valid.length ? `Median of ${valid.length}/${browserRounds} fresh navigations` :
          (skipped.get(mode.name) || 'Fresh connection timing unavailable')
      });
    }
    status.textContent = 'Measurement complete. Medians use up to eight fresh page loads per mode; run the CLI benchmark for verified median and p95.';
  } finally {
    button.disabled = false;
  }
});
