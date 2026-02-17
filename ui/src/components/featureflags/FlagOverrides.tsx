import { useState } from 'react';
import type { FlagOverride, FlagType } from '../../store/featureFlagStore.ts';

interface FlagOverridesProps {
  overrides: FlagOverride[];
  flagType: FlagType;
  onChange: (overrides: FlagOverride[]) => void;
}

const inputStyle: React.CSSProperties = {
  width: '100%',
  padding: '6px 8px',
  borderRadius: 4,
  border: '1px solid #45475a',
  background: '#313244',
  color: '#cdd6f4',
  fontSize: 12,
  outline: 'none',
  boxSizing: 'border-box',
};

const smallBtnStyle: React.CSSProperties = {
  padding: '4px 10px',
  borderRadius: 4,
  border: '1px solid #45475a',
  fontSize: 11,
  fontWeight: 600,
  cursor: 'pointer',
  background: 'transparent',
  color: '#89b4fa',
};

function parseValue(raw: string, flagType: FlagType): unknown {
  if (flagType === 'boolean') return raw === 'true';
  if (flagType === 'number') return Number(raw) || 0;
  if (flagType === 'json') {
    try { return JSON.parse(raw); } catch { return raw; }
  }
  return raw;
}

function formatValue(value: unknown): string {
  if (value === null || value === undefined) return '';
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
}

export default function FlagOverrides({ overrides, flagType, onChange }: FlagOverridesProps) {
  const [newType, setNewType] = useState<'user' | 'group'>('user');
  const [newKey, setNewKey] = useState('');
  const [newValue, setNewValue] = useState('');

  const handleAdd = () => {
    if (!newKey.trim()) return;
    const override: FlagOverride = {
      type: newType,
      key: newKey.trim(),
      value: parseValue(newValue, flagType),
    };
    onChange([...overrides, override]);
    setNewKey('');
    setNewValue('');
  };

  const handleRemove = (index: number) => {
    onChange(overrides.filter((_, i) => i !== index));
  };

  return (
    <div>
      <div style={{ fontSize: 12, fontWeight: 600, color: '#a6adc8', marginBottom: 8 }}>
        Overrides ({overrides.length})
      </div>

      {overrides.length > 0 && (
        <div style={{ marginBottom: 12 }}>
          {overrides.map((ov, i) => (
            <div
              key={i}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 8,
                padding: '6px 8px',
                background: '#313244',
                borderRadius: 4,
                marginBottom: 4,
                fontSize: 12,
              }}
            >
              <span
                style={{
                  padding: '1px 6px',
                  borderRadius: 3,
                  background: ov.type === 'user' ? '#89b4fa22' : '#a6e3a122',
                  color: ov.type === 'user' ? '#89b4fa' : '#a6e3a1',
                  fontSize: 10,
                  fontWeight: 600,
                  textTransform: 'uppercase',
                  flexShrink: 0,
                }}
              >
                {ov.type}
              </span>
              <span style={{ color: '#cdd6f4', fontFamily: 'monospace' }}>{ov.key}</span>
              <span style={{ color: '#6c7086' }}>=</span>
              <span style={{ color: '#f9e2af', fontFamily: 'monospace', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {formatValue(ov.value)}
              </span>
              <button
                onClick={() => handleRemove(i)}
                style={{
                  background: 'none',
                  border: 'none',
                  color: '#f38ba8',
                  cursor: 'pointer',
                  fontSize: 14,
                  padding: '0 4px',
                  flexShrink: 0,
                }}
              >
                x
              </button>
            </div>
          ))}
        </div>
      )}

      <div style={{ display: 'flex', gap: 6, alignItems: 'flex-end' }}>
        <div style={{ width: 80 }}>
          <div style={{ fontSize: 10, color: '#6c7086', marginBottom: 2 }}>Type</div>
          <select
            value={newType}
            onChange={(e) => setNewType(e.target.value as 'user' | 'group')}
            style={{ ...inputStyle, width: 80 }}
          >
            <option value="user">User</option>
            <option value="group">Group</option>
          </select>
        </div>
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: 10, color: '#6c7086', marginBottom: 2 }}>Key</div>
          <input
            value={newKey}
            onChange={(e) => setNewKey(e.target.value)}
            placeholder={newType === 'user' ? 'user-id' : 'group-name'}
            style={inputStyle}
          />
        </div>
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: 10, color: '#6c7086', marginBottom: 2 }}>Value</div>
          {flagType === 'boolean' ? (
            <select
              value={newValue}
              onChange={(e) => setNewValue(e.target.value)}
              style={inputStyle}
            >
              <option value="true">true</option>
              <option value="false">false</option>
            </select>
          ) : (
            <input
              value={newValue}
              onChange={(e) => setNewValue(e.target.value)}
              placeholder="override value"
              style={inputStyle}
            />
          )}
        </div>
        <button onClick={handleAdd} style={{ ...smallBtnStyle, flexShrink: 0 }}>
          + Add
        </button>
      </div>
    </div>
  );
}
