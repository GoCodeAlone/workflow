import { getCartCount } from './cart.js';

export function isLoggedIn() {
  return !!localStorage.getItem('token');
}

export function getUser() {
  try {
    return JSON.parse(localStorage.getItem('user'));
  } catch {
    return null;
  }
}

export function formatPrice(cents) {
  if (typeof cents === 'number') {
    return `$${cents.toFixed(2)}`;
  }
  return '$0.00';
}

export function renderHeader() {
  const loggedIn = isLoggedIn();
  const count = getCartCount();
  const hash = window.location.hash || '#/';

  const navLink = (href, label) => {
    const active = hash === href ? ' active' : '';
    return `<a href="${href}" class="${active}">${label}</a>`;
  };

  return `
    <header class="header">
      <div class="header-inner">
        <a href="#/" class="header-logo">
          <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M6 2L3 6v14a2 2 0 002 2h14a2 2 0 002-2V6l-3-4z"/>
            <line x1="3" y1="6" x2="21" y2="6"/>
            <path d="M16 10a4 4 0 01-8 0"/>
          </svg>
          <span>Workflow Store</span>
        </a>
        <nav class="header-nav">
          ${navLink('#/', 'Catalog')}
          ${loggedIn ? navLink('#/orders', 'Orders') : ''}
          <a href="#/cart" class="cart-link${hash === '#/cart' ? ' active' : ''}">
            Cart${count > 0 ? `<span class="cart-badge">${count}</span>` : ''}
          </a>
          ${loggedIn
            ? navLink('#/profile', 'Profile')
            : navLink('#/login', 'Login')
          }
        </nav>
      </div>
    </header>
  `;
}

export function renderSpinner() {
  return `<div class="spinner-container"><div class="spinner"></div></div>`;
}

export function showToast(message, type = 'info') {
  const container = document.getElementById('toast-container');
  const toast = document.createElement('div');
  toast.className = `toast toast-${type}`;
  toast.textContent = message;
  container.appendChild(toast);
  setTimeout(() => {
    toast.classList.add('toast-exit');
    setTimeout(() => toast.remove(), 300);
  }, 3000);
}

export function requireAuth() {
  if (!isLoggedIn()) {
    const current = window.location.hash;
    if (current && current !== '#/login' && current !== '#/register') {
      sessionStorage.setItem('redirect_after_login', current);
    }
    window.location.hash = '#/login';
    return false;
  }
  return true;
}
