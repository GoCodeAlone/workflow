import * as api from './api.js';
import { renderHeader, renderSpinner, showToast, formatPrice, requireAuth } from './components.js';
import { getCartItems, getCartTotal, clearCart } from './cart.js';

export function renderCheckout() {
  if (!requireAuth()) return '';

  const items = getCartItems();
  if (items.length === 0) {
    window.location.hash = '#/cart';
    return '';
  }

  return `
    ${renderHeader()}
    <div class="main">
      <a href="#/cart" class="back-link">&larr; Back to cart</a>
      <h1 class="page-title">Checkout</h1>
      <div class="checkout-grid">
        <div class="checkout-section">
          <h3>Shipping Information</h3>
          <form id="checkout-form">
            <div class="form-group">
              <label for="address">Street Address</label>
              <input type="text" id="address" class="form-input" placeholder="123 Main St" required>
            </div>
            <div class="form-group">
              <label for="city">City</label>
              <input type="text" id="city" class="form-input" placeholder="San Francisco" required>
            </div>
            <div class="form-group">
              <label for="state">State</label>
              <input type="text" id="state" class="form-input" placeholder="CA" required>
            </div>
            <div class="form-group">
              <label for="zip">ZIP Code</label>
              <input type="text" id="zip" class="form-input" placeholder="94102" required>
            </div>
            <button type="submit" class="btn btn-success btn-block btn-lg" id="place-order-btn">Place Order</button>
          </form>
        </div>
        <div>
          <div class="checkout-section">
            <h3>Order Summary</h3>
            ${items.map(item => `
              <div class="flex-between mb-1">
                <span>${escapeHtml(item.name)} x${item.quantity}</span>
                <span>${formatPrice(item.price * item.quantity)}</span>
              </div>
            `).join('')}
            <div class="cart-summary-total">
              <span>Total</span>
              <span>${formatPrice(getCartTotal())}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  `;
}

export function attachCheckoutHandlers() {
  const form = document.getElementById('checkout-form');
  if (!form) return;

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const btn = document.getElementById('place-order-btn');
    btn.disabled = true;
    btn.textContent = 'Placing order...';

    const items = getCartItems().map(item => ({
      productId: item.productId,
      quantity: item.quantity,
    }));

    const shipping = {
      address: document.getElementById('address').value,
      city: document.getElementById('city').value,
      state: document.getElementById('state').value,
      zip: document.getElementById('zip').value,
    };

    try {
      const order = await api.createOrder(items, shipping);
      clearCart();
      showToast('Order placed successfully!', 'success');
      window.location.hash = `#/orders/${order.id || order.orderId}`;
    } catch (err) {
      showToast(err.message, 'error');
      btn.disabled = false;
      btn.textContent = 'Place Order';
    }
  });
}

export function renderOrders() {
  if (!requireAuth()) return '';

  return `
    ${renderHeader()}
    <div class="main">
      <h1 class="page-title">Your Orders</h1>
      <div id="orders-content">${renderSpinner()}</div>
    </div>
  `;
}

export function attachOrdersHandlers() {
  loadOrders();
}

async function loadOrders() {
  const container = document.getElementById('orders-content');
  if (!container) return;

  try {
    const data = await api.getOrders();
    const orders = Array.isArray(data) ? data : (data.orders || []);

    if (orders.length === 0) {
      container.innerHTML = `
        <div class="cart-empty">
          <p>No orders yet</p>
          <a href="#/" class="btn btn-primary">Start Shopping</a>
        </div>
      `;
      return;
    }

    container.innerHTML = orders.map(order => `
      <div class="order-card" data-id="${order.id || order.orderId}">
        <div class="order-card-header">
          <span class="order-id">Order #${escapeHtml(order.id || order.orderId || '')}</span>
          <span class="order-status status-${(order.state || order.status || 'pending').toLowerCase()}">${escapeHtml(order.state || order.status || 'pending')}</span>
        </div>
        <div class="order-card-meta">
          <span>${order.items ? order.items.length + ' item(s)' : ''}</span>
          <span>${order.total ? formatPrice(order.total) : ''}</span>
          <span>${order.createdAt ? new Date(order.createdAt).toLocaleDateString() : ''}</span>
        </div>
      </div>
    `).join('');

    container.querySelectorAll('.order-card').forEach(card => {
      card.addEventListener('click', () => {
        window.location.hash = `#/orders/${card.dataset.id}`;
      });
    });
  } catch (err) {
    container.innerHTML = `<p class="text-muted">Failed to load orders. ${escapeHtml(err.message)}</p>`;
  }
}

export function renderOrderDetail(id) {
  if (!requireAuth()) return '';

  return `
    ${renderHeader()}
    <div class="main">
      <a href="#/orders" class="back-link">&larr; Back to orders</a>
      <div id="order-detail-content">${renderSpinner()}</div>
    </div>
  `;
}

export function attachOrderDetailHandlers(id) {
  loadOrderDetail(id);
}

async function loadOrderDetail(id) {
  const container = document.getElementById('order-detail-content');
  if (!container) return;

  try {
    const order = await api.getOrder(id);
    const state = (order.state || order.status || 'pending').toLowerCase();
    const items = order.items || [];
    const shipping = order.shipping || {};

    container.innerHTML = `
      <div class="order-detail-header">
        <div>
          <h1 class="page-title mb-1">Order #${escapeHtml(order.id || order.orderId || id)}</h1>
          <p class="text-muted">${order.createdAt ? 'Placed ' + new Date(order.createdAt).toLocaleString() : ''}</p>
        </div>
        <span class="order-status status-${state}">${escapeHtml(order.state || order.status || 'pending')}</span>
      </div>

      <div class="order-detail-items">
        <h3 style="margin-bottom: 0.75rem;">Items</h3>
        ${items.map(item => `
          <div class="order-detail-item">
            <span>${escapeHtml(item.name || item.productId)} x${item.quantity}</span>
            <span>${item.price ? formatPrice(item.price * item.quantity) : ''}</span>
          </div>
        `).join('')}
        ${order.total ? `
          <div class="order-detail-item" style="font-weight: 700; border-bottom: none;">
            <span>Total</span>
            <span>${formatPrice(order.total)}</span>
          </div>
        ` : ''}
      </div>

      ${shipping.address ? `
        <div class="checkout-section">
          <h3>Shipping Address</h3>
          <p>${escapeHtml(shipping.address)}</p>
          <p>${escapeHtml(shipping.city || '')}, ${escapeHtml(shipping.state || '')} ${escapeHtml(shipping.zip || '')}</p>
        </div>
      ` : ''}
    `;
  } catch (err) {
    container.innerHTML = `<p class="text-muted">Failed to load order. ${escapeHtml(err.message)}</p>`;
  }
}

function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = String(str);
  return div.innerHTML;
}
