import { api } from './api.js';
import {
  escapeHtml, showToast, showModal, hideModal, renderBadge,
  renderStatusBadge, renderRiskIndicator, formatTime, formatTimestamp,
  maskPhone, renderSpinner
} from './components.js';
import { renderResourcesPanel, wireResourcesPanel } from './resources.js';

let pollInterval = null;
let lastMessageCount = 0;

export function clearPolling() {
  if (pollInterval) {
    clearInterval(pollInterval);
    pollInterval = null;
  }
  lastMessageCount = 0;
}

export function renderChat(conversationId, readOnly) {
  return `
    <div class="chat-layout">
      <div class="chat-main">
        <div class="chat-topbar">
          <div class="chat-topbar-left">
            <a href="javascript:void(0)" class="back-link" id="chat-back-btn" style="margin-bottom:0">&#8592; Back</a>
            <span class="chat-topbar-id" id="chat-conv-id">${escapeHtml(conversationId)}</span>
            <span id="chat-status-badge"></span>
          </div>
          <div class="chat-topbar-right">
            ${readOnly ? '<span class="badge badge-neutral">Read Only</span>' : `
              <div class="dropdown">
                <button class="btn btn-secondary btn-sm" id="chat-actions-btn">Actions &#9662;</button>
                <div class="dropdown-menu" id="chat-actions-menu">
                  <button class="dropdown-item" data-action="transfer">Transfer</button>
                  <button class="dropdown-item" data-action="escalate-medical">Escalate Medical</button>
                  <button class="dropdown-item danger" data-action="escalate-police">Escalate Police</button>
                  <div class="dropdown-divider"></div>
                  <button class="dropdown-item" data-action="tag">Tag Conversation</button>
                  <button class="dropdown-item" data-action="survey">Send Survey</button>
                  <div class="dropdown-divider"></div>
                  <button class="dropdown-item" data-action="wrap-up">Wrap Up</button>
                  <button class="dropdown-item" data-action="follow-up">Schedule Follow-up</button>
                  <button class="dropdown-item" data-action="close">Close Conversation</button>
                </div>
              </div>
            `}
          </div>
        </div>
        <div class="chat-messages" id="chat-messages">
          ${renderSpinner()}
        </div>
        ${readOnly ? '' : `
          <div class="chat-input-area">
            <textarea class="chat-input" id="chat-input" placeholder="Type your message..." rows="1"></textarea>
            <button class="chat-send-btn" id="chat-send-btn" title="Send">
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <line x1="22" y1="2" x2="11" y2="13"/>
                <polygon points="22 2 15 22 11 13 2 9 22 2"/>
              </svg>
            </button>
          </div>
        `}
      </div>
      <div class="chat-sidebar" id="chat-sidebar">
        <div class="chat-sidebar-section" id="sidebar-transfer-info" style="display:none">
          <h4>Transfer Info</h4>
          <div id="sidebar-transfer-content"></div>
        </div>
        <div class="chat-sidebar-section">
          <h4>Texter Info</h4>
          <div id="sidebar-texter-info">${renderSpinner()}</div>
        </div>
        <div class="chat-sidebar-section">
          <h4>Tags</h4>
          <div id="sidebar-tags" class="sidebar-tags"></div>
        </div>
        <div class="chat-sidebar-section">
          <h4>Risk Assessment</h4>
          <div id="sidebar-risk"></div>
        </div>
        <div class="chat-sidebar-section">
          <h4>AI Summary</h4>
          <div class="ai-summary-toggle" id="ai-summary-toggle">&#9654; Load AI Summary</div>
          <div class="ai-summary-content hidden" id="ai-summary-content"></div>
        </div>
        ${readOnly ? '' : renderResourcesPanel()}
      </div>
    </div>
  `;
}

export function handleChat(conversationId, readOnly) {
  clearPolling();
  loadConversation(conversationId, readOnly);

  // Poll for messages
  pollInterval = setInterval(() => {
    loadMessages(conversationId);
  }, 2000);

  // Actions dropdown toggle
  const actionsBtn = document.getElementById('chat-actions-btn');
  const actionsMenu = document.getElementById('chat-actions-menu');
  if (actionsBtn && actionsMenu) {
    actionsBtn.addEventListener('click', (e) => {
      e.stopPropagation();
      actionsMenu.classList.toggle('open');
    });
    document.addEventListener('click', () => {
      actionsMenu.classList.remove('open');
    });

    actionsMenu.querySelectorAll('.dropdown-item').forEach(item => {
      item.addEventListener('click', (e) => {
        actionsMenu.classList.remove('open');
        handleAction(e.target.dataset.action, conversationId);
      });
    });
  }

  // Send message
  if (!readOnly) {
    const sendBtn = document.getElementById('chat-send-btn');
    const input = document.getElementById('chat-input');

    if (sendBtn && input) {
      sendBtn.addEventListener('click', () => sendMessage(conversationId));
      input.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
          e.preventDefault();
          sendMessage(conversationId);
        }
      });
      // Auto-resize textarea
      input.addEventListener('input', () => {
        input.style.height = 'auto';
        input.style.height = Math.min(input.scrollHeight, 120) + 'px';
      });
    }
  }

  // AI Summary toggle
  const summaryToggle = document.getElementById('ai-summary-toggle');
  if (summaryToggle) {
    summaryToggle.addEventListener('click', () => loadAiSummary(conversationId));
  }

  // Wire shared resources panel (insert text into chat input)
  if (!readOnly) {
    wireResourcesPanel((text) => {
      const chatInput = document.getElementById('chat-input');
      if (chatInput) {
        chatInput.value = text;
        chatInput.style.height = 'auto';
        chatInput.style.height = Math.min(chatInput.scrollHeight, 120) + 'px';
        chatInput.focus();
      }
    });
  }

  // Back button
  const backBtn = document.getElementById('chat-back-btn');
  if (backBtn) {
    backBtn.addEventListener('click', () => {
      const user = api.getUser();
      if (user && user.role === 'supervisor') {
        window.location.hash = '#/supervisor';
      } else {
        window.location.hash = '#/responder';
      }
    });
  }
}

async function loadConversation(conversationId, readOnly) {
  try {
    const convo = await api.get(`/api/conversations/${conversationId}`);
    const data = convo.data || convo;

    // Status badge
    const statusEl = document.getElementById('chat-status-badge');
    if (statusEl) {
      statusEl.innerHTML = renderStatusBadge(data.state || data.status || 'active');
    }

    // Texter info
    const texterInfo = document.getElementById('sidebar-texter-info');
    if (texterInfo) {
      texterInfo.innerHTML = `
        <div class="sidebar-field">
          <span class="sidebar-field-label">Phone</span>
          <span class="sidebar-field-value">${maskPhone(data.texterPhone || data.from)}</span>
        </div>
        <div class="sidebar-field">
          <span class="sidebar-field-label">Session Start</span>
          <span class="sidebar-field-value">${formatTime(data.createdAt || data.startTime)}</span>
        </div>
        <div class="sidebar-field">
          <span class="sidebar-field-label">Program</span>
          <span class="sidebar-field-value">${escapeHtml(data.programName || data.programId || 'N/A')}</span>
        </div>
        <div class="sidebar-field">
          <span class="sidebar-field-label">Provider</span>
          <span class="sidebar-field-value">${escapeHtml(data.provider || 'N/A')}</span>
        </div>
      `;
    }

    // Tags
    const tagsEl = document.getElementById('sidebar-tags');
    if (tagsEl) {
      const tags = data.tags || [];
      tagsEl.innerHTML = tags.length > 0
        ? tags.map(t => `<span class="tag">${escapeHtml(t)}</span>`).join('')
        : '<span class="text-muted" style="font-size:0.8rem">No tags</span>';
    }

    // Risk
    const riskEl = document.getElementById('sidebar-risk');
    if (riskEl) {
      riskEl.innerHTML = renderRiskIndicator(data.riskLevel || 'low');
    }

    // Transfer handoff info
    const transferSection = document.getElementById('sidebar-transfer-info');
    const transferContent = document.getElementById('sidebar-transfer-content');
    if (transferSection && transferContent && data.transferredFrom) {
      transferSection.style.display = '';
      transferContent.innerHTML = `
        <div class="transfer-handoff-banner">
          <div class="transfer-handoff-label">Transferred from</div>
          <div class="transfer-handoff-name">${escapeHtml(data.transferredFrom.name || data.transferredFrom.id || 'Unknown')}</div>
          ${data.transferredFrom.note ? `<div class="transfer-handoff-note">${escapeHtml(data.transferredFrom.note)}</div>` : ''}
          ${data.transferredFrom.timestamp ? `<div class="transfer-handoff-time">${formatTime(data.transferredFrom.timestamp)}</div>` : ''}
        </div>
      `;
    }

    // Load messages
    await loadMessages(conversationId);
  } catch (err) {
    showToast('Failed to load conversation: ' + err.message, 'error');
  }
}

async function loadMessages(conversationId) {
  try {
    const convo = await api.get(`/api/conversations/${conversationId}`);
    const data = convo.data || convo;
    const messages = data.messages || [];

    const container = document.getElementById('chat-messages');
    if (!container) return;

    // Clear spinner on first load even if messages are empty
    if (messages.length === 0 && lastMessageCount === 0) {
      if (container.querySelector('.spinner-container')) {
        container.innerHTML = '<div class="empty-state"><p>No messages yet</p></div>';
      }
      return;
    }

    if (messages.length === lastMessageCount) return;
    lastMessageCount = messages.length;

    const wasAtBottom = container.scrollHeight - container.scrollTop - container.clientHeight < 50;

    container.innerHTML = messages.map(msg => {
      const dir = msg.direction || 'inbound';
      if (msg.type === 'transfer' || (msg.type === 'system' && (msg.content || msg.body || '').toLowerCase().includes('transfer'))) {
        const text = msg.content || msg.body;
        return `
          <div class="message-bubble system transfer-indicator">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="vertical-align:middle;margin-right:4px">
              <polyline points="15 14 20 9 15 4"/><path d="M4 20v-7a4 4 0 0 1 4-4h12"/>
            </svg>
            ${escapeHtml(text)}
          </div>
        `;
      }
      if (msg.type === 'system' || dir === 'system') {
        return `<div class="message-bubble system">${escapeHtml(msg.content || msg.body)}</div>`;
      }
      const cls = dir === 'outbound' ? 'outbound' : 'inbound';
      const statusIcon = msg.status === 'delivered' ? ' &#10003;&#10003;' : msg.status === 'sent' ? ' &#10003;' : '';
      return `
        <div class="message-bubble ${cls}">
          ${escapeHtml(msg.content || msg.body)}
          <div class="message-meta">
            ${formatTimestamp(msg.timestamp || msg.createdAt)}
            <span class="message-status">${statusIcon}</span>
          </div>
        </div>
      `;
    }).join('');

    if (wasAtBottom || lastMessageCount <= messages.length) {
      container.scrollTop = container.scrollHeight;
    }
  } catch (err) {
    // Silently fail on polling errors
  }
}

const MAX_MESSAGE_LENGTH = 5000;

async function sendMessage(conversationId) {
  const input = document.getElementById('chat-input');
  if (!input) return;

  const content = input.value.trim();
  if (!content) return;

  if (content.length > MAX_MESSAGE_LENGTH) {
    showToast(`Message too long (${content.length}/${MAX_MESSAGE_LENGTH} characters)`, 'warning');
    return;
  }

  const sendBtn = document.getElementById('chat-send-btn');
  if (sendBtn) sendBtn.disabled = true;

  try {
    await api.post(`/api/conversations/${conversationId}/messages`, {
      body: content,
      direction: 'outbound'
    });
    input.value = '';
    input.style.height = 'auto';
    await loadMessages(conversationId);
  } catch (err) {
    showToast('Failed to send message: ' + err.message, 'error');
  } finally {
    if (sendBtn) sendBtn.disabled = false;
    input.focus();
  }
}

async function loadAiSummary(conversationId) {
  const toggle = document.getElementById('ai-summary-toggle');
  const content = document.getElementById('ai-summary-content');
  if (!toggle || !content) return;

  if (!content.classList.contains('hidden')) {
    content.classList.add('hidden');
    toggle.innerHTML = '&#9654; Load AI Summary';
    return;
  }

  toggle.innerHTML = '&#9660; Loading...';
  content.classList.remove('hidden');
  content.innerHTML = renderSpinner();

  try {
    const result = await api.get(`/api/conversations/${conversationId}/summary`);
    const data = result.data || result;
    content.innerHTML = `
      <p>${escapeHtml(data.summary || 'No summary available.')}</p>
      ${data.keyTopics && data.keyTopics.length > 0 ? `
        <div class="mt-1">
          <strong style="font-size:0.8rem;color:var(--text-muted)">Key Topics:</strong>
          <div class="sidebar-tags mt-05">
            ${data.keyTopics.map(t => `<span class="tag">${escapeHtml(t)}</span>`).join('')}
          </div>
        </div>
      ` : ''}
      ${data.sentiment ? `
        <div class="mt-1">
          <strong style="font-size:0.8rem;color:var(--text-muted)">Sentiment:</strong>
          <span style="font-size:0.85rem"> ${escapeHtml(data.sentiment)}</span>
        </div>
      ` : ''}
    `;
    toggle.innerHTML = '&#9660; AI Summary';
  } catch (err) {
    content.innerHTML = `<p class="text-muted">Failed to load summary: ${escapeHtml(err.message)}</p>`;
    toggle.innerHTML = '&#9660; AI Summary';
  }
}

function handleAction(action, conversationId) {
  switch (action) {
    case 'transfer':
      showTransferModal(conversationId);
      break;
    case 'escalate-medical':
      showEscalateModal(conversationId, 'medical');
      break;
    case 'escalate-police':
      showEscalateModal(conversationId, 'police');
      break;
    case 'tag':
      showTagModal(conversationId);
      break;
    case 'survey':
      handleSurveyAction(conversationId);
      break;
    case 'wrap-up':
      handleWrapUp(conversationId);
      break;
    case 'follow-up':
      showFollowUpModal(conversationId);
      break;
    case 'close':
      showCloseModal(conversationId);
      break;
  }
}

async function showTransferModal(conversationId) {
  let responders = [];
  try {
    const user = api.getUser();
    const affParam = user?.affiliateId ? `&affiliateId=${user.affiliateId}` : '';
    const result = await api.get(`/api/users?role=responder${affParam}`);
    responders = result.data || result || [];
  } catch (err) {
    showToast('Failed to load responders', 'error');
    return;
  }

  const currentUser = api.getUser();
  const options = responders
    .filter(r => r.id !== currentUser?.id && (r.data?.status === 'active' || r.status === 'active'))
    .map(r => {
      const d = r.data || r;
      return `<option value="${escapeHtml(r.id)}">${escapeHtml(d.name)} (${escapeHtml(d.email)})</option>`;
    })
    .join('');

  showModal('Transfer Conversation', `
    <div class="form-group">
      <label for="transfer-target">Transfer to Responder</label>
      <select class="form-select" id="transfer-target">
        <option value="">Select a responder...</option>
        ${options}
      </select>
    </div>
    <div class="form-group">
      <label for="transfer-note">Note (optional)</label>
      <textarea class="form-textarea" id="transfer-note" placeholder="Add context for the receiving responder..."></textarea>
    </div>
  `, [
    { id: 'modal-cancel-btn', label: 'Cancel', class: 'btn-secondary' },
    { id: 'transfer-confirm-btn', label: 'Transfer', class: 'btn-primary' }
  ]);

  document.getElementById('transfer-confirm-btn').addEventListener('click', async () => {
    const targetId = document.getElementById('transfer-target').value;
    const note = document.getElementById('transfer-note').value;
    if (!targetId) {
      showToast('Please select a responder', 'warning');
      return;
    }
    try {
      await api.post(`/api/conversations/${conversationId}/transfer`, { targetResponderId: targetId, note });
      showToast('Conversation transferred', 'success');
      hideModal();
    } catch (err) {
      showToast('Transfer failed: ' + err.message, 'error');
    }
  });
}

function showEscalateModal(conversationId, type) {
  const typeLabel = type === 'police' ? 'Police / Emergency Services' : 'Medical Professional';
  const warningText = type === 'police'
    ? 'This will initiate contact with local emergency services. Ensure you have location information.'
    : 'This will alert on-call medical staff for clinical assessment.';

  showModal(`Escalate to ${typeLabel}`, `
    <p class="confirm-message">Are you sure you want to escalate this conversation?</p>
    <div class="confirm-warning">${warningText}</div>
  `, [
    { id: 'modal-cancel-btn', label: 'Cancel', class: 'btn-secondary' },
    { id: 'escalate-confirm-btn', label: `Escalate to ${typeLabel}`, class: type === 'police' ? 'btn-danger' : 'btn-warning' }
  ]);

  document.getElementById('escalate-confirm-btn').addEventListener('click', async () => {
    try {
      await api.post(`/api/conversations/${conversationId}/escalate`, { type });
      showToast(`Escalated to ${typeLabel}`, 'success');
      hideModal();
    } catch (err) {
      showToast('Escalation failed: ' + err.message, 'error');
    }
  });
}

function showTagModal(conversationId) {
  const predefinedTags = [
    'anxiety', 'depression', 'self-harm', 'suicidal-ideation',
    'substance-abuse', 'domestic-violence', 'grief', 'relationship',
    'bullying', 'eating-disorder', 'lgbtq', 'financial-stress',
    'academic-stress', 'loneliness', 'trauma', 'anger',
    'crisis-immediate', 'follow-up-needed'
  ];

  const checkboxes = predefinedTags.map(tag =>
    `<label class="checkbox-label">
      <input type="checkbox" value="${escapeHtml(tag)}" class="tag-checkbox">
      ${escapeHtml(tag.replace(/-/g, ' '))}
    </label>`
  ).join('');

  showModal('Tag Conversation', `
    <div class="form-group">
      <label>Select Tags</label>
      <div class="checkbox-group" style="max-height: 300px; overflow-y: auto;">
        ${checkboxes}
      </div>
    </div>
  `, [
    { id: 'modal-cancel-btn', label: 'Cancel', class: 'btn-secondary' },
    { id: 'tag-confirm-btn', label: 'Apply Tags', class: 'btn-primary' }
  ]);

  document.getElementById('tag-confirm-btn').addEventListener('click', async () => {
    const checked = Array.from(document.querySelectorAll('.tag-checkbox:checked'))
      .map(cb => cb.value);
    if (checked.length === 0) {
      showToast('Please select at least one tag', 'warning');
      return;
    }
    try {
      await api.post(`/api/conversations/${conversationId}/tag`, { tags: checked });
      showToast('Tags applied', 'success');
      hideModal();
    } catch (err) {
      showToast('Failed to apply tags: ' + err.message, 'error');
    }
  });
}

async function handleSurveyAction(conversationId) {
  try {
    await api.post(`/api/conversations/${conversationId}/survey`, { action: 'send_exit' });
    showToast('Exit survey sent', 'success');
  } catch (err) {
    showToast('Failed to send survey: ' + err.message, 'error');
  }
}

async function handleWrapUp(conversationId) {
  showModal('Begin Wrap Up', `
    <p class="confirm-message">This will begin the wrap-up process. An exit survey will be sent to the texter if configured.</p>
  `, [
    { id: 'modal-cancel-btn', label: 'Cancel', class: 'btn-secondary' },
    { id: 'wrapup-confirm-btn', label: 'Begin Wrap Up', class: 'btn-warning' }
  ]);

  document.getElementById('wrapup-confirm-btn').addEventListener('click', async () => {
    try {
      await api.post(`/api/conversations/${conversationId}/wrap-up`, {});
      showToast('Wrap-up initiated', 'success');
      hideModal();
    } catch (err) {
      showToast('Wrap-up failed: ' + err.message, 'error');
    }
  });
}

function showFollowUpModal(conversationId) {
  const now = new Date();
  now.setDate(now.getDate() + 1);
  const defaultDate = now.toISOString().slice(0, 16);

  showModal('Schedule Follow-up', `
    <div class="form-group">
      <label for="followup-date">Follow-up Date & Time</label>
      <input type="datetime-local" id="followup-date" value="${defaultDate}">
    </div>
    <div class="form-group">
      <label for="followup-message">Follow-up Message</label>
      <textarea class="form-textarea" id="followup-message" placeholder="Hi, we wanted to check in with you..."></textarea>
    </div>
  `, [
    { id: 'modal-cancel-btn', label: 'Cancel', class: 'btn-secondary' },
    { id: 'followup-confirm-btn', label: 'Schedule', class: 'btn-primary' }
  ]);

  document.getElementById('followup-confirm-btn').addEventListener('click', async () => {
    const scheduledTime = document.getElementById('followup-date').value;
    const message = document.getElementById('followup-message').value;
    if (!scheduledTime) {
      showToast('Please select a date and time', 'warning');
      return;
    }
    try {
      await api.post(`/api/conversations/${conversationId}/follow-up`, {
        scheduledTime: new Date(scheduledTime).toISOString(),
        message
      });
      showToast('Follow-up scheduled', 'success');
      hideModal();
    } catch (err) {
      showToast('Failed to schedule: ' + err.message, 'error');
    }
  });
}

function showCloseModal(conversationId) {
  showModal('Close Conversation', `
    <p class="confirm-message">Are you sure you want to close this conversation? This action is final.</p>
  `, [
    { id: 'modal-cancel-btn', label: 'Cancel', class: 'btn-secondary' },
    { id: 'close-confirm-btn', label: 'Close Conversation', class: 'btn-danger' }
  ]);

  document.getElementById('close-confirm-btn').addEventListener('click', async () => {
    try {
      await api.post(`/api/conversations/${conversationId}/close`, {});
      showToast('Conversation closed', 'success');
      hideModal();
      const user = api.getUser();
      window.location.hash = user?.role === 'supervisor' ? '#/supervisor' : '#/responder';
    } catch (err) {
      showToast('Failed to close: ' + err.message, 'error');
    }
  });
}
