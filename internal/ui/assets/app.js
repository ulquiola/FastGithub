const token = document.querySelector('meta[name="fastgithub-token"]').content;
const $ = id => document.getElementById(id);

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
  $('version').textContent = s.version;
  $('session').textContent = `127.0.0.1:${s.proxyPort}`;
  const rows = [
    ['代理端口', s.proxyPort],
    ['UI 端口', s.uiPort],
    ['拦截模式', s.intercept],
    ['CA 指纹', `${s.caFingerprint.slice(0, 16)}…`],
    ['DNS 源', `DoH ${s.dns.doh} / 回退 ${s.dns.fallback}`],
    ['PAC', `http://127.0.0.1:${s.proxyPort}/`],
  ];
  const list = $('status-list');
  list.replaceChildren();
  for (const [key, value] of rows) {
    const dt = document.createElement('dt');
    dt.textContent = key;
    const dd = document.createElement('dd');
    dd.textContent = value;
    list.append(dt, dd);
  }
}

async function loadDomains() {
  const domains = await fetch('/api/domains').then(r => r.json());
  $('domain-count').textContent = `(${domains.length})`;
  const body = $('domains');
  body.replaceChildren();
  for (const d of domains) {
    const tr = document.createElement('tr');
    const name = document.createElement('td');
    name.textContent = d.pattern;
    const hits = document.createElement('td');
    hits.className = 'num';
    hits.textContent = d.hits;
    tr.append(name, hits);
    body.appendChild(tr);
  }
}

async function loadFlow() {
  const f = await fetch('/flowStatistics').then(r => r.json());
  $('flow-read').textContent = formatBytes(f.TotalRead);
  $('flow-write').textContent = formatBytes(f.TotalWrite);
  $('flow-rate').textContent = `↑ ${formatBytes(f.ReadRate)}/s · ↓ ${formatBytes(f.WriteRate)}/s`;
}

async function loadCertStatus() {
  const badge = $('cert-status');
  try {
    const s = await fetch('/api/cert/status').then(r => r.json());
    if (s.installed) {
      badge.textContent = s.scope === 'system' ? '已安装 · 系统级' : '已安装 · 用户级';
      badge.className = 'badge ok';
    } else {
      badge.textContent = '未安装';
      badge.className = 'badge muted';
    }
    $('install-cert').disabled = s.installed;
    $('uninstall-cert').disabled = !s.installed;
  } catch (e) {
    badge.textContent = '检测失败';
    badge.className = 'badge err';
    $('install-cert').disabled = false;
    $('uninstall-cert').disabled = false;
  }
}

async function loadSystemProxy() {
  const badge = $('sysproxy-status');
  const detail = $('sysproxy-detail');
  const enable = $('enable-sysproxy');
  const disable = $('disable-sysproxy');
  try {
    const s = await fetch('/api/systemproxy').then(r => r.json());
    if (!s.supported) {
      badge.textContent = '不可用';
      badge.className = 'badge muted';
      detail.textContent = '当前平台请在系统中手动设置代理';
      enable.disabled = true;
      disable.disabled = true;
      return;
    }
    if (s.enabled) {
      badge.textContent = '已启用';
      badge.className = 'badge ok';
      detail.textContent = `${s.service} → ${s.server}:${s.port}`;
    } else {
      badge.textContent = '未启用';
      badge.className = 'badge muted';
      detail.textContent = s.service ? `${s.service} 未指向 FastGithub` : '未检测到可用网络服务';
    }
    enable.disabled = s.enabled;
    disable.disabled = !s.enabled;
  } catch (e) {
    badge.textContent = '检测失败';
    badge.className = 'badge err';
    detail.textContent = e.message;
    enable.disabled = false;
    disable.disabled = true;
  }
}

function connectLogs() {
  const list = $('logs');
  const source = new EventSource('/api/logs');
  source.onmessage = event => {
    const e = JSON.parse(event.data);
    const li = document.createElement('li');
    const time = document.createElement('span');
    time.className = 'time';
    time.textContent = e.time;
    const domain = document.createElement('span');
    domain.className = 'domain';
    domain.textContent = e.domain || '-';
    const method = document.createElement('span');
    method.className = 'method';
    method.textContent = e.method;
    const status = document.createElement('span');
    status.className = 'status ' + (e.status >= 500 ? 'err' : e.status >= 400 ? 'warn' : 'ok');
    status.textContent = e.status;
    const meta = document.createElement('span');
    meta.className = 'meta';
    meta.textContent = `${e.durationMs}ms · ${formatBytes(e.read)} / ${formatBytes(e.write)}`;
    li.append(time, domain, method, status, meta);
    list.prepend(li);
    while (list.children.length > 100) list.lastChild.remove();
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
    await loadCertStatus();
    await loadSystemProxy();
  }
}

$('install-cert').onclick = function () { post('/api/cert/install', this); };
$('uninstall-cert').onclick = function () {
  if (confirm('确定从信任库移除 CA？')) post('/api/cert/uninstall', this);
};
$('enable-sysproxy').onclick = function () { post('/api/systemproxy/enable', this); };
$('disable-sysproxy').onclick = function () {
  if (confirm('关闭系统代理后，浏览器将不再经过 FastGithub。确定关闭？')) {
    post('/api/systemproxy/disable', this);
  }
};
$('intercept').onclick = function () { post('/api/intercept', this); };

loadStatus();
loadDomains();
loadFlow();
loadCertStatus();
loadSystemProxy();
connectLogs();
setInterval(loadFlow, 1000);
setInterval(loadDomains, 5000);
