// new.js — form-builder for /admin/infra-admin/new.html.
// CSP-compliant: external file only.
//
// Endpoints:
//   POST /api/infra-admin/types           → AdminListResourceTypesOutput
//   POST /api/infra-admin/providers       → AdminListProvidersOutput
//   POST /api/infra-admin/generate-config → AdminGenerateConfigOutput
//
// Wire format: protojson with UseProtoNames=true (snake_case fields).
//
// FieldSpec.kind values handled:
//   - "string"        → text input (must carry FREEFORM_OK reason on the
//                       server side; client renders tooltip from .description)
//   - "number"        → number input with min_count/max_count as min/max
//   - "bool"          → checkbox
//   - "enum"          → select populated from .enum_values
//   - "enum_dynamic"  → select populated from .enum_source resolution:
//                         "providers" → /providers
//                         "regions"   → ProviderSummary.supported_regions
//                                       (depends_on=provider)
//                         "engines"   → ProviderSummary.supported_engines
//                                       (depends_on=provider)
//                         "sizes"     → fixed [xs, s, m, l, xl]
//   - "array_string"  → repeatable text inputs (add/remove)
//   - "array_number"  → repeatable number inputs
//   - "array_enum"    → repeatable enum selects

const API = '/api/infra-admin';
const SIZE_OPTIONS = ['xs', 's', 'm', 'l', 'xl'];

// In-memory caches populated at load time.
const STATE = {
  types: [],          // AdminResourceTypeMetadata[]
  providers: [],      // AdminProviderSummary[]
  selectedType: null, // AdminResourceTypeMetadata
};

function esc(s) {
  return String(s == null ? '' : s).replace(/[<>&"']/g, c => ({
    '<': '&lt;', '>': '&gt;', '&': '&amp;', '"': '&quot;', "'": '&#39;',
  }[c]));
}

function showError(err) {
  document.getElementById('error').textContent = err ? String(err) : '';
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

function providerByModule(name) {
  return STATE.providers.find(p => p.module_name === name);
}

// --- field rendering -------------------------------------------------------

function makeLabel(spec) {
  const lbl = document.createElement('label');
  lbl.className = 'field-label';
  lbl.setAttribute('for', `field-${spec.name}`);
  lbl.textContent = spec.label || spec.name;
  if (spec.required) lbl.textContent += ' *';
  return lbl;
}

function makeHelp(spec) {
  if (!spec.description) return null;
  const help = document.createElement('div');
  help.className = 'field-help';
  help.textContent = spec.description;
  return help;
}

function makeStringInput(spec) {
  const inp = document.createElement('input');
  inp.type = 'text';
  inp.id = `field-${spec.name}`;
  inp.name = spec.name;
  if (spec.required) inp.required = true;
  if (spec.default_value) inp.value = spec.default_value;
  if (spec.sensitive) inp.type = 'password';
  if (spec.description) inp.title = spec.description;
  return inp;
}

function makeNumberInput(spec) {
  const inp = document.createElement('input');
  inp.type = 'number';
  inp.id = `field-${spec.name}`;
  inp.name = spec.name;
  if (spec.required) inp.required = true;
  if (spec.min_count != null && spec.min_count !== 0) inp.min = spec.min_count;
  if (spec.max_count != null && spec.max_count !== 0) inp.max = spec.max_count;
  if (spec.default_value) inp.value = spec.default_value;
  return inp;
}

function makeBoolInput(spec) {
  const inp = document.createElement('input');
  inp.type = 'checkbox';
  inp.id = `field-${spec.name}`;
  inp.name = spec.name;
  if (spec.default_value === 'true') inp.checked = true;
  return inp;
}

function makeSelect(spec, options) {
  const sel = document.createElement('select');
  sel.id = `field-${spec.name}`;
  sel.name = spec.name;
  if (spec.required) sel.required = true;
  const blank = document.createElement('option');
  blank.value = '';
  blank.textContent = spec.required ? '— select —' : '(any)';
  sel.appendChild(blank);
  for (const v of options) {
    const opt = document.createElement('option');
    opt.value = String(v);
    opt.textContent = String(v);
    if (spec.default_value === String(v)) opt.selected = true;
    sel.appendChild(opt);
  }
  return sel;
}

function resolveDynamicEnum(spec, formState) {
  switch (spec.enum_source) {
    case 'providers':
      return STATE.providers.map(p => p.module_name);
    case 'sizes':
      return SIZE_OPTIONS.slice();
    case 'regions': {
      const providerMod = formState[spec.depends_on_field || 'provider'];
      const p = providerByModule(providerMod);
      return p ? (p.supported_regions || []).slice() : [];
    }
    case 'engines': {
      const providerMod = formState[spec.depends_on_field || 'provider'];
      const p = providerByModule(providerMod);
      return p ? (p.supported_engines || []).slice() : [];
    }
    case 'resource_types':
      return STATE.types.map(t => t.type);
    default:
      return [];
  }
}

function makeArrayField(spec, elementKind) {
  const wrap = document.createElement('div');
  wrap.className = 'array-field';
  wrap.dataset.name = spec.name;
  wrap.dataset.kind = elementKind;

  const rows = document.createElement('div');
  rows.className = 'array-rows';
  wrap.appendChild(rows);

  const addBtn = document.createElement('button');
  addBtn.type = 'button';
  addBtn.textContent = `+ Add ${spec.label || spec.name}`;
  addBtn.addEventListener('click', () => addArrayRow(spec, rows, elementKind));
  wrap.appendChild(addBtn);

  // Seed with min_count rows (or 1 if min_count > 0 / required).
  const seed = Math.max(spec.min_count || 0, spec.required ? 1 : 0);
  for (let i = 0; i < seed; i++) addArrayRow(spec, rows, elementKind);
  return wrap;
}

function addArrayRow(spec, rows, elementKind) {
  const row = document.createElement('div');
  row.className = 'array-row';
  let input;
  switch (elementKind) {
    case 'number':
      input = document.createElement('input');
      input.type = 'number';
      if (spec.min_count != null && spec.min_count !== 0) input.min = spec.min_count;
      if (spec.max_count != null && spec.max_count !== 0) input.max = spec.max_count;
      break;
    case 'enum':
      input = makeSelect(
        { name: `${spec.name}_item`, required: false, default_value: '' },
        spec.enum_values || [],
      );
      break;
    case 'enum_dynamic':
      input = makeSelect(
        { name: `${spec.name}_item`, required: false, default_value: '' },
        resolveDynamicEnum(spec, snapshotFormState()),
      );
      break;
    default:
      input = document.createElement('input');
      input.type = 'text';
  }
  input.dataset.arrayItem = '1';
  input.name = `${spec.name}[]`;
  const rm = document.createElement('button');
  rm.type = 'button';
  rm.textContent = '–';
  rm.addEventListener('click', () => row.remove());
  row.appendChild(input);
  row.appendChild(rm);
  rows.appendChild(row);
}

function renderField(spec) {
  const row = document.createElement('div');
  row.className = 'field-row';
  row.appendChild(makeLabel(spec));

  const right = document.createElement('div');
  let widget;
  switch (spec.kind) {
    case 'string':
      widget = makeStringInput(spec);
      break;
    case 'number':
      widget = makeNumberInput(spec);
      break;
    case 'bool':
      widget = makeBoolInput(spec);
      break;
    case 'enum':
      widget = makeSelect(spec, spec.enum_values || []);
      break;
    case 'enum_dynamic':
      widget = makeSelect(spec, resolveDynamicEnum(spec, snapshotFormState()));
      widget.dataset.enumDynamic = spec.enum_source || '';
      if (spec.depends_on_field) widget.dataset.dependsOn = spec.depends_on_field;
      break;
    case 'array_string':
      widget = makeArrayField(spec, 'string');
      break;
    case 'array_number':
      widget = makeArrayField(spec, 'number');
      break;
    case 'array_enum':
      widget = makeArrayField(spec, 'enum');
      break;
    case 'array_enum_dynamic':
      widget = makeArrayField(spec, 'enum_dynamic');
      break;
    default:
      // Unknown kind — degrade to text input with a warning tooltip.
      widget = makeStringInput(spec);
      widget.title = `unknown kind ${spec.kind}; rendered as text`;
  }
  right.appendChild(widget);
  const help = makeHelp(spec);
  if (help) right.appendChild(help);
  row.appendChild(right);
  return row;
}

// --- form lifecycle --------------------------------------------------------

function snapshotFormState() {
  const out = {};
  const fields = document.getElementById('fields');
  if (!fields) return out;
  for (const el of fields.querySelectorAll('input, select')) {
    if (el.dataset.arrayItem === '1') continue;
    if (el.type === 'checkbox') {
      out[el.name] = el.checked ? 'true' : 'false';
    } else if (el.name) {
      out[el.name] = el.value;
    }
  }
  return out;
}

function refreshDependentDynamics() {
  // Recompute enum_dynamic selects whose depends_on field changed.
  const state = snapshotFormState();
  document.querySelectorAll('select[data-enum-dynamic]').forEach(sel => {
    const src = sel.dataset.enumDynamic;
    const deps = sel.dataset.dependsOn;
    if (!deps) return; // independent dynamic; populated at render
    const spec = {
      name: sel.name,
      enum_source: src,
      depends_on_field: deps,
      required: sel.required,
    };
    const options = resolveDynamicEnum(spec, state);
    const prev = sel.value;
    // Repopulate while preserving placeholder.
    while (sel.options.length > 1) sel.remove(1);
    for (const v of options) {
      const opt = document.createElement('option');
      opt.value = String(v);
      opt.textContent = String(v);
      sel.appendChild(opt);
    }
    if (options.includes(prev)) sel.value = prev;
  });
}

function renderType(typeMeta) {
  STATE.selectedType = typeMeta;
  const wrap = document.getElementById('fields');
  wrap.innerHTML = '';
  if (!typeMeta) return;
  for (const f of (typeMeta.fields || [])) {
    wrap.appendChild(renderField(f));
  }
  // Wire up dependency refresh on every input change.
  wrap.addEventListener('change', refreshDependentDynamics);
}

function readSubmittedFieldValues() {
  const out = {};
  const fields = document.getElementById('fields');
  for (const el of fields.querySelectorAll('input, select')) {
    if (!el.name) continue;
    if (el.type === 'checkbox') {
      out[el.name] = el.checked ? 'true' : 'false';
      continue;
    }
    if (el.name.endsWith('[]')) {
      const key = el.name.slice(0, -2);
      const cur = out[key];
      const val = el.value;
      if (val === '' || val == null) continue;
      out[key] = cur ? `${cur},${val}` : val;
      continue;
    }
    if (el.value !== '' && el.value != null) out[el.name] = el.value;
  }
  return out;
}

async function loadCatalog() {
  showError('');
  try {
    const [typesResp, provResp] = await Promise.all([
      postJSON(`${API}/types`, {
        evidence: { authz_checked: true, authz_allowed: true },
      }),
      postJSON(`${API}/providers`, {
        evidence: { authz_checked: true, authz_allowed: true },
      }),
    ]);
    STATE.types = typesResp.types || [];
    STATE.providers = provResp.providers || [];

    const sel = document.getElementById('type');
    while (sel.options.length > 1) sel.remove(1);
    for (const t of STATE.types) {
      const opt = document.createElement('option');
      opt.value = t.type;
      opt.textContent = t.description ? `${t.type} — ${t.description}` : t.type;
      sel.appendChild(opt);
    }
  } catch (err) {
    showError(`load catalog: ${err.message}`);
  }
}

async function onSubmit(ev) {
  ev.preventDefault();
  showError('');
  const errBox = document.getElementById('validation-errors');
  errBox.innerHTML = '';
  const out = document.getElementById('yaml-output');
  out.textContent = '';
  document.getElementById('copy').disabled = true;

  const typeName = document.getElementById('type').value;
  const resourceName = document.getElementById('name').value.trim();
  if (!typeName || !resourceName) {
    showError('type and name are required');
    return;
  }
  const fieldValues = readSubmittedFieldValues();
  // provider_module is taken from the `provider` field if present;
  // catalog convention assigns enum_source=providers to a field named
  // `provider`, whose value is the module name.
  const providerModule = fieldValues.provider || '';

  try {
    const resp = await postJSON(`${API}/generate-config`, {
      resource_type: typeName,
      resource_name: resourceName,
      provider_module: providerModule,
      field_values: fieldValues,
      evidence: { authz_checked: true, authz_allowed: true },
    });
    if (resp.validation_errors && resp.validation_errors.length > 0) {
      for (const e of resp.validation_errors) {
        const li = document.createElement('li');
        li.textContent = e;
        errBox.appendChild(li);
      }
    }
    out.textContent = resp.yaml_snippet || '';
    document.getElementById('copy').disabled = !resp.yaml_snippet;
  } catch (err) {
    showError(`generate-config: ${err.message}`);
  }
}

function onCopy() {
  const out = document.getElementById('yaml-output');
  if (!out.textContent) return;
  navigator.clipboard.writeText(out.textContent).catch(err => {
    showError(`copy: ${err.message}`);
  });
}

document.getElementById('type').addEventListener('change', ev => {
  const tm = STATE.types.find(t => t.type === ev.target.value);
  renderType(tm || null);
});
document.getElementById('new-resource-form').addEventListener('submit', onSubmit);
document.getElementById('copy').addEventListener('click', onCopy);

loadCatalog();
