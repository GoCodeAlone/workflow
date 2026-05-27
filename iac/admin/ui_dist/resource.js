// resource.js — drives /admin/infra-admin/resource.html?name=<NAME>.
// CSP-compliant: external file only.
//
// Endpoint:
//   POST /api/infra-admin/resources/{name} → AdminGetResourceOutput
//
// Wire format: protojson with UseProtoNames=true (snake_case fields).
// applied_config_json / outputs_json arrive as base64-encoded `bytes` per
// protojson convention. Decoded to a JSON object for display.

const API = '/api/infra-admin';

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
  return new Date(n * 1000).toISOString();
}

function decodeProtoBytes(b64) {
  if (!b64) return null;
  try {
    const raw = atob(b64);
    return JSON.parse(raw);
  } catch (_) {
    return b64; // fall back to raw if not JSON-shaped bytes
  }
}

async function postJSON(path, body) {
  const resp = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!resp.ok) throw new Error(`${path}: HTTP ${resp.status}`);
  const data = await resp.json();
  if (data && data.error) throw new Error(data.error);
  return data;
}

function renderSummary(s) {
  const tbody = document.querySelector('#summary-table tbody');
  tbody.innerHTML = '';
  if (!s) return;
  const rows = [
    ['Name', s.name],
    ['Type', s.type],
    ['Provider module', s.provider_module],
    ['Provider type', s.provider_type],
    ['Provider id', s.provider_id],
    ['Status', s.status],
    ['App context', s.app_context],
    ['Updated', fmtTs(s.updated_at_unix)],
    ['Dependencies', (s.dependencies || []).join(', ')],
  ];
  for (const [k, v] of rows) {
    const tr = document.createElement('tr');
    tr.innerHTML = `<th>${esc(k)}</th><td>${esc(v)}</td>`;
    tbody.appendChild(tr);
  }
}

function renderJSON(elId, obj) {
  const el = document.getElementById(elId);
  if (obj == null) {
    el.textContent = '(empty)';
    return;
  }
  el.textContent = typeof obj === 'string'
    ? obj
    : JSON.stringify(obj, null, 2);
}

function renderRedactionNote(redacted) {
  const note = document.getElementById('redacted-note');
  if (!redacted || redacted.length === 0) {
    note.textContent = '';
    return;
  }
  note.textContent = `Redacted output keys: ${redacted.join(', ')}`;
  note.classList.add('redacted');
}

async function load() {
  const params = new URLSearchParams(window.location.search);
  const name = params.get('name');
  if (!name) {
    showError('missing ?name= query parameter');
    return;
  }
  try {
    // POST /api/infra-admin/resources/{name} — handler reads name from URL
    // path; body carries env_name + evidence. Mirror that here.
    const body = {
      name: name,
      evidence: { authz_checked: true, authz_allowed: true },
    };
    const data = await postJSON(
      `${API}/resources/${encodeURIComponent(name)}`,
      body,
    );
    const r = data.resource || {};
    renderSummary(r.summary);
    renderJSON('applied-config', decodeProtoBytes(r.applied_config_json));
    renderJSON('outputs-json', decodeProtoBytes(r.outputs_json));
    renderRedactionNote(r.sensitive_outputs_redacted || []);
  } catch (err) {
    showError(`get resource: ${err.message}`);
  }
}

load();
