import { api } from './api.js';
import {
  escapeHtml, showToast, formatTimestamp, renderSpinner, requireAuth
} from './components.js';

let dmPollInterval = null;
let currentDmPeerId = null;
let lastDmCount = 0;

export function clearDmPolling() {
  if (dmPollInterval) {
    clearInterval(dmPollInterval);
    dmPollInterval = null;
  }
  currentDmPeerId = null;
  lastDmCount = 0;
}

// --- DM Thread List view ---
function renderDmList() {
  if (!requireAuth()) return '';
  const user = api.getUser();
  return `
    <div class="page">
      <div class="page-header">
        <div>
          <h1 class="page-title">Direct Messages</h1>
          <p class="page-subtitle">Chat with team members</p>
        </div>
        <button class="btn btn-primary" id="dm-new-btn">New Message</button>
      </div>
      <div id="dm-thread-list">
        ${renderSpinner()}
      </div>
    </div>
  `;
}

function handleDmList() {
  clearDmPolling();
  loadDmThreads();
  dmPollInterval = setInterval(loadDmThreads, 5000);

  const newBtn = document.getElementById('dm-new-btn');
  if (newBtn) {
    newBtn.addEventListener('click', showNewDmPicker);
  }
}

async function loadDmThreads() {
  const container = document.getElementById('dm-thread-list');
  if (!container) return;

  try {
    const result = await api.get('/api/dm/threads');
    const threads = result.data || result || [];

    if (threads.length === 0) {
      container.innerHTML = `
        <div class="empty-state">
          <p>No conversations yet</p>
          <p class="text-muted" style="font-size:0.85rem">Start a direct message with a team member.</p>
        </div>
      `;
      return;
    }

    container.innerHTML = threads.map(t => {
      const peer = t.peer || {};
      const lastMsg = t.lastMessage || '';
      const time = t.lastMessageAt || '';
      const unread = t.unreadCount || 0;
      return `
        <div class="dm-thread-item" data-peer-id="${escapeHtml(peer.id || t.peerId)}">
          <div class="dm-thread-info">
            <div class="dm-thread-name">
              ${escapeHtml(peer.name || t.peerId)}
              ${peer.role ? `<span class="badge badge-neutral" style="font-size:0.65rem;margin-left:0.35rem">${escapeHtml(peer.role)}</span>` : ''}
            </div>
            <div class="dm-thread-preview">${escapeHtml(lastMsg)}</div>
          </div>
          <div class="dm-thread-meta">
            <span class="dm-thread-time">${formatTimestamp(time)}</span>
            ${unread > 0 ? `<span class="badge badge-accent badge-count">${unread}</span>` : ''}
          </div>
        </div>
      `;
    }).join('');

    container.querySelectorAll('.dm-thread-item').forEach(item => {
      item.addEventListener('click', () => {
        const peerId = item.dataset.peerId;
        const user = api.getUser();
        const base = user?.role === 'supervisor' ? 'supervisor' : 'responder';
        window.location.hash = `#/${base}/dm/${peerId}`;
      });
    });
  } catch (err) {
    if (container.querySelector('.spinner-container')) {
      container.innerHTML = `<div class="empty-state"><p class="text-muted">Failed to load: ${escapeHtml(err.message)}</p></div>`;
    }
  }
}

async function showNewDmPicker() {
  try {
    const user = api.getUser();
    const affParam = user?.affiliateId ? `?affiliateId=${user.affiliateId}` : '';
    const result = await api.get(`/api/users${affParam}`);
    const users = (result.data || result || []).filter(u => {
      const ud = u.data || u;
      return u.id !== user?.id && ud.status === 'active' &&
        (ud.role === 'responder' || ud.role === 'supervisor');
    });

    if (users.length === 0) {
      showToast('No other team members found', 'info');
      return;
    }

    const container = document.getElementById('dm-thread-list');
    if (!container) return;

    // Show user picker inline
    const pickerHtml = `
      <div class="dm-user-picker">
        <div class="dm-picker-header">
          <h3>Select a team member</h3>
          <button class="btn btn-sm btn-secondary" id="dm-picker-cancel">Cancel</button>
        </div>
        <input class="form-input form-input-sm" id="dm-picker-filter" placeholder="Filter by name..." style="margin-bottom:0.75rem"/>
        <div class="dm-picker-list" id="dm-picker-list">
          ${users.map(u => {
            const ud = u.data || u;
            return `
              <div class="dm-picker-item" data-user-id="${escapeHtml(u.id)}">
                <span class="dm-picker-name">${escapeHtml(ud.name)}</span>
                <span class="badge badge-neutral" style="font-size:0.65rem">${escapeHtml(ud.role)}</span>
              </div>
            `;
          }).join('')}
        </div>
      </div>
    `;
    container.innerHTML = pickerHtml;

    document.getElementById('dm-picker-cancel').addEventListener('click', () => {
      loadDmThreads();
    });

    document.getElementById('dm-picker-filter').addEventListener('input', (e) => {
      const term = e.target.value.toLowerCase();
      document.querySelectorAll('.dm-picker-item').forEach(item => {
        item.style.display = item.textContent.toLowerCase().includes(term) ? '' : 'none';
      });
    });

    document.querySelectorAll('.dm-picker-item').forEach(item => {
      item.addEventListener('click', () => {
        const peerId = item.dataset.userId;
        const base = user?.role === 'supervisor' ? 'supervisor' : 'responder';
        window.location.hash = `#/${base}/dm/${peerId}`;
      });
    });
  } catch (err) {
    showToast('Failed to load users: ' + err.message, 'error');
  }
}

// --- DM Conversation view ---
function renderDmChat(peerId) {
  if (!requireAuth()) return '';
  currentDmPeerId = peerId;

  return `
    <div class="chat-layout">
      <div class="chat-main">
        <div class="chat-topbar">
          <div class="chat-topbar-left">
            <a href="javascript:void(0)" class="back-link" id="dm-back-btn" style="margin-bottom:0">&#8592; Back</a>
            <span class="chat-topbar-id" id="dm-peer-name">Loading...</span>
            <span id="dm-peer-role"></span>
          </div>
        </div>
        <div class="chat-messages" id="dm-messages">
          ${renderSpinner()}
        </div>
        <div class="chat-input-area">
          <textarea class="chat-input" id="dm-input" placeholder="Type your message..." rows="1"></textarea>
          <button class="chat-send-btn" id="dm-send-btn" title="Send">
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <line x1="22" y1="2" x2="11" y2="13"/>
              <polygon points="22 2 15 22 11 13 2 9 22 2"/>
            </svg>
          </button>
        </div>
      </div>
    </div>
  `;
}

function handleDmChat(peerId) {
  clearDmPolling();
  currentDmPeerId = peerId;
  lastDmCount = 0;

  loadDmPeerInfo(peerId);
  loadDmMessages(peerId);
  dmPollInterval = setInterval(() => loadDmMessages(peerId), 2500);

  // Back button
  const backBtn = document.getElementById('dm-back-btn');
  if (backBtn) {
    backBtn.addEventListener('click', () => {
      const user = api.getUser();
      const base = user?.role === 'supervisor' ? 'supervisor' : 'responder';
      window.location.hash = `#/${base}/dm`;
    });
  }

  // Send
  const sendBtn = document.getElementById('dm-send-btn');
  const input = document.getElementById('dm-input');
  if (sendBtn && input) {
    sendBtn.addEventListener('click', () => sendDm(peerId));
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        sendDm(peerId);
      }
    });
    input.addEventListener('input', () => {
      input.style.height = 'auto';
      input.style.height = Math.min(input.scrollHeight, 120) + 'px';
    });
  }
}

async function loadDmPeerInfo(peerId) {
  try {
    const result = await api.get(`/api/users/${peerId}`);
    const user = result.data || result;
    const nameEl = document.getElementById('dm-peer-name');
    const roleEl = document.getElementById('dm-peer-role');
    if (nameEl) nameEl.textContent = user.name || peerId;
    if (roleEl && user.role) {
      roleEl.innerHTML = `<span class="badge badge-neutral" style="font-size:0.7rem">${escapeHtml(user.role)}</span>`;
    }
  } catch (err) {
    // Use peerId as fallback
    const nameEl = document.getElementById('dm-peer-name');
    if (nameEl) nameEl.textContent = peerId;
  }
}

async function loadDmMessages(peerId) {
  try {
    const result = await api.get(`/api/dm/${peerId}`);
    const messages = result.data || result.messages || result || [];

    const container = document.getElementById('dm-messages');
    if (!container) return;

    if (messages.length === 0 && lastDmCount === 0) {
      if (container.querySelector('.spinner-container') || container.querySelector('.empty-state')) {
        container.innerHTML = '<div class="empty-state"><p>No messages yet. Say hello!</p></div>';
      }
      return;
    }

    if (Array.isArray(messages) && messages.length === lastDmCount) return;
    lastDmCount = messages.length;

    const user = api.getUser();
    const wasAtBottom = container.scrollHeight - container.scrollTop - container.clientHeight < 50;

    container.innerHTML = messages.map(msg => {
      const isMine = msg.senderId === user?.id;
      const cls = isMine ? 'outbound' : 'inbound';
      return `
        <div class="message-bubble ${cls}">
          ${escapeHtml(msg.content || msg.body)}
          <div class="message-meta">
            ${formatTimestamp(msg.timestamp || msg.createdAt)}
          </div>
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

async function sendDm(peerId) {
  const input = document.getElementById('dm-input');
  if (!input) return;
  const content = input.value.trim();
  if (!content) return;
  if (content.length > 2000) {
    showToast('Message too long (max 2000 characters)', 'warning');
    return;
  }

  const sendBtn = document.getElementById('dm-send-btn');
  if (sendBtn) sendBtn.disabled = true;

  try {
    await api.post(`/api/dm/${peerId}`, { content });
    input.value = '';
    input.style.height = 'auto';
    await loadDmMessages(peerId);
  } catch (err) {
    showToast('Send failed: ' + err.message, 'error');
  } finally {
    if (sendBtn) sendBtn.disabled = false;
    input.focus();
  }
}

export function registerDmRoutes(registerRoute) {
  registerRoute('responder/dm', renderDmList, handleDmList);
  registerRoute('responder/dm/:id', renderDmChat, handleDmChat);
  registerRoute('supervisor/dm', renderDmList, handleDmList);
  registerRoute('supervisor/dm/:id', renderDmChat, handleDmChat);
}
