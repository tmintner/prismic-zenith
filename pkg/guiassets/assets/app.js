// Zenith GUI - Frontend Logic

let metricsInterval = null;

document.addEventListener('DOMContentLoaded', () => {
  initGreeting();
  startMetricsLoop();
  setupChat();
});

// --- Greeting ---
async function initGreeting() {
  try {
    const info = await getGreeting();
    document.getElementById('greeting').textContent =
      `${info.hostname} | ${info.os}/${info.arch}`;
  } catch (e) {
    document.getElementById('greeting').textContent = 'System info unavailable';
  }
}

// --- Metrics ---
function startMetricsLoop() {
  refreshMetrics();
  metricsInterval = setInterval(refreshMetrics, 10000);
}

async function refreshMetrics() {
  const dot = document.querySelector('.status .dot');
  try {
    const m = await getSystemMetrics();

    dot.classList.toggle('online', !m.error);

    if (m.error) {
      document.getElementById('cpu-value').textContent = '--';
      document.getElementById('mem-value').textContent = '--';
      setGaugeArc('cpu-arc', 0);
      setGaugeArc('mem-arc', 0);
      document.getElementById('top-cpu-body').innerHTML =
        '<tr><td colspan="2" class="no-data">No data</td></tr>';
      document.getElementById('top-mem-body').innerHTML =
        '<tr><td colspan="2" class="no-data">No data</td></tr>';
      return;
    }

    // CPU gauge
    const cpuPct = parseFloat(m.cpu_pct) || 0;
    document.getElementById('cpu-value').textContent = cpuPct.toFixed(1) + '%';
    setGaugeArc('cpu-arc', cpuPct);

    // Memory gauge
    const memUsed = parseFloat(m.mem_used_mb) || 0;
    const memFree = parseFloat(m.mem_free_mb) || 0;
    const memTotal = memUsed + memFree;
    const memPct = memTotal > 0 ? (memUsed / memTotal) * 100 : 0;
    document.getElementById('mem-value').textContent = (memUsed / 1024).toFixed(1) + ' GB';
    setGaugeArc('mem-arc', memPct);

    // Top CPU processes
    renderProcessTable('top-cpu-body', m.top_cpu, 'cpu');
    // Top Memory processes
    renderProcessTable('top-mem-body', m.top_mem, 'mem');

  } catch (e) {
    dot.classList.remove('online');
  }
}

function setGaugeArc(id, pct) {
  const el = document.getElementById(id);
  if (!el) return;
  const circumference = 263.9;
  const offset = circumference - (circumference * Math.min(pct, 100)) / 100;
  el.style.strokeDashoffset = offset;
  el.classList.remove('warn', 'crit');
  if (pct > 85) el.classList.add('crit');
  else if (pct > 65) el.classList.add('warn');
}

function renderProcessTable(tbodyId, processes, type) {
  const tbody = document.getElementById(tbodyId);
  if (!processes || processes.length === 0) {
    tbody.innerHTML = '<tr><td colspan="2" class="no-data">No data</td></tr>';
    return;
  }
  tbody.innerHTML = processes.map(p => {
    const val = type === 'cpu'
      ? parseFloat(p.value).toFixed(1) + '%'
      : (parseFloat(p.value) / (1024 * 1024)).toFixed(0) + ' MB';
    return `<tr><td title="${escapeHtml(p.name)}">${escapeHtml(p.name)}</td><td>${val}</td></tr>`;
  }).join('');
}

// --- Chat ---
function setupChat() {
  const input = document.getElementById('chat-input');
  const sendBtn = document.getElementById('btn-send');
  const recBtn = document.getElementById('btn-recommend');

  input.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleAsk();
    }
  });
  sendBtn.addEventListener('click', handleAsk);
  recBtn.addEventListener('click', handleRecommend);
}

async function handleAsk() {
  const input = document.getElementById('chat-input');
  const query = input.value.trim();
  if (!query) return;

  input.value = '';
  appendMessage('user', query);
  const loadingEl = appendLoading();
  setInputDisabled(true);

  try {
    const resp = await askQuestion(query);
    removeElement(loadingEl);
    if (resp.error) {
      appendMessage('error', resp.error);
    } else {
      appendAssistant(resp.answer, resp.interaction_id);
    }
  } catch (e) {
    removeElement(loadingEl);
    appendMessage('error', 'Failed to reach server: ' + e.message);
  }
  setInputDisabled(false);
  document.getElementById('chat-input').focus();
}

async function handleRecommend() {
  appendMessage('user', 'Get system recommendations');
  const loadingEl = appendLoading();
  setInputDisabled(true);

  try {
    const resp = await getRecommendations();
    removeElement(loadingEl);
    if (resp.error) {
      appendMessage('error', resp.error);
    } else {
      appendAssistant(resp.answer, resp.interaction_id);
    }
  } catch (e) {
    removeElement(loadingEl);
    appendMessage('error', 'Failed to reach server: ' + e.message);
  }
  setInputDisabled(false);
}

function setInputDisabled(disabled) {
  document.getElementById('chat-input').disabled = disabled;
  document.getElementById('btn-send').disabled = disabled;
  document.getElementById('btn-recommend').disabled = disabled;
}

// --- DOM Helpers ---
function appendMessage(type, text) {
  const container = document.getElementById('chat-messages');
  // Remove welcome message if present
  const welcome = container.querySelector('.welcome');
  if (welcome) welcome.remove();

  const div = document.createElement('div');
  div.className = 'msg ' + type;
  div.textContent = text;
  container.appendChild(div);
  container.scrollTop = container.scrollHeight;
  return div;
}

function appendAssistant(text, interactionId) {
  const container = document.getElementById('chat-messages');
  const div = document.createElement('div');
  div.className = 'msg assistant';
  div.innerHTML = formatMarkdown(text);

  if (interactionId) {
    const meta = document.createElement('div');
    meta.className = 'msg-meta';
    meta.innerHTML = `
      <span>ID: ${interactionId}</span>
      <button onclick="handleFeedback(${interactionId}, 1, this)">&#128077;</button>
      <button onclick="handleFeedback(${interactionId}, -1, this)">&#128078;</button>
    `;
    div.appendChild(meta);
  }

  container.appendChild(div);
  container.scrollTop = container.scrollHeight;
  return div;
}

function appendLoading() {
  const container = document.getElementById('chat-messages');
  const div = document.createElement('div');
  div.className = 'msg loading';
  div.innerHTML = 'Thinking<span class="dots"></span>';
  container.appendChild(div);
  container.scrollTop = container.scrollHeight;
  return div;
}

function removeElement(el) {
  if (el && el.parentNode) el.parentNode.removeChild(el);
}

async function handleFeedback(id, val, btn) {
  const meta = btn.parentElement;
  const buttons = meta.querySelectorAll('button');
  buttons.forEach(b => b.classList.remove('selected'));
  btn.classList.add('selected');

  try {
    await sendFeedback(id, val);
    const span = meta.querySelector('span');
    span.textContent = `ID: ${id} - Feedback sent`;
  } catch (e) {
    // silent fail
  }
}

// --- Markdown-ish formatting ---
function formatMarkdown(text) {
  if (!text) return '';
  // Escape HTML first
  let html = escapeHtml(text);
  // Code blocks
  html = html.replace(/```([\s\S]*?)```/g, '<pre>$1</pre>');
  // Inline code
  html = html.replace(/`([^`]+)`/g, '<code>$1</code>');
  // Bold
  html = html.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
  // Headers
  html = html.replace(/^### (.+)$/gm, '<p><strong>$1</strong></p>');
  html = html.replace(/^## (.+)$/gm, '<p><strong>$1</strong></p>');
  html = html.replace(/^# (.+)$/gm, '<p><strong>$1</strong></p>');
  // List items
  html = html.replace(/^[*-] (.+)$/gm, '<li>$1</li>');
  html = html.replace(/(<li>.*<\/li>)/s, '<ul>$1</ul>');
  // Paragraphs (double newline)
  html = html.replace(/\n\n/g, '</p><p>');
  // Single newlines
  html = html.replace(/\n/g, '<br>');
  return '<p>' + html + '</p>';
}

function escapeHtml(text) {
  const div = document.createElement('div');
  div.textContent = text;
  return div.innerHTML;
}
