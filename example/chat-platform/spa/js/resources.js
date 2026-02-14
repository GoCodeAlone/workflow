import { api } from './api.js';
import { escapeHtml, showToast } from './components.js';

let cachedResponses = null;

export async function loadCannedResponses() {
  if (cachedResponses) return cachedResponses;
  try {
    const result = await api.get('/api/resources/canned-responses');
    cachedResponses = result.data || result || [];
    return cachedResponses;
  } catch (err) {
    return [];
  }
}

export function clearResourcesCache() {
  cachedResponses = null;
}

// Group responses by category
function groupByCategory(responses) {
  const groups = {};
  responses.forEach(r => {
    const d = r.data || r;
    const cat = d.category || 'general';
    if (!groups[cat]) groups[cat] = [];
    groups[cat].push(r);
  });
  return groups;
}

function formatCategoryName(cat) {
  return cat.replace(/-/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

// Renders the shared resources panel as a sidebar section.
// Returns HTML string. Call wireResourcesPanel() after inserting into DOM.
export function renderResourcesPanel() {
  return `
    <div class="chat-sidebar-section resources-panel">
      <h4>
        <span class="resources-toggle" id="resources-toggle">
          &#9654; Shared Resources
        </span>
      </h4>
      <div class="resources-content hidden" id="resources-content">
        <input class="form-input form-input-sm" id="resources-filter" placeholder="Search responses..." style="margin-bottom:0.5rem"/>
        <div id="resources-list"></div>
      </div>
    </div>
  `;
}

export async function wireResourcesPanel(onInsertCallback) {
  const toggle = document.getElementById('resources-toggle');
  const content = document.getElementById('resources-content');
  if (!toggle || !content) return;

  toggle.addEventListener('click', async () => {
    if (!content.classList.contains('hidden')) {
      content.classList.add('hidden');
      toggle.innerHTML = '&#9654; Shared Resources';
      return;
    }
    content.classList.remove('hidden');
    toggle.innerHTML = '&#9660; Shared Resources';

    const responses = await loadCannedResponses();
    renderResponseList(responses, onInsertCallback);
  });

  const filterInput = document.getElementById('resources-filter');
  if (filterInput) {
    filterInput.addEventListener('input', async () => {
      const term = filterInput.value.toLowerCase();
      const responses = await loadCannedResponses();
      const filtered = responses.filter(r => {
        const d = r.data || r;
        return (d.title || '').toLowerCase().includes(term) ||
               (d.content || '').toLowerCase().includes(term) ||
               (d.tags || []).some(t => t.toLowerCase().includes(term)) ||
               (d.category || '').toLowerCase().includes(term);
      });
      renderResponseList(filtered, onInsertCallback);
    });
  }
}

function renderResponseList(responses, onInsertCallback) {
  const container = document.getElementById('resources-list');
  if (!container) return;

  if (responses.length === 0) {
    container.innerHTML = '<div class="text-muted" style="font-size:0.8rem;padding:0.5rem 0">No matching responses</div>';
    return;
  }

  const groups = groupByCategory(responses);
  container.innerHTML = Object.entries(groups).map(([cat, items]) => `
    <div class="resource-category">
      <div class="resource-category-header">${escapeHtml(formatCategoryName(cat))}</div>
      ${items.map(r => {
        const d = r.data || r;
        return `
          <div class="resource-item" data-response-id="${escapeHtml(r.id)}">
            <div class="resource-item-title">${escapeHtml(d.title)}</div>
            <div class="resource-item-preview">${escapeHtml((d.content || '').substring(0, 80))}${(d.content || '').length > 80 ? '...' : ''}</div>
            <div class="resource-item-actions">
              <button class="btn btn-sm btn-ghost resource-copy-btn" data-content="${escapeHtml(d.content)}" title="Copy to clipboard">Copy</button>
              <button class="btn btn-sm btn-primary resource-insert-btn" data-content="${escapeHtml(d.content)}" title="Insert into message">Insert</button>
            </div>
          </div>
        `;
      }).join('')}
    </div>
  `).join('');

  // Wire copy buttons
  container.querySelectorAll('.resource-copy-btn').forEach(btn => {
    btn.addEventListener('click', (e) => {
      e.stopPropagation();
      const text = btn.dataset.content;
      navigator.clipboard.writeText(text).then(() => {
        showToast('Copied to clipboard', 'success');
      }).catch(() => {
        showToast('Failed to copy', 'error');
      });
    });
  });

  // Wire insert buttons
  container.querySelectorAll('.resource-insert-btn').forEach(btn => {
    btn.addEventListener('click', (e) => {
      e.stopPropagation();
      const text = btn.dataset.content;
      if (onInsertCallback) {
        onInsertCallback(text);
      }
    });
  });
}
