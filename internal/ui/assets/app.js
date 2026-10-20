const token = document.querySelector('meta[name="fastgithub-token"]').content;

async function loadStatus() {
  const status = await fetch('/api/status').then(r => r.json());
  document.getElementById('version').textContent = status.version;
  document.getElementById('status').textContent =
    `代理 ${status.proxyPort} / UI ${status.uiPort} / 拦截 ${status.intercept}`;
  document.getElementById('status').textContent += ` / CA ${status.caFingerprint.slice(0, 12)}…`;
}

async function loadDomains() {
  const domains = await fetch('/api/domains').then(r => r.json());
  const body = document.getElementById('domains');
  body.innerHTML = '';
  for (const d of domains) {
    const tr = document.createElement('tr');
    const pattern = document.createElement('td');
    pattern.textContent = d.pattern;
    const hits = document.createElement('td');
    hits.textContent = String(d.hits);
    tr.append(pattern, hits);
    body.appendChild(tr);
  }
}

async function loadFlow() {
  const flow = await fetch('/flowStatistics').then(r => r.json());
  document.getElementById('flow').textContent =
    `读 ${flow.TotalRead} / 写 ${flow.TotalWrite}（${flow.ReadRate}/${flow.WriteRate} B/s）`;
}

function connectLogs() {
  const list = document.getElementById('logs');
  const source = new EventSource('/api/logs');
  source.onmessage = event => {
    const e = JSON.parse(event.data);
    const li = document.createElement('li');
    li.textContent = `${e.time} ${e.domain} ${e.method} ${e.status} ${e.durationMs}ms ${e.read}/${e.write}`;
    list.prepend(li);
    while (list.children.length > 100) list.lastChild.remove();
  };
}

async function post(path) {
  const resp = await fetch(path, { method: 'POST', headers: { 'X-FastGithub-Token': token } });
  const body = await resp.json().catch(() => ({}));
  alert(body.error || body.ok || resp.status);
}

document.getElementById('install-cert').onclick = () => post('/api/cert/install');
document.getElementById('uninstall-cert').onclick = () => {
  if (confirm('确定从系统信任库移除 CA？')) post('/api/cert/uninstall');
};
document.getElementById('intercept').onclick = () => post('/api/intercept');

loadStatus();
loadDomains();
loadFlow();
setInterval(loadFlow, 1000);
setInterval(loadDomains, 5000);
connectLogs();
