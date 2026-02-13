import { api } from './api.js';
import {
  escapeHtml, showToast, renderStatusBadge, renderStatusDot,
  renderLoadBar, renderRiskIndicator, formatTime, maskPhone,
  renderSpinner, requireAuth
} from './components.js';

let refreshInterval = null;

function clearRefresh() {
  if (refreshInterval) {
    clearInterval(refreshInterval);
    refreshInterval = null;
  }
}

function renderSupervisorDashboard() {
  if (!requireAuth()) return '';

  return `
    <div class="page">
      <div class="page-header">
        <div>
          <h1 class="page-title">Supervisor Overview</h1>
          <p class="page-subtitle">Monitor responders and conversations</p>
        </div>
      </div>
      <div class="metric-grid" id="supervisor-metrics">
        <div class="metric-card">
          <div class="metric-value text-warning" id="sup-metric-queued">--</div>
          <div class="metric-label">Total Queued</div>
        </div>
        <div class="metric-card">
          <div class="metric-value text-accent" id="sup-metric-active">--</div>
          <div class="metric-label">Active Conversations</div>
        </div>
        <div class="metric-card">
          <div class="metric-value text-info" id="sup-metric-responders">--</div>
          <div class="metric-label">Online Responders</div>
        </div>
        <div class="metric-card">
          <div class="metric-value text-danger" id="sup-metric-alerts">--</div>
          <div class="metric-label">Alerts</div>
        </div>
      </div>
      <h3 class="mb-2">Responders</h3>
      <div class="responder-grid" id="responder-grid">
        ${renderSpinner()}
      </div>
    </div>
  `;
}

function handleSupervisorDashboard() {
  clearRefresh();
  loadSupervisorData();
  refreshInterval = setInterval(loadSupervisorData, 5000);
}

async function loadSupervisorData() {
  try {
    const [usersResult, convosResult, healthResult] = await Promise.all([
      api.get('/api/users?role=responder'),
      api.get('/api/conversations?role=supervisor'),
      api.get('/api/queue/health').catch(() => ({ data: {} }))
    ]);

    const responders = usersResult.data || usersResult || [];
    const conversations = convosResult.data || convosResult || [];
    const health = healthResult.data || healthResult || {};

    const activeConvos = conversations.filter(c => {
      const state = c.state || (c.data && c.data.state) || '';
      return !['closed', 'expired', 'failed'].includes(state);
    });

    const queuedConvos = conversations.filter(c => {
      const state = c.state || (c.data && c.data.state) || '';
      return state === 'queued';
    });

    // Metrics
    const metricQueued = document.getElementById('sup-metric-queued');
    const metricActive = document.getElementById('sup-metric-active');
    const metricResponders = document.getElementById('sup-metric-responders');
    const metricAlerts = document.getElementById('sup-metric-alerts');

    if (metricQueued) metricQueued.textContent = queuedConvos.length;
    if (metricActive) metricActive.textContent = activeConvos.length;
    if (metricResponders) metricResponders.textContent = responders.filter(r => (r.data || r).status === 'active').length;
    if (metricAlerts) metricAlerts.textContent = health.alerts || 0;

    // Build a map of responder -> conversations
    const responderConvos = {};
    activeConvos.forEach(c => {
      const data = c.data || c;
      const rid = data.responderId || data.assignedTo;
      if (rid) {
        if (!responderConvos[rid]) responderConvos[rid] = [];
        responderConvos[rid].push(c);
      }
    });

    // Responder grid
    const grid = document.getElementById('responder-grid');
    if (grid) {
      if (responders.length === 0) {
        grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1"><p>No responders found</p></div>';
      } else {
        grid.innerHTML = responders.map(r => {
          const rd = r.data || r;
          const rid = r.id;
          const convos = responderConvos[rid] || [];
          const maxC = rd.maxConcurrent || 3;
          const status = rd.status === 'active' ? 'online' : 'offline';

          return `
            <div class="responder-card" data-responder-id="${escapeHtml(rid)}">
              <div class="responder-card-header">
                <span class="responder-card-name">${escapeHtml(rd.name)}</span>
                ${renderStatusDot(status)}
              </div>
              ${renderLoadBar(convos.length, maxC)}
              <div class="responder-card-convos mt-1">
                ${convos.length === 0 ? '<div class="text-muted" style="font-size:0.8rem">No active conversations</div>' : ''}
                ${convos.slice(0, 5).map(c => {
                  const cd = c.data || c;
                  const cid = c.id || cd.id;
                  const state = c.state || cd.state || 'active';
                  return `
                    <div class="mini-convo" data-conv-id="${escapeHtml(cid)}">
                      <span>${maskPhone(cd.texterPhone || cd.from)}</span>
                      <span>${renderStatusBadge(state)}</span>
                    </div>
                  `;
                }).join('')}
                ${convos.length > 5 ? `<div class="text-muted" style="font-size:0.75rem;text-align:center;margin-top:0.25rem">+${convos.length - 5} more</div>` : ''}
              </div>
            </div>
          `;
        }).join('');

        // Click handlers for responder cards
        grid.querySelectorAll('.responder-card').forEach(card => {
          card.addEventListener('click', (e) => {
            // If clicking a mini-convo, go to that chat
            const miniConvo = e.target.closest('.mini-convo');
            if (miniConvo) {
              const convId = miniConvo.dataset.convId;
              window.location.hash = `#/supervisor/chat/${convId}`;
              return;
            }
            const rid = card.dataset.responderId;
            window.location.hash = `#/supervisor/responder/${rid}`;
          });
        });
      }
    }
  } catch (err) {
    const grid = document.getElementById('responder-grid');
    if (grid && grid.querySelector('.spinner-container')) {
      grid.innerHTML = `<div class="empty-state" style="grid-column:1/-1"><p>Failed to load: ${escapeHtml(err.message)}</p></div>`;
    }
  }
}

function renderResponderDetail(responderId) {
  if (!requireAuth()) return '';

  return `
    <div class="page">
      <a href="#/supervisor" class="back-link">&#8592; Back to Overview</a>
      <div class="page-header">
        <div>
          <h1 class="page-title" id="responder-detail-name">Responder</h1>
          <p class="page-subtitle" id="responder-detail-subtitle"></p>
        </div>
      </div>
      <div class="dashboard-grid" id="responder-detail-convos">
        ${renderSpinner()}
      </div>
    </div>
  `;
}

function handleResponderDetail(responderId) {
  loadResponderDetail(responderId);
}

async function loadResponderDetail(responderId) {
  try {
    const [usersResult, convosResult] = await Promise.all([
      api.get('/api/users?role=responder'),
      api.get('/api/conversations?role=supervisor')
    ]);

    const responders = usersResult.data || usersResult || [];
    const conversations = convosResult.data || convosResult || [];

    const responder = responders.find(r => r.id === responderId);
    const rd = responder ? (responder.data || responder) : {};

    const nameEl = document.getElementById('responder-detail-name');
    if (nameEl) nameEl.textContent = rd.name || responderId;

    const subtitleEl = document.getElementById('responder-detail-subtitle');
    if (subtitleEl) subtitleEl.textContent = `${rd.email || ''} | ${rd.status || 'unknown'}`;

    const responderConvos = conversations.filter(c => {
      const cd = c.data || c;
      return (cd.responderId === responderId || cd.assignedTo === responderId) &&
        !['closed', 'expired', 'failed'].includes(c.state || cd.state);
    });

    const grid = document.getElementById('responder-detail-convos');
    if (grid) {
      if (responderConvos.length === 0) {
        grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1"><p>No active conversations for this responder</p></div>';
      } else {
        grid.innerHTML = responderConvos.map(c => {
          const cd = c.data || c;
          const cid = c.id || cd.id;
          const state = c.state || cd.state || 'active';
          const risk = cd.riskLevel || 'low';
          const time = cd.lastMessageAt || cd.updatedAt || cd.createdAt;

          return `
            <div class="conversation-card" data-conv-id="${escapeHtml(cid)}">
              <div class="conversation-card-header">
                <span class="conversation-card-id">${maskPhone(cd.texterPhone || cd.from)}</span>
                <span class="conversation-card-time">${formatTime(time)}</span>
              </div>
              <div class="conversation-card-preview">${escapeHtml(cd.lastMessage || cd.preview || '')}</div>
              <div class="conversation-card-footer">
                ${renderStatusBadge(state)}
                ${renderRiskIndicator(risk)}
              </div>
            </div>
          `;
        }).join('');

        grid.querySelectorAll('.conversation-card').forEach(card => {
          card.addEventListener('click', () => {
            const convId = card.dataset.convId;
            window.location.hash = `#/supervisor/chat/${convId}`;
          });
        });
      }
    }
  } catch (err) {
    showToast('Failed to load responder detail: ' + err.message, 'error');
  }
}

export function registerSupervisorRoutes(registerRoute) {
  registerRoute('supervisor', renderSupervisorDashboard, handleSupervisorDashboard);
  registerRoute('supervisor/responder/:id', renderResponderDetail, handleResponderDetail);
}

export { clearRefresh };
