const BASE_URL = '';

function getAuthHeaders() {
  const token = localStorage.getItem('token');
  if (token) {
    return { 'Authorization': `Bearer ${token}` };
  }
  return {};
}

async function request(method, path, body = null) {
  const headers = {
    'Content-Type': 'application/json',
    ...getAuthHeaders(),
  };

  const opts = { method, headers };
  if (body) {
    opts.body = JSON.stringify(body);
  }

  const res = await fetch(`${BASE_URL}${path}`, opts);
  const data = await res.json().catch(() => null);

  if (!res.ok) {
    const message = (data && data.error) || (data && data.message) || `Request failed (${res.status})`;
    throw new Error(message);
  }

  return data;
}

export function getProducts() {
  return request('GET', '/api/products');
}

export function register(email, password, name) {
  return request('POST', '/api/auth/register', { email, password, name });
}

export function login(email, password) {
  return request('POST', '/api/auth/login', { email, password });
}

export function getProfile() {
  return request('GET', '/api/auth/profile');
}

export function updateProfile(data) {
  return request('PUT', '/api/auth/profile', data);
}

export function getOrders() {
  return request('GET', '/api/orders');
}

export function createOrder(items, shipping) {
  return request('POST', '/api/orders', { items, shipping });
}

export function getOrder(id) {
  return request('GET', `/api/orders/${id}`);
}
