const token = document.querySelector('meta[name="fastgithub-token"]').content;
const $ = id => document.getElementById(id);
const SVG_NS = 'http://www.w3.org/2000/svg';

function formatBytes(n) {
  if (!n) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let value = n;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

let toastTimer;
function showToast(text, ok = true) {
  const el = $('toast');
  el.textContent = text;
  el.className = 'toast ' + (ok ? 'ok' : 'err');
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.hidden = true; }, 3500);
}

async function loadStatus() {
  const s = await fetch('/api/status').then(r => r.json());
  const addr = `127.0.0.1:${s.proxyPort}`;
  $('version').textContent = s.version;
  $('session').textContent = addr;
  $('top-proxy').textContent = addr;
  $('proxy-address').textContent = addr;
  $('ui-port').textContent = s.uiPort;
  $('intercept-mode').textContent = s.intercept;
  $('ca-fingerprint').textContent = `${s.caFingerprint.slice(0, 16)}…`;
  $('dns-sources').textContent = `DoH ${s.dns.doh} / 回退 ${s.dns.fallback}`;
}

function renderSparkline(samples) {
  const svg = $('flow-chart');
  svg.replaceChildren();
  if (!samples.length) return;
  const w = 320;
  const h = 80;
  const pad = 4;
  const max = Math.max(1, ...samples.map(s => Math.max(s.read, s.write)));
  const points = key => samples.map((s, i) => {
    const x = samples.length === 1 ? w : (i / (samples.length - 1)) * w;
    const y = h - pad - (s[key] / max) * (h - pad * 2);
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  }).join(' ');
  for (const [key, cls] of [['read', 'spark-read'], ['write', 'spark-write']]) {
    const poly = document.createElementNS(SVG_NS, 'polyline');
    poly.setAttribute('points', points(key));
    poly.setAttribute('class', cls);
    svg.appendChild(poly);
  }
}

async function loadFlow() {
  try {
    const f = await fetch('/api/flow').then(r => r.json());
    $('flow-read').textContent = formatBytes(f.totalRead);
    $('flow-write').textContent = formatBytes(f.totalWrite);
    $('flow-rate').textContent = `↑ ${formatBytes(f.readRate)}/s · ↓ ${formatBytes(f.writeRate)}/s`;
    renderSparkline(f.samples || []);
  } catch (e) {
    // 单次采样失败不打断页面。
  }
}

let allDomains = [];

async function loadDomains() {
  allDomains = await fetch('/api/domains').then(r => r.json());
  $('domain-count').textContent = `(${allDomains.length})`;
  renderDomains();
}

function renderDomains() {
  const body = $('domains');
  body.replaceChildren();
  const filter = $('domain-filter').value.trim().toLowerCase();
  const rows = allDomains.filter(d => !filter || d.pattern.toLowerCase().includes(filter));
  if (!rows.length) {
    const tr = document.createElement('tr');
    const td = document.createElement('td');
    td.colSpan = 2;
    td.className = 'empty';
    td.textContent = allDomains.length ? '没有匹配的规则' : '暂无规则';
    tr.appendChild(td);
    body.appendChild(tr);
    return;
  }
  const max = Math.max(1, ...allDomains.map(d => d.hits));
  for (const d of rows) {
    const tr = document.createElement('tr');
    const name = document.createElement('td');
    name.className = 'mono';
    name.textContent = d.pattern;
    const hits = document.createElement('td');
    hits.className = 'num';
    const wrap = document.createElement('div');
    wrap.className = 'hit';
    const bar = document.createElement('span');
    bar.className = 'hit-bar';
    const fill = document.createElement('i');
    fill.style.width = `${(d.hits / max) * 100}%`;
    bar.appendChild(fill);
    const num = document.createElement('span');
    num.className = 'hit-num';
    num.textContent = d.hits;
    wrap.append(bar, num);
    hits.appendChild(wrap);
    tr.append(name, hits);
    body.appendChild(tr);
  }
}

async function loadCertStatus() {
  const badge = $('cert-status');
  const top = $('top-cert-status');
  try {
    const s = await fetch('/api/cert/status').then(r => r.json());
    if (s.installed) {
      badge.textContent = s.scope === 'system' ? '已安装 · 系统级' : '已安装 · 用户级';
      badge.className = 'badge ok';
      top.textContent = '已安装';
      top.className = 'badge ok';
    } else {
      badge.textContent = '未安装';
      badge.className = 'badge muted';
      top.textContent = '未安装';
      top.className = 'badge muted';
    }
    $('install-cert').disabled = s.installed;
    $('uninstall-cert').disabled = !s.installed;
  } catch (e) {
    badge.textContent = '检测失败';
    badge.className = 'badge err';
    top.textContent = '检测失败';
    top.className = 'badge err';
    $('install-cert').disabled = false;
    $('uninstall-cert').disabled = false;
  }
}

async function loadSystemProxy() {
  const badge = $('sysproxy-status');
  const top = $('top-sysproxy-status');
  const detail = $('sysproxy-detail');
  const toggle = $('sysproxy-toggle');
  const label = $('sysproxy-label');
  try {
    const s = await fetch('/api/systemproxy').then(r => r.json());
    if (!s.supported) {
      badge.textContent = '不可用';
      badge.className = 'badge muted';
      top.textContent = '不可用';
      top.className = 'badge muted';
      detail.textContent = s.note || '当前平台请在系统中手动设置代理';
      toggle.disabled = true;
      toggle.setAttribute('aria-checked', 'false');
      label.textContent = '不可用';
      return;
    }
    const on = !!s.enabled;
    badge.textContent = on ? '已启用' : '未启用';
    badge.className = on ? 'badge ok' : 'badge muted';
    top.textContent = on ? '已启用' : '未启用';
    top.className = on ? 'badge ok' : 'badge muted';
    toggle.disabled = false;
    toggle.setAttribute('aria-checked', on ? 'true' : 'false');
    label.textContent = on ? '已启用' : '未启用';
    detail.textContent = s.note || (s.service ? `${s.service} → ${s.server}:${s.port}` : '未检测到可用网络服务');
  } catch (e) {
    badge.textContent = '检测失败';
    badge.className = 'badge err';
    top.textContent = '检测失败';
    top.className = 'badge err';
    detail.textContent = e.message;
    toggle.disabled = true;
  }
}

const logEntries = [];

function renderLogs() {
  const list = $('logs');
  list.replaceChildren();
  const filter = $('log-filter').value.trim().toLowerCase();
  const entries = logEntries.filter(e => !filter ||
    [e.domain, e.method, String(e.status)].some(v => (v || '').toLowerCase().includes(filter)));
  if (!entries.length) {
    const li = document.createElement('li');
    li.className = 'empty';
    li.textContent = logEntries.length ? '没有匹配的请求' : '暂无请求';
    list.appendChild(li);
    return;
  }
  for (const e of entries) {
    const li = document.createElement('li');
    const time = document.createElement('span');
    time.className = 'time';
    time.textContent = e.time;
    const domain = document.createElement('span');
    domain.className = 'domain';
    domain.textContent = e.domain || '-';
    const method = document.createElement('span');
    method.className = 'method chip';
    method.textContent = e.method;
    const status = document.createElement('span');
    status.className = 'status ' + (e.status >= 500 ? 'err' : e.status >= 400 ? 'warn' : 'ok');
    status.textContent = e.status;
    const meta = document.createElement('span');
    meta.className = 'meta';
    meta.textContent = `${e.durationMs}ms · ${formatBytes(e.read)} / ${formatBytes(e.write)}`;
    li.append(time, domain, method, status, meta);
    list.appendChild(li);
  }
}

function connectLogs() {
  const source = new EventSource('/api/logs');
  source.onmessage = event => {
    logEntries.unshift(JSON.parse(event.data));
    if (logEntries.length > 100) logEntries.pop();
    renderLogs();
  };
}

async function post(path, button) {
  button.disabled = true;
  try {
    const resp = await fetch(path, { method: 'POST', headers: { 'X-FastGithub-Token': token } });
    const body = await resp.json().catch(() => ({}));
    if (!resp.ok) throw new Error(body.error || `HTTP ${resp.status}`);
    showToast('操作成功');
  } catch (e) {
    showToast(e.message, false);
  } finally {
    button.disabled = false;
    await loadCertStatus();
    await loadSystemProxy();
  }
}

function refreshRuntimeState() {
  loadSystemProxy();
  loadCertStatus();
}

$('copy-proxy').onclick = async function () {
  try {
    await navigator.clipboard.writeText($('proxy-address').textContent);
    showToast('已复制代理地址');
  } catch (e) {
    showToast('复制失败', false);
  }
};

$('install-cert').onclick = function () { post('/api/cert/install', this); };
$('uninstall-cert').onclick = function () {
  if (confirm('确定从信任库移除 CA？')) post('/api/cert/uninstall', this);
};
$('sysproxy-toggle').onclick = function () {
  if (this.disabled) return;
  const on = this.getAttribute('aria-checked') === 'true';
  post(on ? '/api/systemproxy/disable' : '/api/systemproxy/enable', this);
};
$('intercept').onclick = function () { post('/api/intercept', this); };
$('domain-filter').addEventListener('input', renderDomains);
$('log-filter').addEventListener('input', renderLogs);

loadStatus();
loadDomains();
loadFlow();
loadCertStatus();
loadSystemProxy();
connectLogs();
renderLogs();
setInterval(loadFlow, 1000);
setInterval(loadDomains, 5000);
setInterval(refreshRuntimeState, 5000);
document.addEventListener('visibilitychange', function () {
  if (!document.hidden) refreshRuntimeState();
});
window.addEventListener('focus', refreshRuntimeState);
