const API_BASE = '';

export function getToken() {
  return localStorage.getItem('token');
}

export function getUser() {
  try {
    return JSON.parse(localStorage.getItem('user'));
  } catch {
    return null;
  }
}

export function setAuth(token, user) {
  localStorage.setItem('token', token);
  localStorage.setItem('user', JSON.stringify(user));
}

export function clearAuth() {
  localStorage.removeItem('token');
  localStorage.removeItem('user');
}

export function isLoggedIn() {
  return !!getToken();
}

async function request(method, path, body) {
  const headers = { 'Content-Type': 'application/json' };
  const token = getToken();
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  const opts = { method, headers };
  if (body !== undefined && body !== null) {
    opts.body = JSON.stringify(body);
  }

  const res = await fetch(`${API_BASE}${path}`, opts);
  const data = await res.json().catch(() => null);

  if (!res.ok) {
    if (res.status === 401) {
      clearAuth();
      window.location.hash = '#/login';
    }
    const message = (data && data.error) || (data && data.message) || `Request failed (${res.status})`;
    throw new Error(message);
  }

  return data;
}

function get(path) {
  return request('GET', path);
}

function post(path, body) {
  return request('POST', path, body);
}

function put(path, body) {
  return request('PUT', path, body);
}

function del(path) {
  return request('DELETE', path);
}

export const api = { get, post, put, del, getUser, getToken, isLoggedIn, setAuth, clearAuth };
