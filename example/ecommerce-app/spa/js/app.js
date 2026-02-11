import { renderCatalog, attachCatalogHandlers, renderProductDetail, attachProductDetailHandlers } from './catalog.js';
import { renderLogin, attachLoginHandlers, renderRegister, attachRegisterHandlers, renderProfile, attachProfileHandlers } from './auth.js';
import { renderCart, attachCartHandlers } from './cart.js';
import { renderCheckout, attachCheckoutHandlers, renderOrders, attachOrdersHandlers, renderOrderDetail, attachOrderDetailHandlers } from './orders.js';
import { requireAuth } from './components.js';

function parseHash() {
  const hash = window.location.hash || '#/';
  const path = hash.slice(1);
  const parts = path.split('/').filter(Boolean);
  return { path, parts };
}

function route() {
  const { path, parts } = parseHash();
  const app = document.getElementById('app');
  let html = '';
  let attach = null;

  if (parts.length === 0 || (parts.length === 1 && parts[0] === '')) {
    // #/ — catalog
    html = renderCatalog();
    attach = attachCatalogHandlers;
  } else if (parts[0] === 'login') {
    html = renderLogin();
    attach = attachLoginHandlers;
  } else if (parts[0] === 'register') {
    html = renderRegister();
    attach = attachRegisterHandlers;
  } else if (parts[0] === 'product' && parts[1]) {
    html = renderProductDetail(parts[1]);
    attach = () => attachProductDetailHandlers(parts[1]);
  } else if (parts[0] === 'cart') {
    html = renderCart();
    attach = attachCartHandlers;
  } else if (parts[0] === 'checkout') {
    if (!requireAuth()) return;
    html = renderCheckout();
    attach = attachCheckoutHandlers;
  } else if (parts[0] === 'orders' && parts[1]) {
    if (!requireAuth()) return;
    html = renderOrderDetail(parts[1]);
    attach = () => attachOrderDetailHandlers(parts[1]);
  } else if (parts[0] === 'orders') {
    if (!requireAuth()) return;
    html = renderOrders();
    attach = attachOrdersHandlers;
  } else if (parts[0] === 'profile') {
    if (!requireAuth()) return;
    html = renderProfile();
    attach = attachProfileHandlers;
  } else {
    // 404
    html = `
      <div class="main text-center" style="padding-top: 4rem;">
        <h1>Page Not Found</h1>
        <p class="text-muted mt-1">The page you are looking for does not exist.</p>
        <a href="#/" class="btn btn-primary mt-2">Go Home</a>
      </div>
    `;
  }

  app.innerHTML = html;
  if (attach) attach();
}

window.addEventListener('hashchange', route);
window.addEventListener('DOMContentLoaded', route);
