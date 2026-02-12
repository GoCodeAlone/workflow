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
      name: item.name,
      price: item.price,
      quantity: item.quantity,
    }));

    const shipping = {
      address: document.getElementById('address').value,
      city: document.getElementById('city').value,
      state: document.getElementById('state').value,
      zip: document.getElementById('zip').value,
    };

    try {
      const total = getCartTotal();
      const order = await api.createOrder(items, shipping, total);
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
    const orders = (Array.isArray(data) ? data : (data.orders || [])).map(normalizeOrder);

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

// Pipeline stages for the order processing timeline
const PIPELINE_STAGES = [
  { key: 'new', label: 'Created' },
  { key: 'validating', label: 'Checking Inventory' },
  { key: 'validated', label: 'Inventory OK' },
  { key: 'paying', label: 'Processing Payment' },
  { key: 'paid', label: 'Payment OK' },
  { key: 'shipping', label: 'Shipping' },
  { key: 'shipped', label: 'Shipped' },
  { key: 'delivered', label: 'Delivered' },
];

// Error states and what stage they correspond to
const ERROR_STATE_MAP = {
  'failed': 1,          // failed during validation
  'payment_failed': 3,  // failed during payment
  'ship_failed': 5,     // failed during shipping
};

function getStageIndex(state) {
  const idx = PIPELINE_STAGES.findIndex(s => s.key === state);
  if (idx >= 0) return idx;
  // Check error states
  if (state in ERROR_STATE_MAP) return ERROR_STATE_MAP[state];
  return -1;
}

function isErrorState(state) {
  return ['failed', 'payment_failed', 'ship_failed', 'cancelled'].includes(state);
}

function getErrorMessage(state, order) {
  const messages = {
    'failed': { title: 'Inventory Check Failed', detail: 'One or more items are out of stock.' },
    'payment_failed': { title: 'Payment Declined', detail: order.error || 'Your payment method was declined. The system will retry automatically.' },
    'ship_failed': { title: 'Shipping Failed', detail: order.error || 'Unable to generate shipping label. The system will retry automatically.' },
    'cancelled': { title: 'Order Cancelled', detail: 'This order has been cancelled.' },
  };
  return messages[state] || { title: 'Error', detail: 'An unexpected error occurred.' };
}

function renderTimeline(state) {
  const currentIdx = getStageIndex(state);
  const isError = isErrorState(state);
  const isCancelled = state === 'cancelled';

  if (isCancelled) {
    return `
      <div class="order-timeline">
        <h3>Order Status</h3>
        <div class="timeline-track">
          <div class="timeline-step error">
            <div class="timeline-dot">X</div>
            <span class="timeline-label">Cancelled</span>
          </div>
        </div>
      </div>
    `;
  }

  let html = '<div class="order-timeline"><h3>Processing Pipeline</h3><div class="timeline-track">';

  PIPELINE_STAGES.forEach((stage, i) => {
    let stepClass = '';
    let dotContent = '';

    if (i < currentIdx) {
      stepClass = 'completed';
      dotContent = '&#10003;';
    } else if (i === currentIdx) {
      if (isError) {
        stepClass = 'error';
        dotContent = '!';
      } else {
        stepClass = 'current';
        dotContent = '&#9679;';
      }
    } else {
      dotContent = (i + 1).toString();
    }

    html += `
      <div class="timeline-step ${stepClass}">
        <div class="timeline-dot">${dotContent}</div>
        <span class="timeline-label">${stage.label}</span>
      </div>
    `;

    // Add connector between steps (not after last)
    if (i < PIPELINE_STAGES.length - 1) {
      const connClass = i < currentIdx ? 'completed' : '';
      html += `<div class="timeline-connector ${connClass}"></div>`;
    }
  });

  html += '</div></div>';
  return html;
}

function renderProcessingInfo(order) {
  const details = [];

  // Show transaction ID if payment was processed
  if (order.transaction_id) {
    details.push({ label: 'Transaction ID', value: order.transaction_id });
  }
  if (order.last4) {
    details.push({ label: 'Card', value: `**** ${order.last4}` });
  }
  if (order.tracking_number) {
    details.push({ label: 'Tracking Number', value: order.tracking_number });
  }
  if (order.carrier) {
    details.push({ label: 'Carrier', value: order.carrier });
  }
  if (order.estimated_delivery) {
    details.push({ label: 'Est. Delivery', value: order.estimated_delivery });
  }

  if (details.length === 0) return '';

  return `
    <div class="order-processing-info">
      <h3>Processing Details</h3>
      ${details.map(d => `
        <div class="processing-detail">
          <span class="detail-label">${escapeHtml(d.label)}</span>
          <span class="detail-value">${escapeHtml(d.value)}</span>
        </div>
      `).join('')}
    </div>
  `;
}

async function loadOrderDetail(id) {
  const container = document.getElementById('order-detail-content');
  if (!container) return;

  try {
    const order = normalizeOrder(await api.getOrder(id));
    const state = (order.state || order.status || 'pending').toLowerCase();
    const items = order.items || [];
    const shipping = order.shipping || {};

    const errorInfo = isErrorState(state) ? getErrorMessage(state, order) : null;

    container.innerHTML = `
      <div class="order-detail-header">
        <div>
          <h1 class="page-title mb-1">Order #${escapeHtml(order.id || order.orderId || id)}</h1>
          <p class="text-muted">${order.createdAt ? 'Placed ' + new Date(order.createdAt).toLocaleString() : ''}</p>
        </div>
        <span class="order-status status-${state}">${escapeHtml(order.state || order.status || 'pending')}</span>
      </div>

      ${renderTimeline(state)}

      ${errorInfo ? `
        <div class="order-error-callout">
          <span class="error-icon">&#9888;</span>
          <div class="error-text">
            <strong>${escapeHtml(errorInfo.title)}</strong>
            <span class="error-detail">${escapeHtml(errorInfo.detail)}</span>
          </div>
        </div>
      ` : ''}

      ${renderProcessingInfo(order)}

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

// normalizeOrder flattens the API response format.
// The API returns {id, data: {items, shipping, ...}, state, lastUpdate}
// but the SPA expects {id, items, shipping, state, ...} at the top level.
function normalizeOrder(raw) {
  const order = { ...raw };
  if (raw.data && typeof raw.data === 'object') {
    // Merge nested data fields to top level (items, shipping, total, userId, etc.)
    for (const [k, v] of Object.entries(raw.data)) {
      if (order[k] === undefined) {
        order[k] = v;
      }
    }
  }
  // Use lastUpdate as createdAt fallback
  if (!order.createdAt && order.lastUpdate) {
    order.createdAt = order.lastUpdate;
  }
  return order;
}

function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = String(str);
  return div.innerHTML;
}
