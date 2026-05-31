// resource.js — drives /admin/infra-admin/resource.html?name=<NAME>.
// CSP-compliant: external file only.
//
// Endpoints (read):
//   POST /api/infra-admin/resources/{name} → AdminGetResourceOutput
//
// Endpoints (v1.1 mutation — bearer required):
//   POST /api/infra-admin/plan    → AdminPlanOutput
//   POST /api/infra-admin/apply   → AdminApplyOutput
//   POST /api/infra-admin/destroy → AdminDestroyOutput
//   POST /api/infra-admin/drift   → AdminDriftOutput
//
// Wire format: protojson with UseProtoNames=true (snake_case fields).
// applied_config_json / outputs_json arrive as base64-encoded `bytes`
// per protojson convention. Decoded to a JSON object for display.
//
// Mutation security:
//   All mutation fetches send Authorization: Bearer <token>.
//   allow_replace selections come from checkboxes rendered against
//   plan action_type=replace rows (selectable, not free-text).

const API = '/api/infra-admin';
const TOKEN_KEY = 'infra_admin_bearer';

// In-flight plan state held between Plan and Apply.
const PLAN_STATE = {
  planId: '',
  desiredHash: '',
  actions: [],
};

// Current resource state populated at load.
const RESOURCE_STATE = {
  name: '',
  appContext: '',
  type: '',
};

// --- helpers ---------------------------------------------------------------

function esc(s) {
  return String(s == null ? '' : s).replace(/[<>&"']/g, c => ({
    '<': '&lt;', '>': '&gt;', '&': '&amp;', '"': '&quot;', "'": '&#39;',
  }[c]));
}

function showError(err) {
  document.getElementById('error').textContent = err ? String(err) : '';
}

function showMutationError(err) {
  document.getElementById('mutation-error').textContent = err ? String(err) : '';
  document.getElementById('mutation-ok').textContent = '';
}

function showMutationOk(msg) {
  document.getElementById('mutation-ok').textContent = msg || '';
  document.getElementById('mutation-error').textContent = '';
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

// bearer returns the current token, saving any new value from the input.
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

// postMutation wraps postJSON and adds the Authorization: Bearer header.
// Throws if no bearer token is configured.
async function postMutation(path, body) {
  const tok = bearer();
  if (!tok) throw new Error('bearer token required — paste JWT into the token field above');
  const resp = await fetch(path, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${tok}`,
    },
    body: JSON.stringify(body),
  });
  if (!resp.ok) throw new Error(`${path}: HTTP ${resp.status}`);
  const data = await resp.json();
  if (data && data.error) throw new Error(data.error);
  return data;
}

// --- render helpers --------------------------------------------------------

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

// renderPlan renders the plan action table and wires the apply confirm
// checkbox → apply button. allow_replace checkboxes are rendered only
// for action_type=replace rows (selectable from the plan, not free-text).
function renderPlan(out) {
  PLAN_STATE.planId = out.plan_id || '';
  PLAN_STATE.desiredHash = out.desired_hash || '';
  PLAN_STATE.actions = out.actions || [];

  document.getElementById('plan-meta').textContent =
    `plan_id=${PLAN_STATE.planId}  desired_hash=${PLAN_STATE.desiredHash}`;

  const tbody = document.querySelector('#plan-actions-table tbody');
  tbody.innerHTML = '';

  for (const a of PLAN_STATE.actions) {
    const isReplace = a.action_type === 'replace';
    const tr = document.createElement('tr');
    tr.innerHTML = [
      `<td>${esc(a.action_type)}</td>`,
      `<td>${esc(a.resource_name)}</td>`,
      `<td>${esc(a.type)}</td>`,
      `<td>${esc(a.change_summary)}</td>`,
      `<td>${isReplace
        ? `<label><input type="checkbox" class="allow-replace-cb" value="${esc(a.resource_name)}"> allow replace</label>`
        : ''}</td>`,
    ].join('');
    tbody.appendChild(tr);
  }

  document.getElementById('plan-result').hidden = false;
  document.getElementById('apply-confirm').checked = false;
  document.getElementById('btn-apply').disabled = true;

  document.getElementById('drift-result').hidden = true;
  showMutationError('');
  showMutationOk('');
}

function renderDrift(drift) {
  const tbody = document.querySelector('#drift-table tbody');
  tbody.innerHTML = '';

  for (const d of (drift || [])) {
    const tr = document.createElement('tr');
    tr.innerHTML = [
      `<td>${esc(d.resource_name)}</td>`,
      `<td>${esc(d.type)}</td>`,
      `<td>${d.drifted ? '<strong>yes</strong>' : 'no'}</td>`,
      `<td>${esc(d.class)}</td>`,
      `<td>${esc((d.fields || []).join(', '))}</td>`,
    ].join('');
    tbody.appendChild(tr);
  }

  document.getElementById('drift-result').hidden = false;
  document.getElementById('plan-result').hidden = true;
  showMutationError('');
}

// --- load (read) -----------------------------------------------------------

async function load() {
  const params = new URLSearchParams(window.location.search);
  const name = params.get('name');
  if (!name) {
    showError('missing ?name= query parameter');
    return;
  }
  RESOURCE_STATE.name = name;

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
    const s = r.summary || {};
    RESOURCE_STATE.appContext = s.app_context || '';
    RESOURCE_STATE.type = s.type || '';

    renderSummary(s);
    renderJSON('applied-config', decodeProtoBytes(r.applied_config_json));
    renderJSON('outputs-json', decodeProtoBytes(r.outputs_json));
    renderRedactionNote(r.sensitive_outputs_redacted || []);
  } catch (err) {
    showError(`get resource: ${err.message}`);
  }

  // Restore stored token into the input field.
  const stored = sessionStorage.getItem(TOKEN_KEY);
  if (stored) document.getElementById('bearer-token').value = stored;
}

// --- mutation handlers -----------------------------------------------------

async function handlePlan() {
  showMutationError('');
  showMutationOk('');
  try {
    const data = await postMutation(`${API}/plan`, {
      app_context: RESOURCE_STATE.appContext,
      resource_filter: RESOURCE_STATE.name,
      evidence: { authz_checked: true, authz_allowed: true },
    });
    renderPlan(data);
    if ((data.actions || []).length === 0) {
      showMutationOk('No changes — resource is up to date.');
    }
  } catch (err) {
    showMutationError(`plan: ${err.message}`);
  }
}

async function handleApply() {
  showMutationError('');
  showMutationOk('');
  if (!PLAN_STATE.planId || !PLAN_STATE.desiredHash) {
    showMutationError('run Plan first');
    return;
  }

  // Collect allow_replace from checked checkboxes (selectable from plan actions).
  const allowReplace = Array.from(
    document.querySelectorAll('.allow-replace-cb:checked'),
  ).map(cb => cb.value);

  try {
    const data = await postMutation(`${API}/apply`, {
      plan_id: PLAN_STATE.planId,
      desired_hash: PLAN_STATE.desiredHash,
      allow_replace: allowReplace,
      app_context: RESOURCE_STATE.appContext,
      evidence: { authz_checked: true, authz_allowed: true },
    });
    const applied = (data.applied || []).map(r => r.name).join(', ');
    const errors = (data.errors || []).map(e => `${e.resource}: ${e.error}`).join('; ');
    showMutationOk(`Applied: ${applied || '(none)'}`);
    if (errors) showMutationError(`Errors: ${errors}`);
    document.getElementById('plan-result').hidden = true;
    PLAN_STATE.planId = '';
    PLAN_STATE.desiredHash = '';
  } catch (err) {
    showMutationError(`apply: ${err.message}`);
  }
}

async function handleDestroy() {
  showMutationError('');
  showMutationOk('');
  try {
    const data = await postMutation(`${API}/destroy`, {
      refs: [{ name: RESOURCE_STATE.name, type: RESOURCE_STATE.type }],
      confirm_hash: PLAN_STATE.desiredHash || '',
      evidence: { authz_checked: true, authz_allowed: true },
    });
    const destroyed = (data.destroyed || []).join(', ');
    const errors = (data.errors || []).map(e => `${e.resource}: ${e.error}`).join('; ');
    showMutationOk(`Destroyed: ${destroyed || '(none)'}`);
    if (errors) showMutationError(`Errors: ${errors}`);
  } catch (err) {
    showMutationError(`destroy: ${err.message}`);
  }
}

async function handleDrift() {
  showMutationError('');
  showMutationOk('');
  try {
    const data = await postMutation(`${API}/drift`, {
      refs: [{ name: RESOURCE_STATE.name, type: RESOURCE_STATE.type }],
      evidence: { authz_checked: true, authz_allowed: true },
    });
    renderDrift(data.drift || []);
    const anyDrift = (data.drift || []).some(d => d.drifted);
    showMutationOk(anyDrift ? 'Drift detected — see table below.' : 'No drift detected.');
  } catch (err) {
    showMutationError(`drift: ${err.message}`);
  }
}

// --- wire events -----------------------------------------------------------

document.getElementById('btn-plan').addEventListener('click', handlePlan);
document.getElementById('btn-drift').addEventListener('click', handleDrift);

// Apply confirm checkbox gates the Apply button.
document.getElementById('apply-confirm').addEventListener('change', function () {
  document.getElementById('btn-apply').disabled = !this.checked;
});

document.getElementById('btn-apply').addEventListener('click', handleApply);

// Destroy confirm checkbox gates the Destroy button.
document.getElementById('destroy-confirm').addEventListener('change', function () {
  document.getElementById('btn-destroy').disabled = !this.checked;
});

document.getElementById('btn-destroy').addEventListener('click', handleDestroy);

// Persist token on change.
document.getElementById('bearer-token').addEventListener('change', function () {
  if (this.value) sessionStorage.setItem(TOKEN_KEY, this.value);
});

load();
