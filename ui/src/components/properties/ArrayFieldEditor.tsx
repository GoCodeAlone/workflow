import { useState } from 'react';

interface ArrayFieldEditorProps {
  value: unknown[];
  onChange: (value: unknown[]) => void;
  itemType?: string; // "string" | "number" | "boolean"
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

export default function ArrayFieldEditor({ value, onChange, itemType, placeholder }: ArrayFieldEditorProps) {
  const [newItem, setNewItem] = useState('');
  const [dragIdx, setDragIdx] = useState<number | null>(null);

  const items = Array.isArray(value) ? value : [];

  const parseItem = (raw: string): unknown => {
    if (itemType === 'number') {
      const n = Number(raw);
      return isNaN(n) ? raw : n;
    }
    if (itemType === 'boolean') {
      return raw === 'true';
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

  const moveUp = (index: number) => {
    if (index === 0) return;
    const updated = [...items];
    [updated[index - 1], updated[index]] = [updated[index], updated[index - 1]];
    onChange(updated);
  };

  const moveDown = (index: number) => {
    if (index >= items.length - 1) return;
    const updated = [...items];
    [updated[index], updated[index + 1]] = [updated[index + 1], updated[index]];
    onChange(updated);
  };

  const handleDragStart = (index: number) => {
    setDragIdx(index);
  };

  const handleDragOver = (e: React.DragEvent, index: number) => {
    e.preventDefault();
    if (dragIdx === null || dragIdx === index) return;
    const updated = [...items];
    const [moved] = updated.splice(dragIdx, 1);
    updated.splice(index, 0, moved);
    setDragIdx(index);
    onChange(updated);
  };

  const handleDragEnd = () => {
    setDragIdx(null);
  };

  return (
    <div>
      {items.map((item, i) => (
        <div
          key={i}
          draggable
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
            cursor: 'grab',
          }}
        >
          {/* Drag handle indicator */}
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
          <input
            type={itemType === 'number' ? 'number' : 'text'}
            value={String(item ?? '')}
            onChange={(e) => updateItem(i, e.target.value)}
            style={{ ...inputStyle, flex: 1 }}
          />
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
            disabled={i >= items.length - 1}
            style={{
              ...arrowBtnStyle,
              opacity: i >= items.length - 1 ? 0.3 : 1,
            }}
            title="Move down"
          >
            &#9660;
          </button>
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
            title="Remove item"
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
      {items.length === 0 && (
        <div style={{ color: '#585b70', fontSize: 10, marginTop: 4 }}>
          No items. Type above and press Enter or click + to add.
        </div>
      )}
    </div>
  );
}
