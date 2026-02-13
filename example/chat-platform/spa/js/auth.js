import { api } from './api.js';
import { renderNav, showToast, escapeHtml } from './components.js';

function renderLogin() {
  return `
    <div class="login-container">
      <div class="login-card">
        <h2>Chat Platform</h2>
        <p class="login-subtitle">Sign in to continue</p>
        <form id="login-form">
          <div class="form-group">
            <label for="email">Email</label>
            <input type="email" id="email" class="form-input" placeholder="you@example.com" required>
          </div>
          <div class="form-group">
            <label for="password">Password</label>
            <input type="password" id="password" class="form-input" placeholder="Password" required>
          </div>
          <button type="submit" class="btn btn-primary btn-block btn-lg" id="login-btn">Sign In</button>
        </form>
        <p class="login-footer">Secure platform for authorized responders</p>
      </div>
    </div>
  `;
}

function handleLogin() {
  const form = document.getElementById('login-form');
  if (!form) return;

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const btn = document.getElementById('login-btn');
    const email = document.getElementById('email').value;
    const password = document.getElementById('password').value;

    btn.disabled = true;
    btn.textContent = 'Signing in...';

    try {
      const result = await api.post('/api/auth/login', { email, password });
      api.setAuth(result.token, result.user);
      showToast('Welcome back, ' + (result.user.name || 'User'), 'success');

      const role = result.user.role || 'responder';
      const redirect = sessionStorage.getItem('redirect_after_login');
      sessionStorage.removeItem('redirect_after_login');

      if (redirect) {
        window.location.hash = redirect;
      } else if (role === 'admin') {
        window.location.hash = '#/admin/affiliates';
      } else if (role === 'supervisor') {
        window.location.hash = '#/supervisor';
      } else {
        window.location.hash = '#/responder';
      }
    } catch (err) {
      showToast(err.message, 'error');
      btn.disabled = false;
      btn.textContent = 'Sign In';
    }
  });
}

function renderProfile() {
  const user = api.getUser();
  if (!user) return '';

  return `
    <div class="page" style="max-width: 600px; margin: 0 auto;">
      <h2 class="page-title">Your Profile</h2>
      <div class="card">
        <div class="sidebar-field mb-1">
          <span class="sidebar-field-label">Name</span>
          <span class="sidebar-field-value">${escapeHtml(user.name)}</span>
        </div>
        <div class="sidebar-field mb-1">
          <span class="sidebar-field-label">Email</span>
          <span class="sidebar-field-value">${escapeHtml(user.email)}</span>
        </div>
        <div class="sidebar-field mb-1">
          <span class="sidebar-field-label">Role</span>
          <span class="sidebar-field-value">${escapeHtml(user.role)}</span>
        </div>
        <div class="sidebar-field mb-1">
          <span class="sidebar-field-label">Affiliate</span>
          <span class="sidebar-field-value">${escapeHtml(user.affiliateId || 'N/A')}</span>
        </div>
        <div class="mt-3">
          <button class="btn btn-danger" id="logout-btn">Sign Out</button>
        </div>
      </div>
    </div>
  `;
}

function handleProfile() {
  const logoutBtn = document.getElementById('logout-btn');
  if (logoutBtn) {
    logoutBtn.addEventListener('click', () => {
      api.clearAuth();
      showToast('Signed out', 'info');
      window.location.hash = '#/login';
    });
  }
}

export function registerAuthRoutes(registerRoute) {
  registerRoute('login', renderLogin, handleLogin);
  registerRoute('profile', renderProfile, handleProfile);
}
