package v1

const dashboardHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>btrfs-nfs-csi agent</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body { font-family: system-ui, sans-serif; background: #0d1117; color: #c9d1d9; padding: 20px; }
h1 { font-size: 1.4em; margin-bottom: 16px; }
h2 { font-size: 1.1em; margin: 20px 0 8px; color: #8b949e; }
.status { display: inline-block; padding: 2px 10px; border-radius: 12px; font-size: .85em; font-weight: 600; }
.status.ok { background: #1b4332; color: #52c41a; }
.status.degraded { background: #4a1d1d; color: #f85149; }
.meta { color: #8b949e; font-size: .85em; margin-left: 12px; }
.tenant { color: #d2a8ff; font-size: .85em; margin-left: 8px; }
table { width: 100%; border-collapse: collapse; margin-top: 4px; font-size: .9em; }
th { text-align: left; padding: 6px 10px; border-bottom: 1px solid #30363d; color: #8b949e; font-weight: 500; }
td { padding: 6px 10px; border-bottom: 1px solid #21262d; }
tr:hover td { background: #161b22; }
.mono { font-family: ui-monospace, monospace; font-size: .85em; }
.bytes { color: #79c0ff; }
.empty { color: #484f58; font-style: italic; padding: 16px 10px; }
#error { color: #f85149; margin: 10px 0; display: none; }
.refresh { float: right; color: #484f58; font-size: .8em; }
.features { margin: 4px 0 0; font-size: .8em; color: #8b949e; }
.feat { display: inline-block; padding: 1px 8px; border-radius: 10px; background: #21262d; margin-right: 6px; }
.btn-del { background: none; border: 1px solid #f8514922; color: #f85149; padding: 2px 10px; border-radius: 6px; cursor: pointer; font-size: .8em; }
.btn-del:hover { background: #f8514922; }
.fs-bar { margin: 12px 0 8px; }
.fs-bar-outer { background: #21262d; border-radius: 6px; height: 20px; overflow: hidden; }
.fs-bar-inner { background: #238636; height: 100%; border-radius: 6px; transition: width .3s; }
.fs-bar-inner.warn { background: #d29922; }
.fs-bar-inner.crit { background: #f85149; }
.fs-info { font-size: .85em; color: #8b949e; margin-top: 4px; }
.quota-bar { display: inline-block; width: 80px; height: 8px; background: #21262d; border-radius: 4px; overflow: hidden; vertical-align: middle; margin-left: 6px; }
.quota-fill { height: 100%; border-radius: 4px; }
</style>
</head>
<body>
<h1>btrfs-nfs-csi agent<span class="tenant">{{TENANT}}</span> <span id="health"></span><span id="version" class="meta"></span><span id="uptime" class="meta"></span></h1>
<div id="features" class="features"></div>
<div id="error"></div>
<span class="refresh">auto-refresh {{REFRESH}}s</span>

<div class="fs-bar">
<div class="fs-bar-outer"><div id="fs-fill" class="fs-bar-inner" style="width:0"></div></div>
<div id="fs-info" class="fs-info"></div>
</div>

<h2>Volumes</h2>
<table>
<thead><tr><th>Name</th><th>Quota</th><th></th><th>Compression</th><th>NoCOW</th><th>Mode</th><th>UID:GID</th><th>Clients</th><th>Last Attach</th><th>Created</th><th></th></tr></thead>
<tbody id="volumes"><tr><td class="empty" colspan="11">loading...</td></tr></tbody>
</table>

<h2>Snapshots</h2>
<table>
<thead><tr><th>Name</th><th>Volume</th><th>Size</th><th>Used</th><th>Exclusive</th><th>Created</th><th></th></tr></thead>
<tbody id="snapshots"><tr><td class="empty" colspan="7">loading...</td></tr></tbody>
</table>

<h2>NFS Exports</h2>
<table>
<thead><tr><th>Path</th><th>Client</th><th></th></tr></thead>
<tbody id="exports"><tr><td class="empty" colspan="3">loading...</td></tr></tbody>
</table>

<script>
function fmt(b) {
  if (!b) return '-';
  const u = ['B','KiB','MiB','GiB','TiB'];
  let i = 0;
  let v = b;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return v.toFixed(i ? 1 : 0) + ' ' + u[i];
}

function fmtUptime(s) {
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d > 0) return d + 'd ' + h + 'h';
  if (h > 0) return h + 'h ' + m + 'm';
  return m + 'm';
}

function fmtDate(s) {
  if (!s) return '-';
  const d = new Date(s);
  return d.toLocaleDateString() + ' ' + d.toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'});
}

async function api(path, opts) {
  const r = await fetch(path, opts);
  if (!r.ok) throw new Error(r.status + ' ' + r.statusText);
  if (r.status === 204) return null;
  return r.json();
}

async function del(type, name) {
  if (!confirm('WARNING: You are about to permanently delete ' + type + ' "' + name + '" directly via the agent API.\n\nOnly do this if you know exactly what you are doing. Deleting resources here bypasses Kubernetes - PVs and PVCs will NOT be cleaned up automatically.\n\nAre you sure?')) return;
  try {
    await api('/v1/' + type + 's/' + name, { method: 'DELETE' });
    refresh();
  } catch(err) {
    alert('Delete failed: ' + err.message);
  }
}

async function unexport(volName, client) {
  if (!confirm('Remove NFS export of "' + volName + '" for client ' + client + '?')) return;
  try {
    await api('/v1/volumes/' + volName + '/export', { method: 'DELETE', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({client: client}) });
    refresh();
  } catch(err) {
    alert('Unexport failed: ' + err.message);
  }
}

async function refresh() {
  try {
    const h = await api('/healthz');
    document.getElementById('health').className = 'status ' + h.status;
    document.getElementById('health').textContent = h.status;
    document.getElementById('version').textContent = 'v' + h.version;
    document.getElementById('uptime').textContent = 'up ' + fmtUptime(h.uptime_seconds);
    if (h.features) {
      document.getElementById('features').innerHTML = Object.entries(h.features).map(([k, v]) => '<span class="feat">' + k + ': ' + v + '</span>').join('');
    }

    try {
      const st = await api('/v1/stats');
      const pct = st.total_bytes ? (st.used_bytes / st.total_bytes * 100) : 0;
      const fill = document.getElementById('fs-fill');
      fill.style.width = pct.toFixed(1) + '%';
      fill.className = 'fs-bar-inner' + (pct > 90 ? ' crit' : pct > 75 ? ' warn' : '');
      document.getElementById('fs-info').textContent = fmt(st.used_bytes) + ' / ' + fmt(st.total_bytes) + ' (' + pct.toFixed(1) + '% used, ' + fmt(st.free_bytes) + ' free)';
    } catch(e) {}

    const v = await api('/v1/volumes');
    const vb = document.getElementById('volumes');
    if (!v.volumes.length) {
      vb.innerHTML = '<tr><td class="empty" colspan="11">no volumes</td></tr>';
    } else {
      vb.innerHTML = v.volumes.map(vol => {
        const quota = vol.quota_bytes || vol.size_bytes;
        const pct = quota ? (vol.used_bytes / quota * 100) : 0;
        const color = pct > 90 ? '#f85149' : pct > 75 ? '#d29922' : '#238636';
        return '<tr>' +
        '<td class="mono">' + vol.name + '</td>' +
        '<td class="bytes">' + fmt(vol.used_bytes) + ' / ' + fmt(quota) + '</td>' +
        '<td><div class="quota-bar"><div class="quota-fill" style="width:' + Math.min(pct,100).toFixed(1) + '%;background:' + color + '"></div></div></td>' +
        '<td>' + (vol.compression || '-') + '</td>' +
        '<td>' + (vol.nocow ? 'yes' : '-') + '</td>' +
        '<td class="mono">' + (vol.mode || '-') + '</td>' +
        '<td>' + vol.uid + ':' + vol.gid + '</td>' +
        '<td>' + vol.clients + '</td>' +
        '<td>' + (vol.last_attach_at ? fmtDate(vol.last_attach_at) : '-') + '</td>' +
        '<td>' + fmtDate(vol.created_at) + '</td>' +
        '<td><button class="btn-del" onclick="del(\'volume\',\'' + vol.name + '\')">delete</button></td>' +
        '</tr>';
      }).join('');
    }

    const s = await api('/v1/snapshots');
    const sb = document.getElementById('snapshots');
    if (!s.snapshots.length) {
      sb.innerHTML = '<tr><td class="empty" colspan="7">no snapshots</td></tr>';
    } else {
      sb.innerHTML = s.snapshots.map(snap =>
        '<tr>' +
        '<td class="mono">' + snap.name + '</td>' +
        '<td class="mono">' + snap.volume + '</td>' +
        '<td class="bytes">' + fmt(snap.size_bytes) + '</td>' +
        '<td class="bytes">' + fmt(snap.used_bytes) + '</td>' +
        '<td class="bytes">' + fmt(snap.exclusive_bytes) + '</td>' +
        '<td>' + fmtDate(snap.created_at) + '</td>' +
        '<td><button class="btn-del" onclick="del(\'snapshot\',\'' + snap.name + '\')">delete</button></td>' +
        '</tr>'
      ).join('');
    }

    const e = await api('/v1/exports');
    const eb = document.getElementById('exports');
    if (!e.exports.length) {
      eb.innerHTML = '<tr><td class="empty" colspan="3">no exports</td></tr>';
    } else {
      eb.innerHTML = e.exports.map(ex => {
        const vol = ex.path.split('/').pop();
        return '<tr><td class="mono">' + ex.path + '</td><td>' + ex.client + '</td>' +
          '<td><button class="btn-del" onclick="unexport(\'' + vol + '\',\'' + ex.client + '\')">delete</button></td></tr>';
      }).join('');
    }

    document.getElementById('error').style.display = 'none';
  } catch(err) {
    document.getElementById('error').textContent = 'Error: ' + err.message;
    document.getElementById('error').style.display = 'block';
  }
}

refresh();
setInterval(refresh, {{REFRESH}} * 1000);
</script>
</body>
</html>`
