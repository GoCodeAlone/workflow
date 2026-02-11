import * as api from './api.js';
import { renderHeader, renderSpinner, showToast, formatPrice } from './components.js';
import { addToCart } from './cart.js';

let productsCache = null;

export async function loadProducts() {
  if (productsCache) return productsCache;
  const data = await api.getProducts();
  productsCache = Array.isArray(data) ? data : (data.products || []);
  return productsCache;
}

export function getProductById(id) {
  if (!productsCache) return null;
  return productsCache.find(p => p.id === id);
}

export function renderCatalog() {
  return `
    ${renderHeader()}
    <div class="main">
      <h1 class="page-title">Products</h1>
      <div id="catalog-content">${renderSpinner()}</div>
    </div>
  `;
}

export function attachCatalogHandlers() {
  loadCatalogContent();
}

async function loadCatalogContent() {
  const container = document.getElementById('catalog-content');
  if (!container) return;

  try {
    const products = await loadProducts();
    if (products.length === 0) {
      container.innerHTML = '<p class="text-muted text-center">No products available.</p>';
      return;
    }

    container.innerHTML = `
      <div class="product-grid">
        ${products.map(p => {
          const d = p.data || p;
          return `
            <div class="product-card" data-id="${p.id}">
              <img class="product-card-img" src="${d.image}" alt="${escapeAttr(d.name)}" onerror="this.style.display='none'">
              <div class="product-card-body">
                <div class="product-card-category">${escapeHtml(d.category)}</div>
                <div class="product-card-name">${escapeHtml(d.name)}</div>
                <div class="product-card-price">${formatPrice(d.price)}</div>
                <div class="product-card-stock">${d.stock > 0 ? `${d.stock} in stock` : 'Out of stock'}</div>
              </div>
            </div>
          `;
        }).join('')}
      </div>
    `;

    container.querySelectorAll('.product-card').forEach(card => {
      card.addEventListener('click', () => {
        window.location.hash = `#/product/${card.dataset.id}`;
      });
    });
  } catch (err) {
    container.innerHTML = `<p class="text-muted text-center">Failed to load products. ${escapeHtml(err.message)}</p>`;
  }
}

export function renderProductDetail(id) {
  return `
    ${renderHeader()}
    <div class="main">
      <a href="#/" class="back-link">&larr; Back to catalog</a>
      <div id="product-detail-content">${renderSpinner()}</div>
    </div>
  `;
}

export function attachProductDetailHandlers(id) {
  loadProductDetail(id);
}

async function loadProductDetail(id) {
  const container = document.getElementById('product-detail-content');
  if (!container) return;

  try {
    await loadProducts();
    const product = getProductById(id);
    if (!product) {
      container.innerHTML = '<p class="text-muted">Product not found.</p>';
      return;
    }

    const d = product.data || product;
    container.innerHTML = `
      <div class="product-detail">
        <img class="product-detail-img" src="${d.image}" alt="${escapeAttr(d.name)}" onerror="this.style.display='none'">
        <div class="product-detail-info">
          <div class="category">${escapeHtml(d.category)}</div>
          <h1>${escapeHtml(d.name)}</h1>
          <div class="price">${formatPrice(d.price)}</div>
          <p class="description">${escapeHtml(d.description)}</p>
          <p class="stock">${d.stock > 0 ? `${d.stock} units in stock` : 'Currently out of stock'}</p>
          ${d.stock > 0 ? `
            <div class="flex gap-1">
              <button class="btn btn-primary btn-lg" id="add-to-cart-btn">Add to Cart</button>
            </div>
          ` : ''}
        </div>
      </div>
    `;

    const btn = document.getElementById('add-to-cart-btn');
    if (btn) {
      btn.addEventListener('click', () => {
        addToCart(product.id, d.name, d.price, d.image);
        showToast(`${d.name} added to cart`, 'success');
        // Re-render header to update cart badge
        const header = document.querySelector('.header');
        if (header) {
          const temp = document.createElement('div');
          temp.innerHTML = renderHeader();
          header.replaceWith(temp.firstElementChild);
        }
      });
    }
  } catch (err) {
    container.innerHTML = `<p class="text-muted">Failed to load product. ${escapeHtml(err.message)}</p>`;
  }
}

function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

function escapeAttr(str) {
  return str.replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}
