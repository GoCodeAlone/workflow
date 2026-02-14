import { api } from './api.js';
import {
  escapeHtml, showToast, renderBadge, formatTime,
  renderSpinner, requireAuth
} from './components.js';

let refreshInterval = null;

function clearRefresh() {
  if (refreshInterval) {
    clearInterval(refreshInterval);
    refreshInterval = null;
  }
}

function renderQueueHealth() {
  if (!requireAuth()) return '';
  const user = api.getUser();
  const isAdmin = user?.role === 'admin';
  const affiliateName = isAdmin ? 'All Affiliates' : (user?.affiliateName || user?.affiliateId || '');

  return `
    <div class="page">
      <div class="page-header">
        <div>
          <h1 class="page-title">Queue Health${affiliateName ? ` <span style="font-size:0.6em;color:var(--text-muted);font-weight:normal">— ${escapeHtml(affiliateName)}</span>` : ''}</h1>
          <p class="page-subtitle">Real-time queue monitoring${user?.role === 'admin' ? ' across all programs' : ' for your programs'}</p>
        </div>
      </div>
      <div class="metric-grid" id="queue-global-metrics">
        <div class="metric-card">
          <div class="metric-value text-warning" id="queue-total">--</div>
          <div class="metric-label">Total Queued</div>
        </div>
        <div class="metric-card">
          <div class="metric-value text-info" id="queue-avg-wait">--</div>
          <div class="metric-label">Avg Wait Time</div>
        </div>
        <div class="metric-card">
          <div class="metric-value text-danger" id="queue-oldest">--</div>
          <div class="metric-label">Oldest Message</div>
        </div>
        <div class="metric-card">
          <div class="metric-value text-accent" id="queue-active-programs">--</div>
          <div class="metric-label">Active Programs</div>
        </div>
      </div>
      <h3 class="mb-2">Per-Program Queues</h3>
      <div class="queue-grid" id="queue-program-grid">
        ${renderSpinner()}
      </div>
    </div>
  `;
}

function handleQueueHealth() {
  clearRefresh();
  loadQueueData();
  refreshInterval = setInterval(loadQueueData, 5000);
}

async function loadQueueData() {
  try {
    const user = api.getUser();
    const affParam = user?.affiliateId ? `?affiliateId=${user.affiliateId}` : '';
    const result = await api.get(`/api/queue/health${affParam}`);
    const health = result.data || result || {};

    const programs = health.programs || health.queues || [];
    const totalQueued = programs.reduce((sum, p) => sum + (p.depth || p.queued || 0), 0);
    const avgWaitSecs = programs.length > 0
      ? Math.round(programs.reduce((sum, p) => sum + (p.avgWaitSeconds || 0), 0) / programs.length)
      : 0;

    let oldestMsg = null;
    programs.forEach(p => {
      if (p.oldestMessageAt) {
        const t = new Date(p.oldestMessageAt);
        if (!oldestMsg || t < oldestMsg) oldestMsg = t;
      }
    });

    // Update global metrics
    const elTotal = document.getElementById('queue-total');
    const elAvg = document.getElementById('queue-avg-wait');
    const elOldest = document.getElementById('queue-oldest');
    const elActive = document.getElementById('queue-active-programs');

    if (elTotal) elTotal.textContent = totalQueued;
    if (elAvg) elAvg.textContent = formatWaitTime(avgWaitSecs);
    if (elOldest) elOldest.textContent = oldestMsg ? formatTime(oldestMsg.toISOString()) : 'N/A';
    if (elActive) elActive.textContent = programs.filter(p => (p.depth || p.queued || 0) > 0).length;

    // Program grid
    const grid = document.getElementById('queue-program-grid');
    if (grid) {
      if (programs.length === 0) {
        grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1"><p>No queue data available</p></div>';
      } else {
        grid.innerHTML = programs.map(p => {
          const depth = p.depth || p.queued || 0;
          const avgWait = p.avgWaitSeconds || 0;
          const threshold = p.alertThreshold || 10;
          const isAlert = depth >= threshold;

          return `
            <div class="queue-card">
              <div class="queue-card-header">
                <span class="queue-card-name">${escapeHtml(p.programName || p.programId || 'Unknown')}</span>
                ${isAlert ? renderBadge('ALERT', 'danger') : renderBadge(`${depth} queued`, depth > 0 ? 'warning' : 'success')}
              </div>
              <div class="queue-stat">
                <span class="queue-stat-label">Queue Depth</span>
                <span class="queue-stat-value ${depth >= threshold ? 'text-danger' : ''}">${depth}</span>
              </div>
              <div class="queue-stat">
                <span class="queue-stat-label">Avg Wait</span>
                <span class="queue-stat-value">${formatWaitTime(avgWait)}</span>
              </div>
              <div class="queue-stat">
                <span class="queue-stat-label">Oldest</span>
                <span class="queue-stat-value">${p.oldestMessageAt ? formatTime(p.oldestMessageAt) : 'N/A'}</span>
              </div>
              <div class="queue-stat">
                <span class="queue-stat-label">Threshold</span>
                <span class="queue-stat-value">${threshold}</span>
              </div>
              ${isAlert ? `
                <div class="queue-alert">
                  &#9888; Queue depth exceeds threshold (${depth} >= ${threshold})
                </div>
              ` : ''}
            </div>
          `;
        }).join('');
      }
    }
  } catch (err) {
    const grid = document.getElementById('queue-program-grid');
    if (grid && grid.querySelector('.spinner-container')) {
      grid.innerHTML = `<div class="empty-state" style="grid-column:1/-1"><p>Failed to load: ${escapeHtml(err.message)}</p></div>`;
    }
  }
}

function formatWaitTime(seconds) {
  if (!seconds || seconds <= 0) return '0s';
  if (seconds < 60) return `${seconds}s`;
  const mins = Math.floor(seconds / 60);
  const secs = seconds % 60;
  if (mins < 60) return `${mins}m ${secs}s`;
  const hours = Math.floor(mins / 60);
  return `${hours}h ${mins % 60}m`;
}

export function registerQueueRoutes(registerRoute) {
  registerRoute('queue', renderQueueHealth, handleQueueHealth);
}

export { clearRefresh };
