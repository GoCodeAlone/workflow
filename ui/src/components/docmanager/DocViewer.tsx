import { useState } from 'react';
import useDocManagerStore from '../../store/docManagerStore.ts';

function renderMarkdown(text: string): string {
  let html = (text ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');

  // Code blocks
  html = html.replace(/```(\w*)\n([\s\S]*?)```/g, (_match, _lang, code) => {
    return `<pre style="background:#313244;padding:12px;border-radius:6px;overflow-x:auto;font-size:13px;color:#cdd6f4"><code>${code.trim()}</code></pre>`;
  });

  const lines = html.split('\n');
  const result: string[] = [];
  let inList = false;

  for (let i = 0; i < lines.length; i++) {
    let line = lines[i];

    if (line.includes('<pre ') || line.includes('</pre>')) {
      if (inList) { result.push('</ul>'); inList = false; }
      result.push(line);
      continue;
    }

    if (line.startsWith('### ')) {
      if (inList) { result.push('</ul>'); inList = false; }
      result.push(`<h3 style="color:#cdd6f4;margin:16px 0 8px;font-size:16px">${applyInline(line.slice(4))}</h3>`);
      continue;
    }
    if (line.startsWith('## ')) {
      if (inList) { result.push('</ul>'); inList = false; }
      result.push(`<h2 style="color:#cdd6f4;margin:16px 0 8px;font-size:18px">${applyInline(line.slice(3))}</h2>`);
      continue;
    }
    if (line.startsWith('# ')) {
      if (inList) { result.push('</ul>'); inList = false; }
      result.push(`<h1 style="color:#cdd6f4;margin:16px 0 8px;font-size:22px">${applyInline(line.slice(2))}</h1>`);
      continue;
    }

    if (line.match(/^[-*] /)) {
      if (!inList) {
        result.push('<ul style="margin:8px 0;padding-left:20px;color:#cdd6f4">');
        inList = true;
      }
      result.push(`<li style="margin:4px 0;font-size:14px">${applyInline(line.slice(2))}</li>`);
      continue;
    }

    if (inList) { result.push('</ul>'); inList = false; }

    if (line.trim() === '') {
      result.push('<br/>');
      continue;
    }

    result.push(`<p style="margin:6px 0;color:#cdd6f4;font-size:14px;line-height:1.6">${applyInline(line)}</p>`);
  }

  if (inList) result.push('</ul>');
  return result.join('\n');
}

function applyInline(text: string): string {
  let out = text.replace(/`([^`]+)`/g, '<code style="background:#313244;padding:2px 6px;border-radius:3px;font-size:12px;color:#f9e2af">$1</code>');
  out = out.replace(/\*\*([^*]+)\*\*/g, '<strong style="color:#cdd6f4">$1</strong>');
  out = out.replace(/\*([^*]+)\*/g, '<em>$1</em>');
  out = out.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener noreferrer" style="color:#89b4fa;text-decoration:underline">$1</a>');
  return out;
}

function formatDateTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString(undefined, {
      month: 'short',
      day: 'numeric',
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  } catch {
    return iso;
  }
}

export default function DocViewer({ onEdit }: { onEdit: () => void }) {
  const selectedDoc = useDocManagerStore((s) => s.selectedDoc);
  const deleteDoc = useDocManagerStore((s) => s.deleteDoc);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);

  if (!selectedDoc) return null;

  const handleDelete = async () => {
    setDeleting(true);
    try {
      await deleteDoc(selectedDoc.id);
    } catch {
      // handled by store
    } finally {
      setDeleting(false);
      setConfirmDelete(false);
    }
  };

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      {/* Header */}
      <div
        style={{
          padding: '16px 24px',
          borderBottom: '1px solid #313244',
          background: '#181825',
          flexShrink: 0,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12 }}>
          <div style={{ flex: 1 }}>
            <h1 style={{ color: '#cdd6f4', fontSize: 20, fontWeight: 600, margin: '0 0 8px' }}>
              {selectedDoc.title}
            </h1>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
              {selectedDoc.category && (
                <span
                  style={{
                    background: '#45475a',
                    color: '#89b4fa',
                    fontSize: 11,
                    padding: '2px 8px',
                    borderRadius: 4,
                    fontWeight: 500,
                  }}
                >
                  {selectedDoc.category}
                </span>
              )}
              <span style={{ color: '#6c7086', fontSize: 11 }}>
                Created: {formatDateTime(selectedDoc.created_at)}
              </span>
              <span style={{ color: '#6c7086', fontSize: 11 }}>
                Updated: {formatDateTime(selectedDoc.updated_at)}
              </span>
            </div>
          </div>
          <div style={{ display: 'flex', gap: 8, flexShrink: 0 }}>
            <button
              onClick={onEdit}
              style={{
                background: '#89b4fa',
                border: 'none',
                borderRadius: 6,
                color: '#1e1e2e',
                padding: '7px 16px',
                fontSize: 13,
                fontWeight: 600,
                cursor: 'pointer',
              }}
            >
              Edit
            </button>
            <button
              onClick={() => setConfirmDelete(true)}
              style={{
                background: 'rgba(243, 139, 168, 0.15)',
                border: '1px solid #f38ba8',
                borderRadius: 6,
                color: '#f38ba8',
                padding: '7px 16px',
                fontSize: 13,
                cursor: 'pointer',
              }}
            >
              Delete
            </button>
          </div>
        </div>

        {confirmDelete && (
          <div
            style={{
              marginTop: 12,
              padding: '8px 12px',
              background: 'rgba(243, 139, 168, 0.1)',
              border: '1px solid #f38ba8',
              borderRadius: 6,
              display: 'flex',
              alignItems: 'center',
              gap: 10,
              fontSize: 12,
              color: '#f38ba8',
            }}
          >
            <span>Delete this document?</span>
            <button
              onClick={handleDelete}
              disabled={deleting}
              style={{
                background: '#f38ba8',
                border: 'none',
                borderRadius: 4,
                color: '#1e1e2e',
                fontSize: 11,
                fontWeight: 600,
                padding: '4px 12px',
                cursor: deleting ? 'not-allowed' : 'pointer',
              }}
            >
              {deleting ? 'Deleting...' : 'Confirm'}
            </button>
            <button
              onClick={() => setConfirmDelete(false)}
              style={{
                background: '#313244',
                border: '1px solid #45475a',
                borderRadius: 4,
                color: '#cdd6f4',
                fontSize: 11,
                padding: '4px 12px',
                cursor: 'pointer',
              }}
            >
              Cancel
            </button>
          </div>
        )}
      </div>

      {/* Content */}
      <div
        style={{
          flex: 1,
          overflowY: 'auto',
          padding: 24,
          background: '#1e1e2e',
        }}
        dangerouslySetInnerHTML={{ __html: renderMarkdown(selectedDoc.content) }}
      />
    </div>
  );
}
