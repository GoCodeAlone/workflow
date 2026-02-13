import { api } from './api.js';

export function escapeHtml(str) {
  if (!str) return '';
  const div = document.createElement('div');
  div.textContent = String(str);
  return div.innerHTML;
}

export function renderNav(user) {
  const hash = window.location.hash || '#/login';

  const navLink = (href, label) => {
    const active = hash.startsWith(href) ? ' active' : '';
    return `<a href="${href}" class="nav-link${active}">${escapeHtml(label)}</a>`;
  };

  if (!user) {
    return `
      <a href="#/login" class="nav-brand">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/>
        </svg>
        Chat Platform
      </a>
      <div class="nav-links">
        ${navLink('#/login', 'Login')}
      </div>
    `;
  }

  const role = user.role || 'responder';
  let links = '';

  if (role === 'responder') {
    links = `
      ${navLink('#/responder', 'Dashboard')}
      ${navLink('#/queue', 'Queue')}
    `;
  } else if (role === 'supervisor') {
    links = `
      ${navLink('#/supervisor', 'Overview')}
      ${navLink('#/queue', 'Queue')}
    `;
  } else if (role === 'admin') {
    links = `
      ${navLink('#/admin/affiliates', 'Affiliates')}
      ${navLink('#/admin/programs', 'Programs')}
      ${navLink('#/admin/users', 'Users')}
      ${navLink('#/admin/keywords', 'Keywords')}
      ${navLink('#/admin/surveys', 'Surveys')}
      ${navLink('#/queue', 'Queue')}
    `;
  }

  const roleBadgeClass = `role-${role}`;

  return `
    <a href="#/${role === 'admin' ? 'admin/affiliates' : role}" class="nav-brand">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/>
      </svg>
      Chat Platform
    </a>
    <div class="nav-links">
      ${links}
    </div>
    <div class="nav-user">
      <span class="nav-role-badge ${roleBadgeClass}">${escapeHtml(role)}</span>
      <span class="nav-user-name">${escapeHtml(user.name)}</span>
      <a href="#/profile" class="nav-link${hash === '#/profile' ? ' active' : ''}">Profile</a>
    </div>
  `;
}

export function showModal(title, contentHtml, actions) {
  const overlay = document.getElementById('modal-overlay');
  const actionsHtml = actions
    ? actions.map(a => `<button class="btn ${a.class || 'btn-secondary'}" id="${a.id}">${escapeHtml(a.label)}</button>`).join('')
    : '<button class="btn btn-secondary" id="modal-cancel-btn">Cancel</button>';

  overlay.innerHTML = `
    <div class="modal">
      <div class="modal-header">
        <h3 class="modal-title">${escapeHtml(title)}</h3>
        <button class="modal-close" id="modal-close-btn">&times;</button>
      </div>
      <div class="modal-body">${contentHtml}</div>
      <div class="modal-footer">${actionsHtml}</div>
    </div>
  `;
  overlay.classList.remove('hidden');

  const closeBtn = document.getElementById('modal-close-btn');
  if (closeBtn) closeBtn.addEventListener('click', hideModal);
  const cancelBtn = document.getElementById('modal-cancel-btn');
  if (cancelBtn) cancelBtn.addEventListener('click', hideModal);

  overlay.addEventListener('click', (e) => {
    if (e.target === overlay) hideModal();
  });
}

export function hideModal() {
  const overlay = document.getElementById('modal-overlay');
  overlay.classList.add('hidden');
  overlay.innerHTML = '';
}

export function showToast(message, type = 'info') {
  const container = document.getElementById('toast-container');
  if (!container) return;
  const toast = document.createElement('div');
  toast.className = `toast toast-${type}`;
  toast.textContent = message;
  container.appendChild(toast);
  setTimeout(() => {
    toast.classList.add('toast-exit');
    setTimeout(() => toast.remove(), 300);
  }, 3500);
}

export function renderBadge(text, color) {
  const cls = color ? `badge-${color}` : 'badge-neutral';
  return `<span class="badge ${cls}">${escapeHtml(text)}</span>`;
}

export function renderStatusBadge(status) {
  const label = (status || 'unknown').replace(/_/g, ' ');
  return `<span class="badge status-${escapeHtml(status)}">${escapeHtml(label)}</span>`;
}

export function renderRiskIndicator(level) {
  if (!level) return '';
  const cls = `risk-${level}`;
  return `<span class="risk-indicator ${cls}">${escapeHtml(level)}</span>`;
}

export function renderStatusDot(status) {
  const cls = status || 'offline';
  return `<span class="status-dot ${cls}">${escapeHtml(cls)}</span>`;
}

export function renderLoadBar(current, max) {
  if (!max) return '';
  const pct = Math.min(100, Math.round((current / max) * 100));
  let cls = 'load-low';
  if (pct >= 80) cls = 'load-high';
  else if (pct >= 50) cls = 'load-medium';
  return `
    <div class="load-bar">
      <div class="load-bar-track">
        <div class="load-bar-fill ${cls}" style="width: ${pct}%"></div>
      </div>
      <span class="load-bar-label">${current}/${max}</span>
    </div>
  `;
}

export function formatTime(iso) {
  if (!iso) return '';
  const date = new Date(iso);
  const now = new Date();
  const diffMs = now - date;
  const diffSecs = Math.floor(diffMs / 1000);
  const diffMins = Math.floor(diffSecs / 60);
  const diffHours = Math.floor(diffMins / 60);
  const diffDays = Math.floor(diffHours / 24);

  if (diffSecs < 60) return 'just now';
  if (diffMins < 60) return `${diffMins}m ago`;
  if (diffHours < 24) return `${diffHours}h ago`;
  if (diffDays < 7) return `${diffDays}d ago`;
  return date.toLocaleDateString();
}

export function formatTimestamp(iso) {
  if (!iso) return '';
  const date = new Date(iso);
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

export function maskPhone(phone) {
  if (!phone) return '****';
  const cleaned = String(phone).replace(/\D/g, '');
  if (cleaned.length < 4) return '****';
  return '***-***-' + cleaned.slice(-4);
}

export function renderSpinner() {
  return '<div class="spinner-container"><div class="spinner"></div></div>';
}

export function requireAuth() {
  if (!api.isLoggedIn()) {
    const current = window.location.hash;
    if (current && current !== '#/login') {
      sessionStorage.setItem('redirect_after_login', current);
    }
    window.location.hash = '#/login';
    return false;
  }
  return true;
}
