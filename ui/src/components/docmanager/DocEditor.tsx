import { useState, useEffect } from 'react';
import useDocManagerStore from '../../store/docManagerStore.ts';

function renderMarkdown(text: string): string {
  // Escape HTML
  let html = text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');

  // Code blocks (``` ... ```)
  html = html.replace(/```(\w*)\n([\s\S]*?)```/g, (_match, _lang, code) => {
    return `<pre style="background:#313244;padding:12px;border-radius:6px;overflow-x:auto;font-size:13px;color:#cdd6f4"><code>${code.trim()}</code></pre>`;
  });

  // Split into lines for block-level processing
  const lines = html.split('\n');
  const result: string[] = [];
  let inList = false;

  for (let i = 0; i < lines.length; i++) {
    let line = lines[i];

    // Skip lines inside pre blocks (already handled)
    if (line.includes('<pre ') || line.includes('</pre>')) {
      if (inList) {
        result.push('</ul>');
        inList = false;
      }
      result.push(line);
      continue;
    }

    // Headers
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

    // List items
    if (line.match(/^[-*] /)) {
      if (!inList) {
        result.push('<ul style="margin:8px 0;padding-left:20px;color:#cdd6f4">');
        inList = true;
      }
      result.push(`<li style="margin:4px 0;font-size:14px">${applyInline(line.slice(2))}</li>`);
      continue;
    }

    // Close list if we're no longer in list items
    if (inList) {
      result.push('</ul>');
      inList = false;
    }

    // Empty line = paragraph break
    if (line.trim() === '') {
      result.push('<br/>');
      continue;
    }

    // Regular paragraph
    result.push(`<p style="margin:6px 0;color:#cdd6f4;font-size:14px;line-height:1.6">${applyInline(line)}</p>`);
  }

  if (inList) result.push('</ul>');

  return result.join('\n');
}

function applyInline(text: string): string {
  // Inline code
  let out = text.replace(/`([^`]+)`/g, '<code style="background:#313244;padding:2px 6px;border-radius:3px;font-size:12px;color:#f9e2af">$1</code>');
  // Bold
  out = out.replace(/\*\*([^*]+)\*\*/g, '<strong style="color:#cdd6f4">$1</strong>');
  // Italic
  out = out.replace(/\*([^*]+)\*/g, '<em>$1</em>');
  // Links
  out = out.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener noreferrer" style="color:#89b4fa;text-decoration:underline">$1</a>');
  return out;
}

const inputStyle: React.CSSProperties = {
  background: '#313244',
  border: '1px solid #45475a',
  borderRadius: 6,
  color: '#cdd6f4',
  padding: '8px 10px',
  fontSize: 13,
  outline: 'none',
  boxSizing: 'border-box',
};

export default function DocEditor({
  isNew,
  onCancel,
}: {
  isNew: boolean;
  onCancel: () => void;
}) {
  const selectedDoc = useDocManagerStore((s) => s.selectedDoc);
  const editorContent = useDocManagerStore((s) => s.editorContent);
  const editorDirty = useDocManagerStore((s) => s.editorDirty);
  const setEditorContent = useDocManagerStore((s) => s.setEditorContent);
  const createDoc = useDocManagerStore((s) => s.createDoc);
  const updateDoc = useDocManagerStore((s) => s.updateDoc);
  const categories = useDocManagerStore((s) => s.categories);

  const [title, setTitle] = useState(isNew ? '' : selectedDoc?.title ?? '');
  const [category, setCategory] = useState(isNew ? '' : selectedDoc?.category ?? '');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!isNew && selectedDoc) {
      setTitle(selectedDoc.title);
      setCategory(selectedDoc.category);
    }
  }, [isNew, selectedDoc]);

  const titleDirty = isNew ? title !== '' : title !== (selectedDoc?.title ?? '');
  const categoryDirty = isNew ? category !== '' : category !== (selectedDoc?.category ?? '');
  const isDirty = editorDirty || titleDirty || categoryDirty;

  const handleSave = async () => {
    if (!title.trim()) {
      setError('Title is required');
      return;
    }
    setError(null);
    setSaving(true);
    try {
      if (isNew) {
        await createDoc({
          title: title.trim(),
          content: editorContent,
          category: category || undefined,
        });
        onCancel(); // Close editor after creating
      } else if (selectedDoc) {
        await updateDoc(selectedDoc.id, {
          title: title.trim(),
          content: editorContent,
          category: category || undefined,
        });
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      {/* Top bar */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          padding: '10px 16px',
          borderBottom: '1px solid #313244',
          background: '#181825',
          flexShrink: 0,
        }}
      >
        <input
          type="text"
          placeholder="Document title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          style={{ ...inputStyle, flex: 1 }}
        />
        <select
          value={category}
          onChange={(e) => setCategory(e.target.value)}
          style={{ ...inputStyle, width: 150 }}
        >
          <option value="">No category</option>
          {categories.map((cat) => (
            <option key={cat} value={cat}>
              {cat}
            </option>
          ))}
        </select>

        {isDirty && (
          <span style={{ color: '#f9e2af', fontSize: 11, fontWeight: 500 }}>unsaved</span>
        )}

        <button
          onClick={handleSave}
          disabled={saving}
          style={{
            background: saving ? '#585b70' : '#89b4fa',
            border: 'none',
            borderRadius: 6,
            color: '#1e1e2e',
            padding: '7px 16px',
            fontSize: 13,
            fontWeight: 600,
            cursor: saving ? 'not-allowed' : 'pointer',
          }}
        >
          {saving ? 'Saving...' : 'Save'}
        </button>
        <button
          onClick={onCancel}
          style={{
            background: '#313244',
            border: '1px solid #45475a',
            borderRadius: 6,
            color: '#cdd6f4',
            padding: '7px 16px',
            fontSize: 13,
            cursor: 'pointer',
          }}
        >
          Cancel
        </button>
      </div>

      {error && (
        <div
          style={{
            background: 'rgba(243, 139, 168, 0.15)',
            border: '1px solid #f38ba8',
            borderRadius: 0,
            padding: '6px 16px',
            color: '#f38ba8',
            fontSize: 12,
          }}
        >
          {error}
        </div>
      )}

      {/* Split pane: editor + preview */}
      <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* Editor pane */}
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', borderRight: '1px solid #313244' }}>
          <div style={{ padding: '6px 12px', color: '#6c7086', fontSize: 11, fontWeight: 500, borderBottom: '1px solid #313244' }}>
            Markdown
          </div>
          <textarea
            value={editorContent}
            onChange={(e) => setEditorContent(e.target.value)}
            placeholder="Write your markdown content here..."
            style={{
              flex: 1,
              background: '#1e1e2e',
              color: '#cdd6f4',
              border: 'none',
              outline: 'none',
              padding: 16,
              fontSize: 14,
              fontFamily: "'JetBrains Mono', 'Fira Code', 'Cascadia Code', monospace",
              lineHeight: 1.6,
              resize: 'none',
              boxSizing: 'border-box',
            }}
          />
        </div>

        {/* Preview pane */}
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
          <div style={{ padding: '6px 12px', color: '#6c7086', fontSize: 11, fontWeight: 500, borderBottom: '1px solid #313244' }}>
            Preview
          </div>
          <div
            style={{
              flex: 1,
              overflowY: 'auto',
              padding: 16,
              background: '#1e1e2e',
            }}
            dangerouslySetInnerHTML={{ __html: renderMarkdown(editorContent) }}
          />
        </div>
      </div>
    </div>
  );
}
