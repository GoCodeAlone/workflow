import { useState } from 'react';

interface ArrayFieldEditorProps {
  value: unknown[];
  onChange: (value: unknown[]) => void;
  itemType?: string; // "string" | "number"
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

export default function ArrayFieldEditor({ value, onChange, itemType, placeholder }: ArrayFieldEditorProps) {
  const [newItem, setNewItem] = useState('');

  const items = Array.isArray(value) ? value : [];

  const parseItem = (raw: string): unknown => {
    if (itemType === 'number') {
      const n = Number(raw);
      return isNaN(n) ? raw : n;
    }
    return raw;
  };

  const addItem = () => {
    const trimmed = newItem.trim();
    if (!trimmed) return;
    onChange([...items, parseItem(trimmed)]);
    setNewItem('');
  };

  const removeItem = (index: number) => {
    onChange(items.filter((_, i) => i !== index));
  };

  const updateItem = (index: number, raw: string) => {
    const updated = [...items];
    updated[index] = parseItem(raw);
    onChange(updated);
  };

  return (
    <div>
      {items.map((item, i) => (
        <div key={i} style={{ display: 'flex', gap: 4, marginBottom: 4, alignItems: 'center' }}>
          <input
            type={itemType === 'number' ? 'number' : 'text'}
            value={String(item ?? '')}
            onChange={(e) => updateItem(i, e.target.value)}
            style={{ ...inputStyle, flex: 1 }}
          />
          <button
            onClick={() => removeItem(i)}
            style={{
              background: 'none',
              border: 'none',
              color: '#f38ba8',
              cursor: 'pointer',
              fontSize: 11,
              padding: '0 4px',
              flexShrink: 0,
            }}
          >
            x
          </button>
        </div>
      ))}
      <div style={{ display: 'flex', gap: 4 }}>
        <input
          type={itemType === 'number' ? 'number' : 'text'}
          value={newItem}
          onChange={(e) => setNewItem(e.target.value)}
          placeholder={placeholder || 'Add item...'}
          style={{ ...inputStyle, flex: 1 }}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              addItem();
            }
          }}
        />
        <button
          onClick={addItem}
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
    </div>
  );
}
