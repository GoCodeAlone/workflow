import { useState } from 'react';

interface MapFieldEditorProps {
  value: Record<string, unknown>;
  onChange: (value: Record<string, unknown>) => void;
  valueType?: string; // "string" | "number" | "boolean"
  placeholder?: string;
  label: string;
}

const inputStyle: React.CSSProperties = {
  width: '100%',
  padding: '6px 8px',
  background: '#1e1e2e',
  border: '1px solid #313244',
  borderRadius: 4,
  color: '#cdd6f4',
  fontSize: 12,
  outline: 'none',
  boxSizing: 'border-box',
};

const arrowBtnStyle: React.CSSProperties = {
  background: 'none',
  border: 'none',
  color: '#6c7086',
  cursor: 'pointer',
  fontSize: 8,
  padding: '0 2px',
  lineHeight: 1,
};

/**
 * Rebuild a Record from an ordered array of [key, value] entries.
 * This preserves insertion order in the JS object.
 */
function fromEntries(entries: [string, unknown][]): Record<string, unknown> {
  const result: Record<string, unknown> = {};
  for (const [k, v] of entries) {
    result[k] = v;
  }
  return result;
}

export default function MapFieldEditor({ value, onChange, valueType, placeholder }: MapFieldEditorProps) {
  const [newKey, setNewKey] = useState('');
  const [newValue, setNewValue] = useState('');
  const [dragIdx, setDragIdx] = useState<number | null>(null);
  const [editingKey, setEditingKey] = useState<number | null>(null);

  const entries: [string, unknown][] = value && typeof value === 'object' && !Array.isArray(value)
    ? Object.entries(value)
    : [];

  const parseValue = (raw: string): unknown => {
    if (valueType === 'number') {
      const n = Number(raw);
      return isNaN(n) ? raw : n;
    }
    if (valueType === 'boolean') {
      return raw === 'true';
    }
    return raw;
  };

  const addEntry = () => {
    const trimmedKey = newKey.trim();
    if (!trimmedKey) return;
    onChange({ ...value, [trimmedKey]: parseValue(newValue.trim()) });
    setNewKey('');
    setNewValue('');
  };

  const removeEntry = (index: number) => {
    const updated = entries.filter((_, i) => i !== index);
    onChange(fromEntries(updated));
  };

  const updateEntryValue = (index: number, raw: string) => {
    const updated = [...entries];
    updated[index] = [updated[index][0], parseValue(raw)];
    onChange(fromEntries(updated));
  };

  const updateEntryKey = (index: number, newKeyName: string) => {
    const trimmed = newKeyName.trim();
    if (!trimmed) return;
    // Check for duplicate keys (allow same index)
    const hasDuplicate = entries.some(([k], i) => i !== index && k === trimmed);
    if (hasDuplicate) return;
    const updated = [...entries];
    updated[index] = [trimmed, updated[index][1]];
    onChange(fromEntries(updated));
    setEditingKey(null);
  };

  const moveUp = (index: number) => {
    if (index === 0) return;
    const updated = [...entries];
    [updated[index - 1], updated[index]] = [updated[index], updated[index - 1]];
    onChange(fromEntries(updated));
  };

  const moveDown = (index: number) => {
    if (index >= entries.length - 1) return;
    const updated = [...entries];
    [updated[index], updated[index + 1]] = [updated[index + 1], updated[index]];
    onChange(fromEntries(updated));
  };

  const handleDragStart = (index: number) => {
    setDragIdx(index);
  };

  const handleDragOver = (e: React.DragEvent, index: number) => {
    e.preventDefault();
    if (dragIdx === null || dragIdx === index) return;
    const updated = [...entries];
    const [moved] = updated.splice(dragIdx, 1);
    updated.splice(index, 0, moved);
    setDragIdx(index);
    onChange(fromEntries(updated));
  };

  const handleDragEnd = () => {
    setDragIdx(null);
  };

  return (
    <div>
      {entries.map(([k, v], i) => (
        <div
          key={`${k}-${i}`}
          draggable={editingKey !== i}
          onDragStart={() => handleDragStart(i)}
          onDragOver={(e) => handleDragOver(e, i)}
          onDragEnd={handleDragEnd}
          style={{
            display: 'flex',
            gap: 4,
            marginBottom: 3,
            alignItems: 'center',
            padding: '2px 0',
            background: dragIdx === i ? '#313244' : 'transparent',
            borderRadius: 4,
            cursor: editingKey === i ? 'default' : 'grab',
          }}
        >
          {/* Drag handle */}
          <span
            style={{
              color: '#45475a',
              fontSize: 10,
              cursor: 'grab',
              flexShrink: 0,
              userSelect: 'none',
              padding: '0 2px',
            }}
            title="Drag to reorder"
          >
            &#8942;&#8942;
          </span>
          {/* Key display/edit */}
          {editingKey === i ? (
            <input
              type="text"
              defaultValue={k}
              autoFocus
              onBlur={(e) => updateEntryKey(i, e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  updateEntryKey(i, (e.target as HTMLInputElement).value);
                } else if (e.key === 'Escape') {
                  setEditingKey(null);
                }
              }}
              style={{
                ...inputStyle,
                width: 'auto',
                flex: '0 0 70px',
                fontSize: 11,
                padding: '4px 6px',
              }}
            />
          ) : (
            <span
              onClick={() => setEditingKey(i)}
              style={{
                color: '#a6adc8',
                fontSize: 11,
                minWidth: 50,
                maxWidth: 80,
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
                flexShrink: 0,
                cursor: 'text',
                padding: '2px 4px',
                borderRadius: 3,
                border: '1px solid transparent',
              }}
              title={`${k} (click to rename)`}
            >
              {k}
            </span>
          )}
          {/* Value input */}
          <input
            type={valueType === 'number' ? 'number' : 'text'}
            value={String(v ?? '')}
            onChange={(e) => updateEntryValue(i, e.target.value)}
            style={{ ...inputStyle, flex: 1 }}
          />
          {/* Reorder buttons */}
          <button
            onClick={() => moveUp(i)}
            disabled={i === 0}
            style={{
              ...arrowBtnStyle,
              opacity: i === 0 ? 0.3 : 1,
            }}
            title="Move up"
          >
            &#9650;
          </button>
          <button
            onClick={() => moveDown(i)}
            disabled={i >= entries.length - 1}
            style={{
              ...arrowBtnStyle,
              opacity: i >= entries.length - 1 ? 0.3 : 1,
            }}
            title="Move down"
          >
            &#9660;
          </button>
          {/* Remove button */}
          <button
            onClick={() => removeEntry(i)}
            style={{
              background: 'none',
              border: 'none',
              color: '#f38ba8',
              cursor: 'pointer',
              fontSize: 11,
              padding: '0 4px',
              flexShrink: 0,
            }}
            title="Remove entry"
          >
            x
          </button>
        </div>
      ))}
      {/* Add new entry row */}
      <div style={{ display: 'flex', gap: 4 }}>
        <input
          type="text"
          value={newKey}
          onChange={(e) => setNewKey(e.target.value)}
          placeholder="key"
          style={{ ...inputStyle, flex: 1 }}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              addEntry();
            }
          }}
        />
        <input
          type={valueType === 'number' ? 'number' : 'text'}
          value={newValue}
          onChange={(e) => setNewValue(e.target.value)}
          placeholder={placeholder || 'value'}
          style={{ ...inputStyle, flex: 1 }}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              addEntry();
            }
          }}
        />
        <button
          onClick={addEntry}
          style={{
            background: '#313244',
            border: '1px solid #45475a',
            borderRadius: 4,
            color: '#cdd6f4',
            cursor: 'pointer',
            fontSize: 11,
            padding: '4px 8px',
            flexShrink: 0,
          }}
        >
          +
        </button>
      </div>
      {entries.length === 0 && (
        <div style={{ color: '#585b70', fontSize: 10, marginTop: 4 }}>
          No entries. Add a key-value pair above.
        </div>
      )}
    </div>
  );
}
