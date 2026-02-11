import * as api from './api.js';
import { renderHeader, showToast, isLoggedIn } from './components.js';

export function renderLogin() {
  if (isLoggedIn()) {
    window.location.hash = '#/';
    return '';
  }

  return `
    ${renderHeader()}
    <div class="main">
      <div class="auth-container">
        <div class="auth-card">
          <h2>Welcome back</h2>
          <p class="subtitle">Sign in to your account</p>
          <form id="login-form">
            <div class="form-group">
              <label for="email">Email</label>
              <input type="email" id="email" class="form-input" placeholder="you@example.com" required>
            </div>
            <div class="form-group">
              <label for="password">Password</label>
              <input type="password" id="password" class="form-input" placeholder="Your password" required>
            </div>
            <button type="submit" class="btn btn-primary btn-block btn-lg" id="login-btn">Sign In</button>
          </form>
          <p class="footer-text">
            Don't have an account? <a href="#/register">Create one</a>
          </p>
        </div>
      </div>
    </div>
  `;
}

export function attachLoginHandlers() {
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
      const result = await api.login(email, password);
      localStorage.setItem('token', result.token);
      localStorage.setItem('user', JSON.stringify(result.user));
      showToast('Welcome back!', 'success');
      const redirect = sessionStorage.getItem('redirect_after_login') || '#/';
      sessionStorage.removeItem('redirect_after_login');
      window.location.hash = redirect;
    } catch (err) {
      showToast(err.message, 'error');
      btn.disabled = false;
      btn.textContent = 'Sign In';
    }
  });
}

export function renderRegister() {
  if (isLoggedIn()) {
    window.location.hash = '#/';
    return '';
  }

  return `
    ${renderHeader()}
    <div class="main">
      <div class="auth-container">
        <div class="auth-card">
          <h2>Create account</h2>
          <p class="subtitle">Join Workflow Store today</p>
          <form id="register-form">
            <div class="form-group">
              <label for="name">Full name</label>
              <input type="text" id="name" class="form-input" placeholder="Jane Doe" required>
            </div>
            <div class="form-group">
              <label for="email">Email</label>
              <input type="email" id="email" class="form-input" placeholder="you@example.com" required>
            </div>
            <div class="form-group">
              <label for="password">Password</label>
              <input type="password" id="password" class="form-input" placeholder="Min 8 characters" required minlength="8">
            </div>
            <button type="submit" class="btn btn-primary btn-block btn-lg" id="register-btn">Create Account</button>
          </form>
          <p class="footer-text">
            Already have an account? <a href="#/login">Sign in</a>
          </p>
        </div>
      </div>
    </div>
  `;
}

export function attachRegisterHandlers() {
  const form = document.getElementById('register-form');
  if (!form) return;

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const btn = document.getElementById('register-btn');
    const name = document.getElementById('name').value;
    const email = document.getElementById('email').value;
    const password = document.getElementById('password').value;

    btn.disabled = true;
    btn.textContent = 'Creating account...';

    try {
      const result = await api.register(email, password, name);
      localStorage.setItem('token', result.token);
      localStorage.setItem('user', JSON.stringify(result.user));
      showToast('Account created! Welcome!', 'success');
      const redirect = sessionStorage.getItem('redirect_after_login') || '#/';
      sessionStorage.removeItem('redirect_after_login');
      window.location.hash = redirect;
    } catch (err) {
      showToast(err.message, 'error');
      btn.disabled = false;
      btn.textContent = 'Create Account';
    }
  });
}

export function renderProfile() {
  return `
    ${renderHeader()}
    <div class="main">
      <div class="profile-container">
        <div class="profile-card">
          <h2>Your Profile</h2>
          <div id="profile-content">
            <div class="spinner-container"><div class="spinner"></div></div>
          </div>
        </div>
      </div>
    </div>
  `;
}

export function attachProfileHandlers() {
  loadProfile();
}

async function loadProfile() {
  const container = document.getElementById('profile-content');
  if (!container) return;

  try {
    const profile = await api.getProfile();
    container.innerHTML = `
      <form id="profile-form">
        <div class="form-group">
          <label for="name">Full name</label>
          <input type="text" id="name" class="form-input" value="${escapeHtml(profile.name || '')}" required>
        </div>
        <div class="form-group">
          <label for="email">Email</label>
          <input type="email" id="email" class="form-input" value="${escapeHtml(profile.email || '')}" readonly>
        </div>
        <div class="flex gap-1 mt-2">
          <button type="submit" class="btn btn-primary" id="save-btn">Save Changes</button>
          <button type="button" class="btn btn-danger" id="logout-btn">Sign Out</button>
        </div>
      </form>
    `;

    document.getElementById('profile-form').addEventListener('submit', async (e) => {
      e.preventDefault();
      const btn = document.getElementById('save-btn');
      btn.disabled = true;
      btn.textContent = 'Saving...';
      try {
        const updated = await api.updateProfile({ name: document.getElementById('name').value });
        localStorage.setItem('user', JSON.stringify(updated.user || updated));
        showToast('Profile updated', 'success');
      } catch (err) {
        showToast(err.message, 'error');
      }
      btn.disabled = false;
      btn.textContent = 'Save Changes';
    });

    document.getElementById('logout-btn').addEventListener('click', () => {
      localStorage.removeItem('token');
      localStorage.removeItem('user');
      showToast('Signed out', 'info');
      window.location.hash = '#/';
    });
  } catch (err) {
    container.innerHTML = `<p class="text-muted">Failed to load profile. ${escapeHtml(err.message)}</p>`;
  }
}

function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}
