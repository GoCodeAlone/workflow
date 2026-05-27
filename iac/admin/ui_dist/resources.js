// resources.js — drives /admin/infra-admin/resources.html.
// CSP-compliant: external file only, no inline scripts/handlers.
//
// Wire format: protojson with UseProtoNames=true on the handler side.
// Field names match workflow/iac/admin/proto/infra_admin.proto snake_case.
//
// Endpoints:
//   POST /api/infra-admin/resources       → AdminListResourcesOutput
//   POST /api/infra-admin/providers       → AdminListProvidersOutput (populates filter dropdown)

const API = '/api/infra-admin';
const DETAIL_PATH = '/admin/infra-admin/resource.html';

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

async function loadProviders() {
  try {
    const data = await postJSON(`${API}/providers`, {
      evidence: { authz_checked: true, authz_allowed: true },
    });
    const sel = document.getElementById('filter-provider');
    // Preserve "all providers" first option.
    while (sel.options.length > 1) sel.remove(1);
    for (const p of (data.providers || [])) {
      const opt = document.createElement('option');
      opt.value = p.module_name;
      opt.textContent = `${p.module_name} (${p.provider_type || '?'})`;
      sel.appendChild(opt);
    }
  } catch (err) {
    showError(`provider list: ${err.message}`);
  }
}

function renderTable(rows) {
  const tbody = document.querySelector('#resources tbody');
  tbody.innerHTML = '';
  if (rows.length === 0) {
    const tr = document.createElement('tr');
    tr.innerHTML = '<td colspan="6"><em>No resources match the current filters.</em></td>';
    tbody.appendChild(tr);
    return;
  }
  for (const r of rows) {
    const tr = document.createElement('tr');
    const href = `${DETAIL_PATH}?name=${encodeURIComponent(r.name || '')}`;
    tr.innerHTML = `
      <td>${esc(r.name)}</td>
      <td>${esc(r.type)}</td>
      <td>${esc(r.provider_module)}${r.provider_type ? ' / ' + esc(r.provider_type) : ''}</td>
      <td>${esc(r.status)}</td>
      <td>${esc(fmtTs(r.updated_at_unix))}</td>
      <td><a href="${esc(href)}">Detail</a></td>`;
    tbody.appendChild(tr);
  }
}

async function fetchResources() {
  showError('');
  const body = {
    type_filter: document.getElementById('filter-type').value.trim(),
    provider_filter: document.getElementById('filter-provider').value,
    app_context_filter: document.getElementById('filter-app-context').value.trim(),
    evidence: { authz_checked: true, authz_allowed: true },
  };
  try {
    const data = await postJSON(`${API}/resources`, body);
    renderTable(data.resources || []);
  } catch (err) {
    showError(`list resources: ${err.message}`);
    renderTable([]);
  }
}

document.getElementById('refresh').addEventListener('click', fetchResources);
document.getElementById('filter-type').addEventListener('change', fetchResources);
document.getElementById('filter-provider').addEventListener('change', fetchResources);
document.getElementById('filter-app-context').addEventListener('change', fetchResources);

(async () => {
  await loadProviders();
  await fetchResources();
})();
