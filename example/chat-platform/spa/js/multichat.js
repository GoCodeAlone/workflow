import { api } from './api.js';
import {
  escapeHtml, showToast, showModal, hideModal, renderBadge,
  renderStatusBadge, renderRiskIndicator, formatTime, formatTimestamp,
  maskPhone, renderSpinner, requireAuth
} from './components.js';

const MAX_PANELS = 4;
const POLL_INTERVAL = 2500;
let openPanels = []; // Array of { id, pollInterval, lastMessageCount }
let listRefreshInterval = null;

function clearAllIntervals() {
  if (listRefreshInterval) {
    clearInterval(listRefreshInterval);
    listRefreshInterval = null;
  }
  openPanels.forEach(p => {
    if (p.pollInterval) clearInterval(p.pollInterval);
  });
  openPanels = [];
}

function renderMultiChat() {
  if (!requireAuth()) return '';
  const user = api.getUser();

  return `
    <div class="multi-chat-page-header">
      <h2>Multi-Chat</h2>
      <span class="text-muted" style="font-size:0.8rem">Up to ${MAX_PANELS} simultaneous conversations</span>
    </div>
    <div class="multi-chat-layout">
      <div class="multi-chat-list" id="multi-chat-list">
        <div class="multi-chat-list-header">
          <h3>Conversations</h3>
          <span class="badge badge-info" id="mc-panel-count">0/${MAX_PANELS}</span>
        </div>
        <div class="multi-chat-list-filter">
          <input class="form-input form-input-sm" id="mc-filter" placeholder="Filter..." />
        </div>
        <div class="multi-chat-list-items" id="mc-list-items">
          ${renderSpinner()}
        </div>
      </div>
      <div class="multi-chat-panels" id="multi-chat-panels">
        <div class="multi-chat-empty" id="mc-empty-state">
          <div class="empty-state">
            <h3>Multi-Chat View</h3>
            <p class="text-muted mt-1">Click conversations on the left to open them as panels.</p>
          </div>
        </div>
      </div>
    </div>
  `;
}

function handleMultiChat() {
  clearAllIntervals();
  loadConversationList();
  listRefreshInterval = setInterval(loadConversationList, 5000);

  const filterInput = document.getElementById('mc-filter');
  if (filterInput) {
    filterInput.addEventListener('input', () => {
      const term = filterInput.value.toLowerCase();
      const items = document.querySelectorAll('.mc-list-item');
      items.forEach(item => {
        const text = item.textContent.toLowerCase();
        item.style.display = text.includes(term) ? '' : 'none';
      });
    });
  }
}

async function loadConversationList() {
  const listEl = document.getElementById('mc-list-items');
  if (!listEl) return;

  try {
    const user = api.getUser();
    const affParam = user?.affiliateId ? `&affiliateId=${user.affiliateId}` : '';
    const progParam = user?.programIds?.length ? `&programId=${user.programIds.join(',')}` : '';
    const result = await api.get(`/api/conversations?role=responder${affParam}${progParam}`);
    const conversations = result.data || result || [];

    const active = conversations.filter(c => {
      const state = c.state || (c.data && c.data.state) || '';
      return !['closed', 'expired', 'failed'].includes(state);
    });

    if (active.length === 0) {
      listEl.innerHTML = '<div class="empty-state" style="padding:2rem"><p>No active conversations</p></div>';
      return;
    }

    listEl.innerHTML = active.map(c => {
      const data = c.data || c;
      const id = c.id || data.id;
      const state = c.state || data.state || 'active';
      const programName = data.programName || data.programId || '';
      const isOpen = openPanels.some(p => p.id === id);
      const risk = data.riskLevel || 'low';

      return `
        <div class="mc-list-item ${isOpen ? 'mc-list-item-active' : ''}" data-conv-id="${escapeHtml(id)}">
          <div class="mc-list-item-top">
            <span class="mc-list-item-phone">${maskPhone(data.texterPhone || data.from)}</span>
            <span class="mc-list-item-time">${formatTime(data.lastMessageAt || data.updatedAt || data.createdAt)}</span>
          </div>
          <div class="mc-list-item-mid">
            ${renderStatusBadge(state)}
            ${programName ? renderBadge(programName, 'info') : ''}
            ${renderRiskIndicator(risk)}
          </div>
          <div class="mc-list-item-preview">${escapeHtml(data.lastMessage || data.preview || '')}</div>
        </div>
      `;
    }).join('');

    listEl.querySelectorAll('.mc-list-item').forEach(item => {
      item.addEventListener('click', () => {
        const convId = item.dataset.convId;
        togglePanel(convId);
      });
    });
  } catch (err) {
    if (listEl.querySelector('.spinner-container')) {
      listEl.innerHTML = `<div class="empty-state"><p class="text-muted">Failed to load</p></div>`;
    }
  }
}

function togglePanel(convId) {
  const existing = openPanels.findIndex(p => p.id === convId);
  if (existing !== -1) {
    closePanel(convId);
    return;
  }

  if (openPanels.length >= MAX_PANELS) {
    showToast(`Maximum ${MAX_PANELS} chats open. Close one first.`, 'warning');
    return;
  }

  openPanel(convId);
}

function openPanel(convId) {
  const panelsEl = document.getElementById('multi-chat-panels');
  const emptyState = document.getElementById('mc-empty-state');
  if (!panelsEl) return;

  if (emptyState) emptyState.style.display = 'none';

  const panel = document.createElement('div');
  panel.className = 'mc-panel';
  panel.id = `mc-panel-${convId}`;
  panel.innerHTML = `
    <div class="mc-panel-topbar">
      <span class="mc-panel-id">${escapeHtml(convId.slice(-8))}</span>
      <span class="mc-panel-status" id="mc-status-${convId}"></span>
      <div class="mc-panel-actions">
        <button class="btn btn-sm btn-secondary mc-panel-expand" title="Open full view" data-conv-id="${escapeHtml(convId)}">&#8599;</button>
        <button class="btn btn-sm btn-secondary mc-panel-close" title="Close panel" data-conv-id="${escapeHtml(convId)}">&times;</button>
      </div>
    </div>
    <div class="mc-panel-messages" id="mc-msgs-${convId}">
      ${renderSpinner()}
    </div>
    <div class="mc-panel-input">
      <textarea class="mc-input" id="mc-input-${convId}" placeholder="Type..." rows="1"></textarea>
      <button class="mc-send-btn" id="mc-send-${convId}" title="Send">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <line x1="22" y1="2" x2="11" y2="13"/><polygon points="22 2 15 22 11 13 2 9 22 2"/>
        </svg>
      </button>
    </div>
  `;

  panelsEl.appendChild(panel);

  const panelData = { id: convId, pollInterval: null, lastMessageCount: 0 };
  openPanels.push(panelData);

  // Load initial data
  loadPanelData(convId);

  // Start polling
  panelData.pollInterval = setInterval(() => loadPanelMessages(convId), POLL_INTERVAL);

  // Wire up send
  const sendBtn = document.getElementById(`mc-send-${convId}`);
  const input = document.getElementById(`mc-input-${convId}`);
  if (sendBtn && input) {
    sendBtn.addEventListener('click', () => sendPanelMessage(convId));
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        sendPanelMessage(convId);
      }
    });
    input.addEventListener('input', () => {
      input.style.height = 'auto';
      input.style.height = Math.min(input.scrollHeight, 80) + 'px';
    });
  }

  // Wire up close/expand
  panel.querySelector('.mc-panel-close').addEventListener('click', (e) => {
    e.stopPropagation();
    closePanel(convId);
  });
  panel.querySelector('.mc-panel-expand').addEventListener('click', (e) => {
    e.stopPropagation();
    window.location.hash = `#/responder/chat/${convId}`;
  });

  updatePanelCount();
  updateListItemHighlights();
}

function closePanel(convId) {
  const idx = openPanels.findIndex(p => p.id === convId);
  if (idx === -1) return;

  if (openPanels[idx].pollInterval) {
    clearInterval(openPanels[idx].pollInterval);
  }
  openPanels.splice(idx, 1);

  const panel = document.getElementById(`mc-panel-${convId}`);
  if (panel) panel.remove();

  if (openPanels.length === 0) {
    const emptyState = document.getElementById('mc-empty-state');
    if (emptyState) emptyState.style.display = '';
  }

  updatePanelCount();
  updateListItemHighlights();
}

async function loadPanelData(convId) {
  try {
    const convo = await api.get(`/api/conversations/${convId}`);
    const data = convo.data || convo;
    const statusEl = document.getElementById(`mc-status-${convId}`);
    if (statusEl) {
      statusEl.innerHTML = renderStatusBadge(data.state || data.status || 'active');
    }
    await loadPanelMessages(convId);
  } catch (err) {
    showToast(`Failed to load ${convId}`, 'error');
  }
}

async function loadPanelMessages(convId) {
  const panelData = openPanels.find(p => p.id === convId);
  if (!panelData) return;

  try {
    const convo = await api.get(`/api/conversations/${convId}`);
    const data = convo.data || convo;
    const messages = data.messages || [];

    const container = document.getElementById(`mc-msgs-${convId}`);
    if (!container) return;

    // On first load (spinner visible), always render even if empty
    const isFirstLoad = container.querySelector('.spinner-container') || container.querySelector('.mc-no-msgs');

    if (messages.length === 0) {
      if (isFirstLoad || panelData.lastMessageCount === 0) {
        container.innerHTML = '<div class="mc-no-msgs">No messages yet</div>';
        panelData.lastMessageCount = 0;
      }
      return;
    }

    // Skip re-render if count unchanged (unless first load)
    if (!isFirstLoad && messages.length === panelData.lastMessageCount) return;
    panelData.lastMessageCount = messages.length;

    const wasAtBottom = container.scrollHeight - container.scrollTop - container.clientHeight < 50;

    container.innerHTML = messages.map(msg => {
      const dir = msg.direction || 'inbound';
      if (msg.type === 'system' || dir === 'system') {
        return `<div class="mc-msg mc-msg-system">${escapeHtml(msg.content || msg.body)}</div>`;
      }
      const cls = dir === 'outbound' ? 'mc-msg-out' : 'mc-msg-in';
      return `
        <div class="mc-msg ${cls}">
          ${escapeHtml(msg.content || msg.body)}
          <span class="mc-msg-time">${formatTimestamp(msg.timestamp || msg.createdAt)}</span>
        </div>
      `;
    }).join('');

    if (wasAtBottom) {
      container.scrollTop = container.scrollHeight;
    }
  } catch (err) {
    // Silently fail on poll
  }
}

async function sendPanelMessage(convId) {
  const input = document.getElementById(`mc-input-${convId}`);
  if (!input) return;
  const content = input.value.trim();
  if (!content) return;
  if (content.length > 5000) {
    showToast('Message too long', 'warning');
    return;
  }

  const sendBtn = document.getElementById(`mc-send-${convId}`);
  if (sendBtn) sendBtn.disabled = true;

  try {
    await api.post(`/api/conversations/${convId}/messages`, {
      content,
      direction: 'outbound'
    });
    input.value = '';
    input.style.height = 'auto';
    await loadPanelMessages(convId);
  } catch (err) {
    showToast('Send failed: ' + err.message, 'error');
  } finally {
    if (sendBtn) sendBtn.disabled = false;
    input.focus();
  }
}

function updatePanelCount() {
  const badge = document.getElementById('mc-panel-count');
  if (badge) badge.textContent = `${openPanels.length}/${MAX_PANELS}`;
}

function updateListItemHighlights() {
  document.querySelectorAll('.mc-list-item').forEach(item => {
    const convId = item.dataset.convId;
    const isOpen = openPanels.some(p => p.id === convId);
    item.classList.toggle('mc-list-item-active', isOpen);
  });
}

export function registerMultiChatRoutes(registerRoute) {
  registerRoute('responder/multi', renderMultiChat, handleMultiChat);
}

export function clearMultiChat() {
  clearAllIntervals();
}
