import { api } from './api.js';
import { renderNav, requireAuth } from './components.js';
import { registerAuthRoutes } from './auth.js';
import { registerResponderRoutes, clearRefresh as clearResponderRefresh } from './responder.js';
import { registerSupervisorRoutes, clearRefresh as clearSupervisorRefresh } from './supervisor.js';
import { registerAdminRoutes } from './admin.js';
import { registerQueueRoutes, clearRefresh as clearQueueRefresh } from './queue.js';
import { renderChat, handleChat, clearPolling } from './chat.js';

const routes = {};

function registerRoute(pattern, renderFn, handlerFn) {
  routes[pattern] = { render: renderFn, handler: handlerFn };
}

// Register all module routes
registerAuthRoutes(registerRoute);
registerResponderRoutes(registerRoute);
registerSupervisorRoutes(registerRoute);
registerAdminRoutes(registerRoute);
registerQueueRoutes(registerRoute);

function matchRoute(hash) {
  const path = (hash || '#/login').slice(1).replace(/^\//, '');
  const parts = path.split('/').filter(Boolean);

  // Direct match first
  if (routes[path]) {
    return { route: routes[path], params: {} };
  }

  // Pattern matching with :param extraction
  for (const [pattern, route] of Object.entries(routes)) {
    const patternParts = pattern.split('/').filter(Boolean);
    if (patternParts.length !== parts.length) continue;

    const params = {};
    let match = true;
    for (let i = 0; i < patternParts.length; i++) {
      if (patternParts[i].startsWith(':')) {
        params[patternParts[i].slice(1)] = parts[i];
      } else if (patternParts[i] !== parts[i]) {
        match = false;
        break;
      }
    }
    if (match) return { route, params };
  }

  return null;
}

function clearAllTimers() {
  clearPolling();
  clearResponderRefresh();
  clearSupervisorRefresh();
  clearQueueRefresh();
}

function navigate() {
  clearAllTimers();

  const hash = window.location.hash || '#/login';
  const nav = document.getElementById('nav');
  const main = document.getElementById('main');
  if (!nav || !main) return;

  const user = api.getUser();
  nav.innerHTML = renderNav(user);

  // Special case: chat routes
  const chatMatch = hash.match(/^#\/responder\/chat\/(.+)$/);
  if (chatMatch) {
    if (!requireAuth()) return;
    const convId = chatMatch[1];
    main.innerHTML = renderChat(convId, false);
    handleChat(convId, false);
    return;
  }

  const supervisorChatMatch = hash.match(/^#\/supervisor\/chat\/(.+)$/);
  if (supervisorChatMatch) {
    if (!requireAuth()) return;
    const convId = supervisorChatMatch[1];
    main.innerHTML = renderChat(convId, true);
    handleChat(convId, true);
    return;
  }

  // Handle #/ or empty hash
  if (hash === '#/' || hash === '#') {
    if (user) {
      const role = user.role || 'responder';
      if (role === 'admin') {
        window.location.hash = '#/admin/affiliates';
      } else if (role === 'supervisor') {
        window.location.hash = '#/supervisor';
      } else {
        window.location.hash = '#/responder';
      }
    } else {
      window.location.hash = '#/login';
    }
    return;
  }

  // Match registered routes
  const matched = matchRoute(hash);
  if (matched) {
    const { route, params } = matched;
    const paramValues = Object.values(params);

    // Auth guard for non-login routes
    const path = hash.slice(1).replace(/^\//, '');
    if (path !== 'login' && !requireAuth()) return;

    const html = route.render(...paramValues);
    if (html !== undefined && html !== null) {
      main.innerHTML = html;
    }
    if (route.handler) {
      route.handler(...paramValues);
    }
    return;
  }

  // 404
  main.innerHTML = `
    <div class="page">
      <div class="empty-state" style="padding-top: 4rem;">
        <h2>Page Not Found</h2>
        <p class="text-muted mt-1">The page you are looking for does not exist.</p>
        <a href="#/" class="btn btn-primary mt-2">Go Home</a>
      </div>
    </div>
  `;
}

window.addEventListener('hashchange', navigate);
window.addEventListener('DOMContentLoaded', navigate);
