import { useRef, useCallback } from 'react';

interface SqlEditorProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
}

// SQL keywords to highlight
const SQL_KEYWORDS = new Set([
  'SELECT', 'INSERT', 'UPDATE', 'DELETE', 'FROM', 'WHERE', 'SET', 'VALUES',
  'INTO', 'JOIN', 'ON', 'AND', 'OR', 'NOT', 'ORDER', 'BY', 'GROUP', 'LIMIT',
  'AS', 'CREATE', 'ALTER', 'DROP', 'TABLE', 'INDEX', 'IS', 'NULL', 'LIKE',
  'IN', 'EXISTS', 'BETWEEN', 'CASE', 'WHEN', 'THEN', 'ELSE', 'END', 'HAVING',
  'DISTINCT', 'COUNT', 'DEFAULT', 'PRIMARY', 'KEY', 'FOREIGN', 'REFERENCES',
  'CASCADE', 'LEFT', 'RIGHT', 'INNER', 'OUTER', 'FULL', 'CROSS', 'UNION',
  'ALL', 'ANY', 'SOME', 'ASC', 'DESC', 'OFFSET', 'FETCH', 'NEXT', 'ROWS',
  'ONLY', 'WITH', 'RECURSIVE', 'RETURNING', 'CONFLICT', 'DO', 'NOTHING',
  'SUM', 'AVG', 'MIN', 'MAX', 'COALESCE', 'IFNULL', 'NULLIF',
]);

function highlightSQL(text: string): string {
  const result: string[] = [];
  let i = 0;

  while (i < text.length) {
    // Comment: -- to end of line
    if (text[i] === '-' && text[i + 1] === '-') {
      const end = text.indexOf('\n', i);
      const comment = end === -1 ? text.slice(i) : text.slice(i, end);
      result.push(`<span style="color:#585b70;font-style:italic">${escapeHtml(comment)}</span>`);
      i += comment.length;
      continue;
    }

    // String literal: '...'
    if (text[i] === "'") {
      let j = i + 1;
      while (j < text.length) {
        if (text[j] === "'" && text[j + 1] === "'") {
          j += 2; // escaped quote
        } else if (text[j] === "'") {
          j++;
          break;
        } else {
          j++;
        }
      }
      result.push(`<span style="color:#a6e3a1">${escapeHtml(text.slice(i, j))}</span>`);
      i = j;
      continue;
    }

    // Template expression: {{ ... }}
    if (text[i] === '{' && text[i + 1] === '{') {
      const end = text.indexOf('}}', i + 2);
      if (end !== -1) {
        const expr = text.slice(i, end + 2);
        result.push(`<span style="color:#cba6f7;background:#cba6f710">${escapeHtml(expr)}</span>`);
        i = end + 2;
        continue;
      }
    }

    // Placeholder: ?
    if (text[i] === '?') {
      result.push(`<span style="color:#f9e2af;font-weight:bold">?</span>`);
      i++;
      continue;
    }

    // Number
    if (/\d/.test(text[i]) && (i === 0 || /[\s,=(+\-*/]/.test(text[i - 1]))) {
      let j = i;
      while (j < text.length && /[\d.]/.test(text[j])) j++;
      // Make sure it's not part of an identifier
      if (j === text.length || !/[a-zA-Z_]/.test(text[j])) {
        result.push(`<span style="color:#fab387">${escapeHtml(text.slice(i, j))}</span>`);
        i = j;
        continue;
      }
    }

    // Word (potential keyword)
    if (/[a-zA-Z_]/.test(text[i])) {
      let j = i;
      while (j < text.length && /[a-zA-Z0-9_]/.test(text[j])) j++;
      const word = text.slice(i, j);
      if (SQL_KEYWORDS.has(word.toUpperCase())) {
        result.push(`<span style="color:#89b4fa;font-weight:bold">${escapeHtml(word)}</span>`);
      } else {
        result.push(escapeHtml(word));
      }
      i = j;
      continue;
    }

    // Newline - preserve as-is
    if (text[i] === '\n') {
      result.push('\n');
      i++;
      continue;
    }

    // Any other character
    result.push(escapeHtml(text[i]));
    i++;
  }

  return result.join('');
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}

const sharedStyle: React.CSSProperties = {
  fontFamily: 'ui-monospace, "Cascadia Code", "Source Code Pro", Menlo, monospace',
  fontSize: 12,
  lineHeight: '1.5',
  padding: '8px',
  border: '1px solid #313244',
  borderRadius: 4,
  whiteSpace: 'pre-wrap',
  wordWrap: 'break-word',
  overflowWrap: 'break-word',
  tabSize: 2,
  boxSizing: 'border-box' as const,
  width: '100%',
};

export default function SqlEditor({ value, onChange, placeholder }: SqlEditorProps) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const handleChange = useCallback(
    (e: React.ChangeEvent<HTMLTextAreaElement>) => {
      onChange(e.target.value);
    },
    [onChange],
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      // Tab inserts two spaces
      if (e.key === 'Tab') {
        e.preventDefault();
        const ta = e.currentTarget;
        const start = ta.selectionStart;
        const end = ta.selectionEnd;
        const newVal = value.slice(0, start) + '  ' + value.slice(end);
        onChange(newVal);
        requestAnimationFrame(() => {
          ta.selectionStart = ta.selectionEnd = start + 2;
        });
      }
    },
    [value, onChange],
  );

  const highlighted = highlightSQL(value);

  return (
    <div style={{ position: 'relative', width: '100%' }}>
      {/* Highlighted background layer */}
      <div
        aria-hidden
        style={{
          ...sharedStyle,
          position: 'absolute',
          top: 0,
          left: 0,
          background: '#1e1e2e',
          color: '#cdd6f4',
          pointerEvents: 'none',
          minHeight: '7.5em',
        }}
        dangerouslySetInnerHTML={{ __html: highlighted || `<span style="color:#585b70">${escapeHtml(placeholder ?? '')}</span>` }}
      />

      {/* Transparent textarea on top */}
      <textarea
        ref={textareaRef}
        value={value}
        onChange={handleChange}
        onKeyDown={handleKeyDown}
        placeholder={placeholder}
        spellCheck={false}
        rows={6}
        style={{
          ...sharedStyle,
          position: 'relative',
          background: 'transparent',
          color: 'transparent',
          caretColor: '#cdd6f4',
          resize: 'vertical',
          outline: 'none',
          minHeight: '7.5em',
          // Hide placeholder when we render our own
          ...(value === '' ? {} : {}),
        }}
      />
    </div>
  );
}
