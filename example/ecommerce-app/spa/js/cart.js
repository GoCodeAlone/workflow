import { renderHeader, showToast, formatPrice, requireAuth } from './components.js';

const CART_KEY = 'cart';

function getCart() {
  try {
    return JSON.parse(localStorage.getItem(CART_KEY)) || [];
  } catch {
    return [];
  }
}

function saveCart(cart) {
  localStorage.setItem(CART_KEY, JSON.stringify(cart));
}

export function getCartCount() {
  return getCart().reduce((sum, item) => sum + item.quantity, 0);
}

export function getCartTotal() {
  return getCart().reduce((sum, item) => sum + item.price * item.quantity, 0);
}

export function getCartItems() {
  return getCart();
}

export function clearCart() {
  localStorage.removeItem(CART_KEY);
}

export function addToCart(productId, name, price, image) {
  const cart = getCart();
  const existing = cart.find(item => item.productId === productId);
  if (existing) {
    existing.quantity += 1;
  } else {
    cart.push({ productId, name, price, quantity: 1, image });
  }
  saveCart(cart);
}

function updateQuantity(productId, delta) {
  const cart = getCart();
  const item = cart.find(i => i.productId === productId);
  if (!item) return;
  item.quantity += delta;
  if (item.quantity <= 0) {
    const idx = cart.indexOf(item);
    cart.splice(idx, 1);
  }
  saveCart(cart);
}

function removeFromCart(productId) {
  const cart = getCart().filter(i => i.productId !== productId);
  saveCart(cart);
}

export function renderCart() {
  const items = getCart();

  return `
    ${renderHeader()}
    <div class="main">
      <div class="cart-container">
        <h1 class="page-title">Your Cart</h1>
        ${items.length === 0 ? `
          <div class="cart-empty">
            <p>Your cart is empty</p>
            <a href="#/" class="btn btn-primary">Browse Products</a>
          </div>
        ` : `
          <div id="cart-items">
            ${items.map(item => `
              <div class="cart-item" data-id="${item.productId}">
                <img class="cart-item-img" src="${item.image}" alt="${escapeAttr(item.name)}" onerror="this.style.display='none'">
                <div class="cart-item-info">
                  <div class="cart-item-name">${escapeHtml(item.name)}</div>
                  <div class="cart-item-price">${formatPrice(item.price)}</div>
                </div>
                <div class="cart-item-qty">
                  <button class="qty-minus" data-id="${item.productId}">&minus;</button>
                  <span>${item.quantity}</span>
                  <button class="qty-plus" data-id="${item.productId}">+</button>
                </div>
                <button class="cart-item-remove" data-id="${item.productId}" title="Remove">&times;</button>
              </div>
            `).join('')}
          </div>
          <div class="cart-summary">
            <div class="cart-summary-row">
              <span>Items (${items.reduce((s, i) => s + i.quantity, 0)})</span>
              <span>${formatPrice(getCartTotal())}</span>
            </div>
            <div class="cart-summary-total">
              <span>Total</span>
              <span>${formatPrice(getCartTotal())}</span>
            </div>
            <div class="mt-2">
              <a href="#/checkout" class="btn btn-success btn-block btn-lg">Proceed to Checkout</a>
            </div>
          </div>
        `}
      </div>
    </div>
  `;
}

export function attachCartHandlers() {
  document.querySelectorAll('.qty-minus').forEach(btn => {
    btn.addEventListener('click', () => {
      updateQuantity(btn.dataset.id, -1);
      rerender();
    });
  });

  document.querySelectorAll('.qty-plus').forEach(btn => {
    btn.addEventListener('click', () => {
      updateQuantity(btn.dataset.id, 1);
      rerender();
    });
  });

  document.querySelectorAll('.cart-item-remove').forEach(btn => {
    btn.addEventListener('click', () => {
      removeFromCart(btn.dataset.id);
      showToast('Item removed', 'info');
      rerender();
    });
  });
}

function rerender() {
  const app = document.getElementById('app');
  app.innerHTML = renderCart();
  attachCartHandlers();
}

function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

function escapeAttr(str) {
  return str.replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}
