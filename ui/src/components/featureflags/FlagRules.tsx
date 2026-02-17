import { useState } from 'react';
import type { TargetingRule, TargetingCondition, FlagType } from '../../store/featureFlagStore.ts';

interface FlagRulesProps {
  rules: TargetingRule[];
  flagType: FlagType;
  onChange: (rules: TargetingRule[]) => void;
}

const OPERATORS: { value: TargetingCondition['operator']; label: string }[] = [
  { value: 'eq', label: '=' },
  { value: 'neq', label: '!=' },
  { value: 'in', label: 'in' },
  { value: 'contains', label: 'contains' },
  { value: 'startsWith', label: 'starts with' },
  { value: 'gt', label: '>' },
  { value: 'lt', label: '<' },
];

const inputStyle: React.CSSProperties = {
  padding: '5px 8px',
  borderRadius: 4,
  border: '1px solid #45475a',
  background: '#313244',
  color: '#cdd6f4',
  fontSize: 12,
  outline: 'none',
  boxSizing: 'border-box',
};

const smallBtnStyle: React.CSSProperties = {
  padding: '3px 8px',
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

function emptyCondition(): TargetingCondition {
  return { attribute: '', operator: 'eq', value: '' };
}

function emptyRule(flagType: FlagType): TargetingRule {
  const defaultValue = flagType === 'boolean' ? true : flagType === 'number' ? 0 : '';
  return { conditions: [emptyCondition()], value: defaultValue };
}

function ConditionEditor({
  condition,
  onChange,
  onRemove,
}: {
  condition: TargetingCondition;
  onChange: (c: TargetingCondition) => void;
  onRemove: () => void;
}) {
  return (
    <div style={{ display: 'flex', gap: 4, alignItems: 'center', marginBottom: 4 }}>
      <input
        value={condition.attribute}
        onChange={(e) => onChange({ ...condition, attribute: e.target.value })}
        placeholder="attribute"
        style={{ ...inputStyle, width: 120 }}
      />
      <select
        value={condition.operator}
        onChange={(e) => onChange({ ...condition, operator: e.target.value as TargetingCondition['operator'] })}
        style={{ ...inputStyle, width: 100 }}
      >
        {OPERATORS.map((op) => (
          <option key={op.value} value={op.value}>{op.label}</option>
        ))}
      </select>
      <input
        value={condition.value}
        onChange={(e) => onChange({ ...condition, value: e.target.value })}
        placeholder="value"
        style={{ ...inputStyle, flex: 1 }}
      />
      <button
        onClick={onRemove}
        style={{
          background: 'none',
          border: 'none',
          color: '#f38ba8',
          cursor: 'pointer',
          fontSize: 13,
          padding: '0 4px',
          flexShrink: 0,
        }}
      >
        x
      </button>
    </div>
  );
}

function RuleEditor({
  rule,
  index,
  flagType,
  onChange,
  onRemove,
  onMoveUp,
  onMoveDown,
  isFirst,
  isLast,
}: {
  rule: TargetingRule;
  index: number;
  flagType: FlagType;
  onChange: (r: TargetingRule) => void;
  onRemove: () => void;
  onMoveUp: () => void;
  onMoveDown: () => void;
  isFirst: boolean;
  isLast: boolean;
}) {
  const updateCondition = (ci: number, c: TargetingCondition) => {
    const conditions = rule.conditions.map((cond, i) => (i === ci ? c : cond));
    onChange({ ...rule, conditions });
  };

  const removeCondition = (ci: number) => {
    if (rule.conditions.length <= 1) return;
    onChange({ ...rule, conditions: rule.conditions.filter((_, i) => i !== ci) });
  };

  const addCondition = () => {
    onChange({ ...rule, conditions: [...rule.conditions, emptyCondition()] });
  };

  return (
    <div
      style={{
        background: '#1e1e2e',
        border: '1px solid #45475a',
        borderRadius: 6,
        padding: 12,
        marginBottom: 8,
      }}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
        <span style={{ fontSize: 12, fontWeight: 600, color: '#a6adc8' }}>
          Rule {index + 1}
        </span>
        <div style={{ display: 'flex', gap: 4 }}>
          <button
            onClick={onMoveUp}
            disabled={isFirst}
            style={{ ...smallBtnStyle, opacity: isFirst ? 0.3 : 1, cursor: isFirst ? 'default' : 'pointer' }}
            title="Move up"
          >
            ^
          </button>
          <button
            onClick={onMoveDown}
            disabled={isLast}
            style={{ ...smallBtnStyle, opacity: isLast ? 0.3 : 1, cursor: isLast ? 'default' : 'pointer' }}
            title="Move down"
          >
            v
          </button>
          <button
            onClick={onRemove}
            style={{ ...smallBtnStyle, color: '#f38ba8', borderColor: '#f38ba844' }}
          >
            Remove
          </button>
        </div>
      </div>

      {/* Conditions */}
      <div style={{ marginBottom: 8 }}>
        <div style={{ fontSize: 10, color: '#6c7086', marginBottom: 4, textTransform: 'uppercase', letterSpacing: 0.5 }}>
          Conditions (all must match)
        </div>
        {rule.conditions.map((c, ci) => (
          <ConditionEditor
            key={ci}
            condition={c}
            onChange={(updated) => updateCondition(ci, updated)}
            onRemove={() => removeCondition(ci)}
          />
        ))}
        <button onClick={addCondition} style={{ ...smallBtnStyle, marginTop: 4, fontSize: 10 }}>
          + Condition
        </button>
      </div>

      {/* Result value */}
      <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: 10, color: '#6c7086', marginBottom: 2 }}>Result Value</div>
          {flagType === 'boolean' ? (
            <select
              value={String(rule.value)}
              onChange={(e) => onChange({ ...rule, value: e.target.value === 'true' })}
              style={{ ...inputStyle, width: '100%' }}
            >
              <option value="true">true</option>
              <option value="false">false</option>
            </select>
          ) : (
            <input
              value={formatValue(rule.value)}
              onChange={(e) => onChange({ ...rule, value: parseValue(e.target.value, flagType) })}
              placeholder="result value"
              style={{ ...inputStyle, width: '100%' }}
            />
          )}
        </div>
        <div style={{ width: 140 }}>
          <div style={{ fontSize: 10, color: '#6c7086', marginBottom: 2 }}>Rollout %</div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
            <input
              type="range"
              min={0}
              max={100}
              value={rule.percentage ?? 100}
              onChange={(e) => onChange({ ...rule, percentage: Number(e.target.value) })}
              style={{ flex: 1 }}
            />
            <span style={{ fontSize: 11, color: '#cdd6f4', width: 30, textAlign: 'right' }}>
              {rule.percentage ?? 100}%
            </span>
          </div>
        </div>
      </div>
    </div>
  );
}

export default function FlagRules({ rules, flagType, onChange }: FlagRulesProps) {
  const [collapsed, setCollapsed] = useState(false);

  const addRule = () => {
    onChange([...rules, emptyRule(flagType)]);
  };

  const updateRule = (index: number, rule: TargetingRule) => {
    onChange(rules.map((r, i) => (i === index ? rule : r)));
  };

  const removeRule = (index: number) => {
    onChange(rules.filter((_, i) => i !== index));
  };

  const moveRule = (from: number, to: number) => {
    if (to < 0 || to >= rules.length) return;
    const updated = [...rules];
    const [moved] = updated.splice(from, 1);
    updated.splice(to, 0, moved);
    onChange(updated);
  };

  return (
    <div>
      <div
        style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8, cursor: 'pointer' }}
        onClick={() => setCollapsed(!collapsed)}
      >
        <div style={{ fontSize: 12, fontWeight: 600, color: '#a6adc8' }}>
          Targeting Rules ({rules.length})
        </div>
        <span style={{ fontSize: 10, color: '#6c7086' }}>
          {collapsed ? '[+]' : '[-]'}
        </span>
      </div>

      {!collapsed && (
        <>
          {rules.map((rule, i) => (
            <RuleEditor
              key={i}
              rule={rule}
              index={i}
              flagType={flagType}
              onChange={(r) => updateRule(i, r)}
              onRemove={() => removeRule(i)}
              onMoveUp={() => moveRule(i, i - 1)}
              onMoveDown={() => moveRule(i, i + 1)}
              isFirst={i === 0}
              isLast={i === rules.length - 1}
            />
          ))}
          <button onClick={addRule} style={smallBtnStyle}>
            + Add Rule
          </button>
        </>
      )}
    </div>
  );
}
