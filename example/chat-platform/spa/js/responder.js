import { api } from './api.js';
import {
  escapeHtml, showToast, renderBadge, renderStatusBadge,
  renderRiskIndicator, formatTime, maskPhone, renderSpinner, requireAuth
} from './components.js';

let refreshInterval = null;

function clearRefresh() {
  if (refreshInterval) {
    clearInterval(refreshInterval);
    refreshInterval = null;
  }
}

function renderDashboard() {
  if (!requireAuth()) return '';
  const user = api.getUser();

  return `
    <div class="page">
      <div class="page-header">
        <div>
          <h1 class="page-title">Welcome, ${escapeHtml(user?.name || 'Responder')}</h1>
          <p class="page-subtitle" id="dashboard-subtitle">Loading conversations...</p>
        </div>
        <div class="flex gap-1 items-center">
          <span id="queue-badge"></span>
          <button class="btn btn-primary" id="pick-queue-btn">Pick from Queue</button>
        </div>
      </div>
      <div class="metric-grid" id="dashboard-metrics">
        <div class="metric-card">
          <div class="metric-value text-accent" id="metric-active">--</div>
          <div class="metric-label">Active Conversations</div>
        </div>
        <div class="metric-card">
          <div class="metric-value text-warning" id="metric-queue">--</div>
          <div class="metric-label">In Queue</div>
        </div>
        <div class="metric-card">
          <div class="metric-value text-success" id="metric-closed">--</div>
          <div class="metric-label">Closed Today</div>
        </div>
        <div class="metric-card">
          <div class="metric-value text-info" id="metric-max">--</div>
          <div class="metric-label">Max Concurrent</div>
        </div>
      </div>
      <h3 class="mb-2">Active Conversations</h3>
      <div class="dashboard-grid" id="conversation-grid">
        ${renderSpinner()}
      </div>
    </div>
  `;
}

function handleDashboard() {
  clearRefresh();
  loadDashboardData();
  refreshInterval = setInterval(loadDashboardData, 5000);

  const pickBtn = document.getElementById('pick-queue-btn');
  if (pickBtn) {
    pickBtn.addEventListener('click', pickFromQueue);
  }
}

async function loadDashboardData() {
  try {
    const user = api.getUser();
    const affParam = user?.affiliateId ? `&affiliateId=${user.affiliateId}` : '';
    const progParam = user?.programIds?.length ? `&programId=${user.programIds.join(',')}` : '';
    const [convosResult, queueResult] = await Promise.all([
      api.get(`/api/conversations?role=responder${affParam}${progParam}`),
      api.get(`/api/queue${affParam ? '?' + affParam.slice(1) : ''}`)
    ]);

    const conversations = convosResult.data || convosResult || [];
    const queue = queueResult.data || queueResult || {};

    const active = conversations.filter(c => {
      const state = c.state || (c.data && c.data.state) || '';
      return !['closed', 'expired', 'failed'].includes(state);
    });
    const closedToday = conversations.filter(c => {
      const state = c.state || (c.data && c.data.state) || '';
      return state === 'closed';
    });

    const maxConcurrent = user?.maxConcurrent || 3;
    const queueCount = queue.totalQueued || queue.count || 0;

    // Update metrics
    const metricActive = document.getElementById('metric-active');
    const metricQueue = document.getElementById('metric-queue');
    const metricClosed = document.getElementById('metric-closed');
    const metricMax = document.getElementById('metric-max');
    if (metricActive) metricActive.textContent = active.length;
    if (metricQueue) metricQueue.textContent = queueCount;
    if (metricClosed) metricClosed.textContent = closedToday.length;
    if (metricMax) metricMax.textContent = maxConcurrent;

    // Subtitle
    const subtitle = document.getElementById('dashboard-subtitle');
    if (subtitle) {
      if (active.length >= maxConcurrent) {
        subtitle.textContent = `${active.length} active — at capacity (max ${maxConcurrent})`;
      } else {
        subtitle.textContent = `${active.length} active, ${maxConcurrent - active.length} slots available`;
      }
    }

    // Queue badge
    const queueBadge = document.getElementById('queue-badge');
    if (queueBadge && queueCount > 0) {
      queueBadge.innerHTML = `<span class="badge badge-warning badge-count">${queueCount}</span>`;
    } else if (queueBadge) {
      queueBadge.innerHTML = '';
    }

    // Conversation grid
    const grid = document.getElementById('conversation-grid');
    if (grid) {
      if (active.length === 0) {
        grid.innerHTML = `
          <div class="empty-state" style="grid-column: 1/-1;">
            <p>No active conversations</p>
            <button class="btn btn-primary" id="pick-queue-btn-empty">Pick from Queue</button>
          </div>
        `;
        const pickEmpty = document.getElementById('pick-queue-btn-empty');
        if (pickEmpty) pickEmpty.addEventListener('click', pickFromQueue);
      } else {
        grid.innerHTML = active.map(c => {
          const data = c.data || c;
          const id = c.id || data.id;
          const lastMsg = data.lastMessage || data.preview || '';
          const time = data.lastMessageAt || data.updatedAt || data.createdAt;
          const tags = data.tags || [];
          const risk = data.riskLevel || 'low';
          const state = c.state || data.state || 'active';
          const programName = data.programName || data.programId || '';

          return `
            <div class="conversation-card" data-conv-id="${escapeHtml(id)}">
              <div class="conversation-card-header">
                <span class="conversation-card-id">${maskPhone(data.texterPhone || data.from)}</span>
                <span class="conversation-card-time">${formatTime(time)}</span>
              </div>
              ${programName ? `<div style="margin-bottom:0.25rem">${renderBadge(programName, 'info')}</div>` : ''}
              <div class="conversation-card-preview">${escapeHtml(lastMsg)}</div>
              <div class="conversation-card-footer">
                <div class="conversation-card-tags">
                  ${renderStatusBadge(state)}
                  ${tags.slice(0, 2).map(t => `<span class="tag">${escapeHtml(t)}</span>`).join('')}
                </div>
                ${renderRiskIndicator(risk)}
              </div>
            </div>
          `;
        }).join('');

        grid.querySelectorAll('.conversation-card').forEach(card => {
          card.addEventListener('click', () => {
            const convId = card.dataset.convId;
            window.location.hash = `#/responder/chat/${convId}`;
          });
        });
      }
    }
  } catch (err) {
    // Silently fail on refresh
    const grid = document.getElementById('conversation-grid');
    if (grid && grid.querySelector('.spinner-container')) {
      grid.innerHTML = `<div class="empty-state" style="grid-column:1/-1"><p>Failed to load: ${escapeHtml(err.message)}</p></div>`;
    }
  }
}

async function pickFromQueue() {
  try {
    const queueResult = await api.get('/api/queue');
    const queued = queueResult.data || queueResult.conversations || [];
    if (Array.isArray(queued) && queued.length > 0) {
      const first = queued[0];
      const convId = first.id || first.conversationId;
      await api.post(`/api/conversations/${convId}/assign`, {});
      showToast('Conversation assigned to you', 'success');
      window.location.hash = `#/responder/chat/${convId}`;
    } else {
      showToast('No conversations in queue', 'info');
    }
  } catch (err) {
    showToast('Failed to pick from queue: ' + err.message, 'error');
  }
}

export function registerResponderRoutes(registerRoute) {
  registerRoute('responder', renderDashboard, handleDashboard);
}

export { clearRefresh };
