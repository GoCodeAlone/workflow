import { api } from './api.js';
import {
  escapeHtml, showToast, showModal, hideModal, renderBadge,
  renderSpinner, requireAuth
} from './components.js';

// ==================== Affiliates ====================

function renderAffiliates() {
  if (!requireAuth()) return '';
  return `
    <div class="page">
      <div class="page-header">
        <h1 class="page-title">Affiliates</h1>
        <button class="btn btn-primary" id="create-affiliate-btn">Create Affiliate</button>
      </div>
      <div class="table-container">
        <table class="data-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Name</th>
              <th>Region</th>
              <th>Retention (days)</th>
              <th>Status</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody id="affiliates-tbody">
            <tr><td colspan="6">${renderSpinner()}</td></tr>
          </tbody>
        </table>
      </div>
    </div>
  `;
}

function handleAffiliates() {
  loadAffiliates();
  document.getElementById('create-affiliate-btn')?.addEventListener('click', () => showAffiliateModal());
}

async function loadAffiliates() {
  try {
    const result = await api.get('/api/affiliates');
    const affiliates = result.data || result || [];
    const tbody = document.getElementById('affiliates-tbody');
    if (!tbody) return;

    if (affiliates.length === 0) {
      tbody.innerHTML = '<tr><td colspan="6" class="text-center text-muted">No affiliates found</td></tr>';
      return;
    }

    tbody.innerHTML = affiliates.map(a => {
      const d = a.data || a;
      return `
        <tr>
          <td><code style="font-size:0.8rem">${escapeHtml(a.id)}</code></td>
          <td>${escapeHtml(d.name)}</td>
          <td>${escapeHtml(d.region || 'N/A')}</td>
          <td>${d.dataRetentionDays || 'N/A'}</td>
          <td>${renderBadge(d.status || a.state || 'active', d.status === 'active' ? 'success' : 'neutral')}</td>
          <td><button class="btn btn-sm btn-ghost edit-affiliate-btn" data-id="${escapeHtml(a.id)}">Edit</button></td>
        </tr>
      `;
    }).join('');

    tbody.querySelectorAll('.edit-affiliate-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        const aff = affiliates.find(a => a.id === btn.dataset.id);
        if (aff) showAffiliateModal(aff);
      });
    });
  } catch (err) {
    showToast('Failed to load affiliates: ' + err.message, 'error');
  }
}

function showAffiliateModal(existing) {
  const d = existing ? (existing.data || existing) : {};
  const isEdit = !!existing;

  showModal(isEdit ? 'Edit Affiliate' : 'Create Affiliate', `
    <div class="form-group">
      <label for="aff-name">Name</label>
      <input class="form-input" id="aff-name" value="${escapeHtml(d.name || '')}" placeholder="Organization name">
    </div>
    <div class="form-row">
      <div class="form-group">
        <label for="aff-region">Region</label>
        <input class="form-input" id="aff-region" value="${escapeHtml(d.region || '')}" placeholder="US-East">
      </div>
      <div class="form-group">
        <label for="aff-retention">Retention Days</label>
        <input type="number" class="form-input" id="aff-retention" value="${d.dataRetentionDays || 365}">
      </div>
    </div>
    <div class="form-group">
      <label for="aff-email">Contact Email</label>
      <input type="email" class="form-input" id="aff-email" value="${escapeHtml(d.contactEmail || '')}" placeholder="admin@org.com">
    </div>
  `, [
    { id: 'modal-cancel-btn', label: 'Cancel', class: 'btn-secondary' },
    { id: 'aff-save-btn', label: isEdit ? 'Update' : 'Create', class: 'btn-primary' }
  ]);

  document.getElementById('aff-save-btn').addEventListener('click', async () => {
    const body = {
      name: document.getElementById('aff-name').value,
      region: document.getElementById('aff-region').value,
      dataRetentionDays: parseInt(document.getElementById('aff-retention').value) || 365,
      contactEmail: document.getElementById('aff-email').value,
      status: 'active'
    };
    try {
      if (isEdit) {
        await api.put(`/api/affiliates/${existing.id}`, body);
        showToast('Affiliate updated', 'success');
      } else {
        await api.post('/api/affiliates', body);
        showToast('Affiliate created', 'success');
      }
      hideModal();
      loadAffiliates();
    } catch (err) {
      showToast(err.message, 'error');
    }
  });
}

// ==================== Programs ====================

function renderPrograms() {
  if (!requireAuth()) return '';
  return `
    <div class="page">
      <div class="page-header">
        <h1 class="page-title">Programs</h1>
        <button class="btn btn-primary" id="create-program-btn">Create Program</button>
      </div>
      <div class="table-container">
        <table class="data-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Name</th>
              <th>Affiliate</th>
              <th>Providers</th>
              <th>Short Code</th>
              <th>Status</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody id="programs-tbody">
            <tr><td colspan="7">${renderSpinner()}</td></tr>
          </tbody>
        </table>
      </div>
    </div>
  `;
}

function handlePrograms() {
  loadPrograms();
  document.getElementById('create-program-btn')?.addEventListener('click', () => showProgramModal());
}

async function loadPrograms() {
  try {
    const result = await api.get('/api/programs');
    const programs = result.data || result || [];
    const tbody = document.getElementById('programs-tbody');
    if (!tbody) return;

    if (programs.length === 0) {
      tbody.innerHTML = '<tr><td colspan="7" class="text-center text-muted">No programs found</td></tr>';
      return;
    }

    tbody.innerHTML = programs.map(p => {
      const d = p.data || p;
      const providers = (d.providers || []).join(', ');
      return `
        <tr>
          <td><code style="font-size:0.8rem">${escapeHtml(p.id)}</code></td>
          <td>${escapeHtml(d.name)}</td>
          <td>${escapeHtml(d.affiliateId || 'N/A')}</td>
          <td>${escapeHtml(providers)}</td>
          <td>${escapeHtml(d.shortCode || 'N/A')}</td>
          <td>${renderBadge(d.status || p.state || 'active', d.status === 'active' ? 'success' : 'neutral')}</td>
          <td><button class="btn btn-sm btn-ghost edit-program-btn" data-id="${escapeHtml(p.id)}">Edit</button></td>
        </tr>
      `;
    }).join('');

    tbody.querySelectorAll('.edit-program-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        const prog = programs.find(p => p.id === btn.dataset.id);
        if (prog) showProgramModal(prog);
      });
    });
  } catch (err) {
    showToast('Failed to load programs: ' + err.message, 'error');
  }
}

function showProgramModal(existing) {
  const d = existing ? (existing.data || existing) : {};
  const isEdit = !!existing;
  const providerChecks = ['twilio', 'aws', 'partner', 'webchat'].map(p => {
    const checked = (d.providers || []).includes(p) ? 'checked' : '';
    return `<label class="checkbox-label"><input type="checkbox" class="provider-cb" value="${p}" ${checked}> ${p}</label>`;
  }).join('');

  showModal(isEdit ? 'Edit Program' : 'Create Program', `
    <div class="form-group">
      <label for="prog-name">Name</label>
      <input class="form-input" id="prog-name" value="${escapeHtml(d.name || '')}">
    </div>
    <div class="form-group">
      <label for="prog-affiliate">Affiliate ID</label>
      <input class="form-input" id="prog-affiliate" value="${escapeHtml(d.affiliateId || '')}">
    </div>
    <div class="form-group">
      <label>Providers</label>
      <div class="checkbox-group">${providerChecks}</div>
    </div>
    <div class="form-row">
      <div class="form-group">
        <label for="prog-shortcode">Short Code</label>
        <input class="form-input" id="prog-shortcode" value="${escapeHtml(d.shortCode || '')}">
      </div>
      <div class="form-group">
        <label for="prog-max">Max Concurrent/Responder</label>
        <input type="number" class="form-input" id="prog-max" value="${(d.settings && d.settings.maxConcurrentPerResponder) || 3}">
      </div>
    </div>
    <div class="form-group">
      <label for="prog-desc">Description</label>
      <textarea class="form-textarea" id="prog-desc">${escapeHtml(d.description || '')}</textarea>
    </div>
  `, [
    { id: 'modal-cancel-btn', label: 'Cancel', class: 'btn-secondary' },
    { id: 'prog-save-btn', label: isEdit ? 'Update' : 'Create', class: 'btn-primary' }
  ]);

  document.getElementById('prog-save-btn').addEventListener('click', async () => {
    const providers = Array.from(document.querySelectorAll('.provider-cb:checked')).map(cb => cb.value);
    const body = {
      name: document.getElementById('prog-name').value,
      affiliateId: document.getElementById('prog-affiliate').value,
      providers,
      shortCode: document.getElementById('prog-shortcode').value,
      description: document.getElementById('prog-desc').value,
      status: 'active',
      settings: {
        maxConcurrentPerResponder: parseInt(document.getElementById('prog-max').value) || 3
      }
    };
    try {
      if (isEdit) {
        await api.put(`/api/programs/${existing.id}`, body);
        showToast('Program updated', 'success');
      } else {
        await api.post('/api/programs', body);
        showToast('Program created', 'success');
      }
      hideModal();
      loadPrograms();
    } catch (err) {
      showToast(err.message, 'error');
    }
  });
}

// ==================== Users ====================

function renderUsers() {
  if (!requireAuth()) return '';
  return `
    <div class="page">
      <div class="page-header">
        <h1 class="page-title">Users</h1>
        <button class="btn btn-primary" id="create-user-btn">Create User</button>
      </div>
      <div class="table-container">
        <table class="data-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Name</th>
              <th>Email</th>
              <th>Role</th>
              <th>Affiliate</th>
              <th>Status</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody id="users-tbody">
            <tr><td colspan="7">${renderSpinner()}</td></tr>
          </tbody>
        </table>
      </div>
    </div>
  `;
}

function handleUsers() {
  loadUsers();
  document.getElementById('create-user-btn')?.addEventListener('click', () => showUserModal());
}

async function loadUsers() {
  try {
    const result = await api.get('/api/users');
    const users = result.data || result || [];
    const tbody = document.getElementById('users-tbody');
    if (!tbody) return;

    if (users.length === 0) {
      tbody.innerHTML = '<tr><td colspan="7" class="text-center text-muted">No users found</td></tr>';
      return;
    }

    tbody.innerHTML = users.map(u => {
      const d = u.data || u;
      const roleClass = d.role === 'admin' ? 'danger' : d.role === 'supervisor' ? 'warning' : 'accent';
      return `
        <tr>
          <td><code style="font-size:0.8rem">${escapeHtml(u.id)}</code></td>
          <td>${escapeHtml(d.name)}</td>
          <td>${escapeHtml(d.email)}</td>
          <td>${renderBadge(d.role, roleClass)}</td>
          <td>${escapeHtml(d.affiliateId || 'N/A')}</td>
          <td>${renderBadge(d.status || u.state || 'active', d.status === 'active' ? 'success' : 'neutral')}</td>
          <td><button class="btn btn-sm btn-ghost edit-user-btn" data-id="${escapeHtml(u.id)}">Edit</button></td>
        </tr>
      `;
    }).join('');

    tbody.querySelectorAll('.edit-user-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        const user = users.find(u => u.id === btn.dataset.id);
        if (user) showUserModal(user);
      });
    });
  } catch (err) {
    showToast('Failed to load users: ' + err.message, 'error');
  }
}

function showUserModal(existing) {
  const d = existing ? (existing.data || existing) : {};
  const isEdit = !!existing;

  showModal(isEdit ? 'Edit User' : 'Create User', `
    <div class="form-row">
      <div class="form-group">
        <label for="user-name">Name</label>
        <input class="form-input" id="user-name" value="${escapeHtml(d.name || '')}">
      </div>
      <div class="form-group">
        <label for="user-email">Email</label>
        <input type="email" class="form-input" id="user-email" value="${escapeHtml(d.email || '')}">
      </div>
    </div>
    ${!isEdit ? `
      <div class="form-group">
        <label for="user-password">Password</label>
        <input type="password" class="form-input" id="user-password" placeholder="Min 6 characters">
      </div>
    ` : ''}
    <div class="form-row">
      <div class="form-group">
        <label for="user-role">Role</label>
        <select class="form-select" id="user-role">
          <option value="responder" ${d.role === 'responder' ? 'selected' : ''}>Responder</option>
          <option value="supervisor" ${d.role === 'supervisor' ? 'selected' : ''}>Supervisor</option>
          <option value="admin" ${d.role === 'admin' ? 'selected' : ''}>Admin</option>
        </select>
      </div>
      <div class="form-group">
        <label for="user-affiliate">Affiliate ID</label>
        <input class="form-input" id="user-affiliate" value="${escapeHtml(d.affiliateId || '')}">
      </div>
    </div>
    <div class="form-group">
      <label for="user-max">Max Concurrent</label>
      <input type="number" class="form-input" id="user-max" value="${d.maxConcurrent || 3}">
    </div>
  `, [
    { id: 'modal-cancel-btn', label: 'Cancel', class: 'btn-secondary' },
    { id: 'user-save-btn', label: isEdit ? 'Update' : 'Create', class: 'btn-primary' }
  ]);

  document.getElementById('user-save-btn').addEventListener('click', async () => {
    const body = {
      name: document.getElementById('user-name').value,
      email: document.getElementById('user-email').value,
      role: document.getElementById('user-role').value,
      affiliateId: document.getElementById('user-affiliate').value,
      maxConcurrent: parseInt(document.getElementById('user-max').value) || 3,
      status: 'active'
    };
    if (!isEdit) {
      body.password = document.getElementById('user-password')?.value || '';
    }
    try {
      if (isEdit) {
        await api.put(`/api/users/${existing.id}`, body);
        showToast('User updated', 'success');
      } else {
        await api.post('/api/users', body);
        showToast('User created', 'success');
      }
      hideModal();
      loadUsers();
    } catch (err) {
      showToast(err.message, 'error');
    }
  });
}

// ==================== Keywords ====================

function renderKeywords() {
  if (!requireAuth()) return '';
  return `
    <div class="page">
      <div class="page-header">
        <h1 class="page-title">Keywords</h1>
        <button class="btn btn-primary" id="create-keyword-btn">Create Keyword</button>
      </div>
      <div class="table-container">
        <table class="data-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Keyword</th>
              <th>Program</th>
              <th>Action</th>
              <th>Sub-Program</th>
              <th>Response</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody id="keywords-tbody">
            <tr><td colspan="7">${renderSpinner()}</td></tr>
          </tbody>
        </table>
      </div>
    </div>
  `;
}

function handleKeywords() {
  loadKeywords();
  document.getElementById('create-keyword-btn')?.addEventListener('click', () => showKeywordModal());
}

async function loadKeywords() {
  try {
    const result = await api.get('/api/keywords');
    const keywords = result.data || result || [];
    const tbody = document.getElementById('keywords-tbody');
    if (!tbody) return;

    if (keywords.length === 0) {
      tbody.innerHTML = '<tr><td colspan="7" class="text-center text-muted">No keywords found</td></tr>';
      return;
    }

    tbody.innerHTML = keywords.map(k => {
      const d = k.data || k;
      return `
        <tr>
          <td><code style="font-size:0.8rem">${escapeHtml(k.id)}</code></td>
          <td><strong>${escapeHtml(d.keyword)}</strong></td>
          <td>${escapeHtml(d.programId)}</td>
          <td>${renderBadge(d.action || 'route', d.action === 'route_priority' ? 'danger' : 'accent')}</td>
          <td>${escapeHtml(d.subProgram || 'N/A')}</td>
          <td style="max-width:200px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${escapeHtml(d.response || '')}</td>
          <td><button class="btn btn-sm btn-ghost edit-keyword-btn" data-id="${escapeHtml(k.id)}">Edit</button></td>
        </tr>
      `;
    }).join('');

    tbody.querySelectorAll('.edit-keyword-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        const kw = keywords.find(k => k.id === btn.dataset.id);
        if (kw) showKeywordModal(kw);
      });
    });
  } catch (err) {
    showToast('Failed to load keywords: ' + err.message, 'error');
  }
}

function showKeywordModal(existing) {
  const d = existing ? (existing.data || existing) : {};
  const isEdit = !!existing;

  showModal(isEdit ? 'Edit Keyword' : 'Create Keyword', `
    <div class="form-row">
      <div class="form-group">
        <label for="kw-keyword">Keyword</label>
        <input class="form-input" id="kw-keyword" value="${escapeHtml(d.keyword || '')}" placeholder="HELLO">
      </div>
      <div class="form-group">
        <label for="kw-program">Program ID</label>
        <input class="form-input" id="kw-program" value="${escapeHtml(d.programId || '')}" placeholder="prog-001">
      </div>
    </div>
    <div class="form-row">
      <div class="form-group">
        <label for="kw-action">Action</label>
        <select class="form-select" id="kw-action">
          <option value="route" ${d.action === 'route' ? 'selected' : ''}>Route</option>
          <option value="route_priority" ${d.action === 'route_priority' ? 'selected' : ''}>Route (Priority)</option>
        </select>
      </div>
      <div class="form-group">
        <label for="kw-subprogram">Sub-Program</label>
        <input class="form-input" id="kw-subprogram" value="${escapeHtml(d.subProgram || '')}" placeholder="general">
      </div>
    </div>
    <div class="form-group">
      <label for="kw-response">Auto-Response</label>
      <textarea class="form-textarea" id="kw-response">${escapeHtml(d.response || '')}</textarea>
    </div>
  `, [
    { id: 'modal-cancel-btn', label: 'Cancel', class: 'btn-secondary' },
    { id: 'kw-save-btn', label: isEdit ? 'Update' : 'Create', class: 'btn-primary' }
  ]);

  document.getElementById('kw-save-btn').addEventListener('click', async () => {
    const body = {
      keyword: document.getElementById('kw-keyword').value,
      programId: document.getElementById('kw-program').value,
      action: document.getElementById('kw-action').value,
      subProgram: document.getElementById('kw-subprogram').value,
      response: document.getElementById('kw-response').value
    };
    try {
      if (isEdit) {
        await api.put(`/api/keywords/${existing.id}`, body);
        showToast('Keyword updated', 'success');
      } else {
        await api.post('/api/keywords', body);
        showToast('Keyword created', 'success');
      }
      hideModal();
      loadKeywords();
    } catch (err) {
      showToast(err.message, 'error');
    }
  });
}

// ==================== Surveys ====================

function renderSurveys() {
  if (!requireAuth()) return '';
  return `
    <div class="page">
      <div class="page-header">
        <h1 class="page-title">Surveys</h1>
        <button class="btn btn-primary" id="create-survey-btn">Create Survey</button>
      </div>
      <div class="table-container">
        <table class="data-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Title</th>
              <th>Program</th>
              <th>Type</th>
              <th>Questions</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody id="surveys-tbody">
            <tr><td colspan="6">${renderSpinner()}</td></tr>
          </tbody>
        </table>
      </div>
    </div>
  `;
}

function handleSurveys() {
  loadSurveys();
  document.getElementById('create-survey-btn')?.addEventListener('click', () => showSurveyModal());
}

async function loadSurveys() {
  try {
    const result = await api.get('/api/surveys');
    const surveys = result.data || result || [];
    const tbody = document.getElementById('surveys-tbody');
    if (!tbody) return;

    if (surveys.length === 0) {
      tbody.innerHTML = '<tr><td colspan="6" class="text-center text-muted">No surveys found</td></tr>';
      return;
    }

    tbody.innerHTML = surveys.map(s => {
      const d = s.data || s;
      const questions = d.questions || [];
      return `
        <tr>
          <td><code style="font-size:0.8rem">${escapeHtml(s.id)}</code></td>
          <td>${escapeHtml(d.title)}</td>
          <td>${escapeHtml(d.programId)}</td>
          <td>${renderBadge(d.type || 'entry', d.type === 'exit' ? 'warning' : 'info')}</td>
          <td>${questions.length}</td>
          <td><button class="btn btn-sm btn-ghost edit-survey-btn" data-id="${escapeHtml(s.id)}">Edit</button></td>
        </tr>
      `;
    }).join('');

    tbody.querySelectorAll('.edit-survey-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        const survey = surveys.find(s => s.id === btn.dataset.id);
        if (survey) showSurveyModal(survey);
      });
    });
  } catch (err) {
    showToast('Failed to load surveys: ' + err.message, 'error');
  }
}

function showSurveyModal(existing) {
  const d = existing ? (existing.data || existing) : {};
  const isEdit = !!existing;
  const questions = d.questions || [];

  const questionsHtml = questions.map((q, i) => `
    <div class="survey-question-row" data-q-index="${i}">
      <div class="form-group">
        <label>Question ${i + 1}</label>
        <input class="form-input q-text" value="${escapeHtml(q.text || '')}">
      </div>
      <div class="form-group" style="max-width:120px">
        <label>Type</label>
        <select class="form-select q-type">
          <option value="text" ${q.type === 'text' ? 'selected' : ''}>Text</option>
          <option value="scale" ${q.type === 'scale' ? 'selected' : ''}>Scale</option>
          <option value="choice" ${q.type === 'choice' ? 'selected' : ''}>Choice</option>
        </select>
      </div>
    </div>
  `).join('');

  showModal(isEdit ? 'Edit Survey' : 'Create Survey', `
    <div class="form-group">
      <label for="survey-title">Title</label>
      <input class="form-input" id="survey-title" value="${escapeHtml(d.title || '')}">
    </div>
    <div class="form-row">
      <div class="form-group">
        <label for="survey-program">Program ID</label>
        <input class="form-input" id="survey-program" value="${escapeHtml(d.programId || '')}">
      </div>
      <div class="form-group">
        <label for="survey-type">Type</label>
        <select class="form-select" id="survey-type">
          <option value="entry" ${d.type === 'entry' ? 'selected' : ''}>Entry</option>
          <option value="exit" ${d.type === 'exit' ? 'selected' : ''}>Exit</option>
        </select>
      </div>
    </div>
    <div class="form-group">
      <label>Questions</label>
      <div class="survey-questions" id="survey-questions">${questionsHtml}</div>
      <button class="btn btn-sm btn-ghost mt-1" id="add-question-btn">+ Add Question</button>
    </div>
  `, [
    { id: 'modal-cancel-btn', label: 'Cancel', class: 'btn-secondary' },
    { id: 'survey-save-btn', label: isEdit ? 'Update' : 'Create', class: 'btn-primary' }
  ]);

  document.getElementById('add-question-btn').addEventListener('click', () => {
    const container = document.getElementById('survey-questions');
    const idx = container.children.length;
    const row = document.createElement('div');
    row.className = 'survey-question-row';
    row.dataset.qIndex = idx;
    row.innerHTML = `
      <div class="form-group">
        <label>Question ${idx + 1}</label>
        <input class="form-input q-text" value="">
      </div>
      <div class="form-group" style="max-width:120px">
        <label>Type</label>
        <select class="form-select q-type">
          <option value="text">Text</option>
          <option value="scale">Scale</option>
          <option value="choice">Choice</option>
        </select>
      </div>
    `;
    container.appendChild(row);
  });

  document.getElementById('survey-save-btn').addEventListener('click', async () => {
    const rows = document.querySelectorAll('.survey-question-row');
    const qs = Array.from(rows).map((row, i) => ({
      id: `q${i + 1}`,
      text: row.querySelector('.q-text').value,
      type: row.querySelector('.q-type').value
    })).filter(q => q.text.trim());

    const body = {
      title: document.getElementById('survey-title').value,
      programId: document.getElementById('survey-program').value,
      type: document.getElementById('survey-type').value,
      questions: qs
    };
    try {
      if (isEdit) {
        await api.put(`/api/surveys/${existing.id}`, body);
        showToast('Survey updated', 'success');
      } else {
        await api.post('/api/surveys', body);
        showToast('Survey created', 'success');
      }
      hideModal();
      loadSurveys();
    } catch (err) {
      showToast(err.message, 'error');
    }
  });
}

// ==================== Route Registration ====================

export function registerAdminRoutes(registerRoute) {
  registerRoute('admin/affiliates', renderAffiliates, handleAffiliates);
  registerRoute('admin/programs', renderPrograms, handlePrograms);
  registerRoute('admin/users', renderUsers, handleUsers);
  registerRoute('admin/keywords', renderKeywords, handleKeywords);
  registerRoute('admin/surveys', renderSurveys, handleSurveys);
}
