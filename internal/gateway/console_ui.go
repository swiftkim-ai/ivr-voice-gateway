package gateway

const consoleHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>IVR Voice Gateway — Console</title>
<style>
:root {
  --bg: #0f1117;
  --surface: #1a1d27;
  --surface-raised: #242836;
  --border: #2e3345;
  --text: #e1e4ed;
  --text-muted: #8b8fa3;
  --primary: #5b7ff5;
  --primary-fg: #fff;
  --success: #34d399;
  --warning: #fbbf24;
  --danger: #f87171;
  --info: #60a5fa;
  --radius: 8px;
  --radius-lg: 12px;
}
@media (prefers-color-scheme: light) {
  :root {
    --bg: #f4f5f7;
    --surface: #fff;
    --surface-raised: #f9fafb;
    --border: #e2e5ea;
    --text: #1a1d27;
    --text-muted: #6b7085;
  }
}
* { box-sizing: border-box; margin: 0; padding: 0; }
body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  background: var(--bg);
  color: var(--text);
  line-height: 1.5;
  min-height: 100vh;
}
.header {
  padding: 16px 24px;
  border-bottom: 1px solid var(--border);
  display: flex;
  align-items: center;
  gap: 12px;
}
.header h1 { font-size: 18px; font-weight: 600; }
.status {
  font-size: 12px;
  padding: 2px 8px;
  border-radius: 10px;
  background: var(--surface-raised);
  color: var(--text-muted);
}
.status.connected { background: #064e3b; color: var(--success); }
.main {
  display: grid;
  grid-template-columns: 1fr 360px;
  gap: 1px;
  background: var(--border);
  height: calc(100vh - 57px);
}
@media (max-width: 768px) {
  .main { grid-template-columns: 1fr; grid-template-rows: 1fr auto; }
}
.transcript-panel, .handoff-panel {
  background: var(--bg);
  overflow-y: auto;
  padding: 16px 24px;
}
.handoff-panel { background: var(--surface); }
.section-title {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-muted);
  margin-bottom: 12px;
}
.event {
  padding: 8px 0;
  border-bottom: 1px solid var(--border);
  font-size: 14px;
}
.event:last-child { border-bottom: none; }
.event .meta {
  font-size: 11px;
  color: var(--text-muted);
  margin-bottom: 2px;
}
.event.reply .content { color: var(--primary); }
.event.transcript .content { color: var(--text); }
.event.action .content { color: var(--warning); font-family: monospace; font-size: 13px; }
.event.escalation .content { color: var(--danger); font-weight: 600; }
.event.call_started .content, .event.call_ended .content { color: var(--text-muted); font-style: italic; }

.handoff-card {
  display: none;
  background: var(--surface-raised);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 16px;
}
.handoff-card.active { display: block; }
.handoff-card h3 {
  font-size: 14px;
  font-weight: 600;
  margin-bottom: 12px;
  color: var(--danger);
}
.field { margin-bottom: 10px; }
.field .label {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-muted);
  margin-bottom: 2px;
}
.field .value { font-size: 14px; }
.badge {
  display: inline-block;
  font-size: 12px;
  padding: 2px 8px;
  border-radius: 4px;
  font-weight: 500;
}
.badge.sentiment-calm { background: #064e3b; color: var(--success); }
.badge.sentiment-frustrated { background: #78350f; color: var(--warning); }
.badge.sentiment-angry { background: #7f1d1d; color: var(--danger); }
.empty {
  color: var(--text-muted);
  text-align: center;
  padding: 48px 24px;
  font-size: 14px;
}
</style>
</head>
<body>
<div class="header">
  <h1>Console</h1>
  <span id="status" class="status">Connecting</span>
</div>
<div class="main">
  <div class="transcript-panel">
    <div class="section-title">Live transcript</div>
    <div id="events"><div class="empty">Waiting for a call...</div></div>
  </div>
  <div class="handoff-panel">
    <div class="section-title">Handoff</div>
    <div id="handoff" class="handoff-card">
      <h3>Escalation</h3>
      <div class="field"><div class="label">Identity</div><div class="value" id="h-identity">—</div></div>
      <div class="field"><div class="label">Intent</div><div class="value" id="h-intent">—</div></div>
      <div class="field"><div class="label">Sentiment</div><div class="value" id="h-sentiment">—</div></div>
      <div class="field"><div class="label">Escalation code</div><div class="value" id="h-code">—</div></div>
      <div class="field"><div class="label">Summary</div><div class="value" id="h-summary">—</div></div>
      <div class="field"><div class="label">Entities</div><div class="value" id="h-entities">—</div></div>
      <div class="field"><div class="label">Actions taken</div><div class="value" id="h-actions">—</div></div>
    </div>
    <div id="no-handoff" class="empty">No escalation yet.</div>
  </div>
</div>
<script>
(function() {
  var events = document.getElementById('events');
  var status = document.getElementById('status');
  var handoff = document.getElementById('handoff');
  var noHandoff = document.getElementById('no-handoff');
  var firstEvent = true;

  function connect() {
    var proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    var ws = new WebSocket(proto + '//' + location.host + '/v1/console');

    ws.onopen = function() {
      status.textContent = 'Connected';
      status.className = 'status connected';
    };

    ws.onclose = function() {
      status.textContent = 'Disconnected';
      status.className = 'status';
      setTimeout(connect, 2000);
    };

    ws.onmessage = function(e) {
      try { var data = JSON.parse(e.data); } catch(_) { return; }
      var ev = data.event;
      if (!ev) return;

      if (firstEvent) {
        events.innerHTML = '';
        firstEvent = false;
      }

      var div = document.createElement('div');
      div.className = 'event ' + (ev.type || '');

      var meta = document.createElement('div');
      meta.className = 'meta';
      meta.textContent = (ev.at ? new Date(ev.at).toLocaleTimeString() + ' ' : '') +
        'seq ' + (ev.seq || 0) + ' · ' + data.call_id;
      div.appendChild(meta);

      var content = document.createElement('div');
      content.className = 'content';

      switch (ev.type) {
        case 'call_started':
          content.textContent = 'Call started' + (ev.call_id ? ' (' + ev.call_id + ')' : '');
          break;
        case 'call_ended':
          content.textContent = 'Call ended: ' + (ev.reason || 'unknown');
          break;
        case 'reply':
          content.textContent = 'Agent: ' + ev.text;
          break;
        case 'transcript':
          content.textContent = 'Caller: ' + ev.text + (ev.final ? '' : ' ...');
          break;
        case 'dtmf':
          content.textContent = 'DTMF: ' + ev.digit + (ev.sensitive ? ' (sensitive)' : '');
          break;
        case 'digits':
          content.textContent = 'Digits collected: ' + (ev.redacted ? ev.count + ' digits (redacted)' : ev.digits);
          break;
        case 'action':
          content.textContent = ev.kind + (ev.args ? ' ' + JSON.stringify(ev.args) : '');
          break;
        case 'escalation':
          content.textContent = 'ESCALATION — handing to human agent';
          showHandoff(ev.handoff);
          break;
        default:
          content.textContent = JSON.stringify(ev);
      }

      div.appendChild(content);
      events.appendChild(div);
      events.scrollTop = events.scrollHeight;
    };
  }

  function showHandoff(h) {
    if (!h) return;
    handoff.classList.add('active');
    noHandoff.style.display = 'none';
    document.getElementById('h-identity').textContent = h.identity || '—';
    document.getElementById('h-intent').textContent = h.intent || '—';
    var s = h.sentiment || '—';
    var badge = document.getElementById('h-sentiment');
    badge.textContent = s;
    badge.className = 'value badge sentiment-' + s.toLowerCase();
    document.getElementById('h-code').textContent = h.escalation_code || '—';
    document.getElementById('h-summary').textContent = h.summary || '—';
    if (h.entities && Object.keys(h.entities).length) {
      document.getElementById('h-entities').textContent =
        Object.entries(h.entities).map(function(e) { return e[0] + ': ' + e[1]; }).join(', ');
    }
    if (h.actions_taken && h.actions_taken.length) {
      document.getElementById('h-actions').textContent = h.actions_taken.join('; ');
    }
  }

  connect();
})();
</script>
</body>
</html>`
