import { useState } from 'react';

interface MapFieldEditorProps {
  value: Record<string, unknown>;
  onChange: (value: Record<string, unknown>) => void;
  valueType?: string; // "string" | "number"
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

export default function MapFieldEditor({ value, onChange, valueType, placeholder }: MapFieldEditorProps) {
  const [newKey, setNewKey] = useState('');
  const [newValue, setNewValue] = useState('');

  const entries = value && typeof value === 'object' && !Array.isArray(value)
    ? Object.entries(value)
    : [];

  const parseValue = (raw: string): unknown => {
    if (valueType === 'number') {
      const n = Number(raw);
      return isNaN(n) ? raw : n;
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

  const removeEntry = (key: string) => {
    const updated = { ...value };
    delete updated[key];
    onChange(updated);
  };

  const updateValue = (key: string, raw: string) => {
    onChange({ ...value, [key]: parseValue(raw) });
  };

  return (
    <div>
      {entries.map(([k, v]) => (
        <div key={k} style={{ display: 'flex', gap: 4, marginBottom: 4, alignItems: 'center' }}>
          <span
            style={{
              color: '#a6adc8',
              fontSize: 11,
              minWidth: 60,
              maxWidth: 80,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              flexShrink: 0,
            }}
            title={k}
          >
            {k}
          </span>
          <input
            type={valueType === 'number' ? 'number' : 'text'}
            value={String(v ?? '')}
            onChange={(e) => updateValue(k, e.target.value)}
            style={{ ...inputStyle, flex: 1 }}
          />
          <button
            onClick={() => removeEntry(k)}
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
    </div>
  );
}
