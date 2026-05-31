// actions.js — drives /admin/infra-admin/actions.html.
// CSP-compliant: external file only.
//
// Endpoint:
//   GET /api/infra-admin/audit?limit=N&since=<unix> → ndjson of AdminAuditEntry
//
// Wire format: ndjson — one protojson-encoded AdminAuditEntry per line.
// AdminAuditEntry fields (snake_case): schema_version, ts_unix, subject,
// action, targets[], result, app_context.
//
// Security: Authorization: Bearer <token> on every fetch.
// Filters (client-side after fetch): action, result.
// Auto-refresh: 30-second interval, toggle by checkbox.

const API = '/api/infra-admin';
const TOKEN_KEY = 'infra_admin_bearer';
const REFRESH_INTERVAL_MS = 30_000;

let autoRefreshTimer = null;

// --- helpers ---------------------------------------------------------------

function esc(s) {
  return String(s == null ? '' : s).replace(/[<>&"']/g, c => ({
    '<': '&lt;', '>': '&gt;', '&': '&amp;', '"': '&quot;', "'": '&#39;',
  }[c]));
}

function showError(err) {
  document.getElementById('error').textContent = err ? String(err) : '';
}

function fmtTs(unix) {
  if (!unix || unix === '0') return '';
  const n = typeof unix === 'string' ? parseInt(unix, 10) : unix;
  if (!Number.isFinite(n) || n === 0) return '';
  return new Date(n * 1000).toISOString().replace('T', ' ').replace(/\.\d+Z$/, ' Z');
}

// bearer returns the token, persisting any freshly-entered value.
function bearer() {
  const inp = document.getElementById('bearer-token');
  if (inp.value) {
    sessionStorage.setItem(TOKEN_KEY, inp.value);
  } else {
    const stored = sessionStorage.getItem(TOKEN_KEY);
    if (stored) inp.value = stored;
  }
  return inp.value;
}

// parseNdjson splits a text body into non-empty lines and JSON-parses each.
// Lines that fail to parse are silently skipped (partial writes in the log).
function parseNdjson(text) {
  const entries = [];
  for (const line of text.split('\n')) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    try {
      entries.push(JSON.parse(trimmed));
    } catch (_) {
      // skip malformed lines (partial writes mid-rotation)
    }
  }
  return entries;
}

// resultClass maps audit result values to CSS classes for styling.
function resultClass(result) {
  if (result === 'ok') return 'audit-ok';
  if (result === 'denied') return 'audit-denied';
  if (result === 'error') return 'audit-error';
  return '';
}

// --- render ----------------------------------------------------------------

function renderEntries(entries) {
  const tbody = document.querySelector('#audit-table tbody');
  tbody.innerHTML = '';

  const filterAction = document.getElementById('filter-action').value;
  const filterResult = document.getElementById('filter-result').value;

  // Apply client-side filters (entries already limited server-side by ?limit=).
  const filtered = entries.filter(e => {
    if (filterAction && e.action !== filterAction) return false;
    if (filterResult && e.result !== filterResult) return false;
    return true;
  });

  document.getElementById('empty-note').hidden = filtered.length > 0;

  for (const e of filtered) {
    const tr = document.createElement('tr');
    const cls = resultClass(e.result);
    tr.innerHTML = [
      `<td>${esc(fmtTs(e.ts_unix))}</td>`,
      `<td>${esc(e.subject)}</td>`,
      `<td>${esc(e.action)}</td>`,
      `<td>${esc((e.targets || []).join(', '))}</td>`,
      `<td${cls ? ` class="${cls}"` : ''}><strong>${esc(e.result)}</strong></td>`,
      `<td>${esc(e.app_context)}</td>`,
    ].join('');
    tbody.appendChild(tr);
  }
}

// --- fetch -----------------------------------------------------------------

async function fetchAudit() {
  const tok = bearer();
  if (!tok) {
    showError('bearer token required — paste JWT into the token field above');
    return;
  }

  const limit = document.getElementById('filter-limit').value || '50';
  const url = `${API}/audit${limit !== '0' ? `?limit=${encodeURIComponent(limit)}` : ''}`;

  try {
    const resp = await fetch(url, {
      headers: { 'Authorization': `Bearer ${tok}` },
    });
    if (!resp.ok) {
      showError(`audit: HTTP ${resp.status}`);
      return;
    }
    const text = await resp.text();
    showError('');
    const entries = parseNdjson(text);
    renderEntries(entries);
  } catch (err) {
    showError(`audit: ${err.message}`);
  }
}

// --- auto-refresh ----------------------------------------------------------

function startAutoRefresh() {
  stopAutoRefresh();
  autoRefreshTimer = setInterval(fetchAudit, REFRESH_INTERVAL_MS);
}

function stopAutoRefresh() {
  if (autoRefreshTimer !== null) {
    clearInterval(autoRefreshTimer);
    autoRefreshTimer = null;
  }
}

// --- wire events -----------------------------------------------------------

document.getElementById('btn-refresh').addEventListener('click', fetchAudit);

document.getElementById('auto-refresh').addEventListener('change', function () {
  if (this.checked) {
    startAutoRefresh();
  } else {
    stopAutoRefresh();
  }
});

// Re-filter the already-fetched entries when filter selects change.
// (Avoids a round-trip for a client-side filter change.)
let lastEntries = [];
const origFetch = fetchAudit;

// Wrap fetchAudit to cache entries for re-filtering.
async function fetchAndCache() {
  const tok = bearer();
  if (!tok) {
    showError('bearer token required — paste JWT into the token field above');
    return;
  }

  const limit = document.getElementById('filter-limit').value || '50';
  const url = `${API}/audit${limit !== '0' ? `?limit=${encodeURIComponent(limit)}` : ''}`;

  try {
    const resp = await fetch(url, {
      headers: { 'Authorization': `Bearer ${tok}` },
    });
    if (!resp.ok) {
      showError(`audit: HTTP ${resp.status}`);
      return;
    }
    const text = await resp.text();
    showError('');
    lastEntries = parseNdjson(text);
    renderEntries(lastEntries);
  } catch (err) {
    showError(`audit: ${err.message}`);
  }
}

// Replace button listener with caching version.
document.getElementById('btn-refresh').removeEventListener('click', fetchAudit);
document.getElementById('btn-refresh').addEventListener('click', fetchAndCache);

document.getElementById('filter-action').addEventListener('change', () => renderEntries(lastEntries));
document.getElementById('filter-result').addEventListener('change', () => renderEntries(lastEntries));
// limit change needs a new fetch (different server-side slice).
document.getElementById('filter-limit').addEventListener('change', fetchAndCache);

// Restore stored token on load.
const storedTok = sessionStorage.getItem(TOKEN_KEY);
if (storedTok) document.getElementById('bearer-token').value = storedTok;

// Initial load.
fetchAndCache();
