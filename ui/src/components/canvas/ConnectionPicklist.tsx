import { useState, useEffect, useRef, useMemo, useCallback } from 'react';
import { CATEGORIES, CATEGORY_COLORS } from '../../types/workflow.ts';
import type { ModuleCategory, ModuleTypeInfo } from '../../types/workflow.ts';
import useModuleSchemaStore from '../../store/moduleSchemaStore.ts';
import {
  getCompatibleModuleTypes,
  getCompatibleSourceModuleTypes,
} from '../../utils/connectionCompatibility.ts';

interface ConnectionPicklistProps {
  position: { x: number; y: number };
  connectingFrom: {
    nodeId: string;
    handleId: string | null;
    handleType: 'source' | 'target';
    outputTypes: string[];
  };
  onSelect: (moduleType: string) => void;
  onClose: () => void;
}

export default function ConnectionPicklist({
  position,
  connectingFrom,
  onSelect,
  onClose,
}: ConnectionPicklistProps) {
  const [search, setSearch] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const containerRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const itemRefs = useRef<(HTMLDivElement | null)[]>([]);

  const moduleTypes = useModuleSchemaStore((s) => s.moduleTypes);

  // Get compatible module types based on the output/input types
  const compatibleTypes = useMemo(() => {
    if (connectingFrom.outputTypes.length === 0) return moduleTypes;

    let results: ModuleTypeInfo[] = [];
    for (const type of connectingFrom.outputTypes) {
      if (connectingFrom.handleType === 'source') {
        results = results.concat(getCompatibleModuleTypes(type, moduleTypes));
      } else {
        results = results.concat(getCompatibleSourceModuleTypes(type, moduleTypes));
      }
    }

    // Deduplicate
    const seen = new Set<string>();
    return results.filter((t) => {
      if (seen.has(t.type)) return false;
      seen.add(t.type);
      return true;
    });
  }, [connectingFrom, moduleTypes]);

  // Filter by search
  const searchLower = search.toLowerCase();
  const filtered = useMemo(() => {
    if (!searchLower) return compatibleTypes;
    return compatibleTypes.filter(
      (t) =>
        t.label.toLowerCase().includes(searchLower) ||
        t.type.toLowerCase().includes(searchLower),
    );
  }, [compatibleTypes, searchLower]);

  // Group by category
  const grouped = useMemo(() => {
    return CATEGORIES.map((cat) => ({
      ...cat,
      types: filtered.filter((t) => t.category === cat.key),
    })).filter((cat) => cat.types.length > 0);
  }, [filtered]);

  // Flat list for keyboard navigation
  const flatList = useMemo(() => {
    return grouped.flatMap((cat) => cat.types);
  }, [grouped]);

  // Reset selection when filtered list changes
  useEffect(() => {
    setSelectedIndex(0);
  }, [flatList.length]);

  // Focus search input on mount
  useEffect(() => {
    searchRef.current?.focus();
  }, []);

  // Click outside to close
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        onClose();
      }
    };
    // Use a timeout to avoid catching the same mouseup that opened the picklist
    const timer = setTimeout(() => {
      document.addEventListener('mousedown', handler);
    }, 100);
    return () => {
      clearTimeout(timer);
      document.removeEventListener('mousedown', handler);
    };
  }, [onClose]);

  // Keyboard navigation
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        onClose();
      } else if (e.key === 'ArrowDown') {
        e.preventDefault();
        setSelectedIndex((prev) => {
          const next = Math.min(prev + 1, flatList.length - 1);
          itemRefs.current[next]?.scrollIntoView({ block: 'nearest' });
          return next;
        });
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        setSelectedIndex((prev) => {
          const next = Math.max(prev - 1, 0);
          itemRefs.current[next]?.scrollIntoView({ block: 'nearest' });
          return next;
        });
      } else if (e.key === 'Enter' && flatList.length > 0) {
        e.preventDefault();
        onSelect(flatList[selectedIndex].type);
      }
    },
    [flatList, selectedIndex, onSelect, onClose],
  );

  // Clamp position to viewport
  const panelWidth = 260;
  const panelMaxHeight = 340;
  const clampedX = Math.min(position.x, window.innerWidth - panelWidth - 20);
  const clampedY = Math.min(position.y, window.innerHeight - panelMaxHeight - 20);

  const typeLabel = connectingFrom.outputTypes.join(' | ');

  let itemIndex = 0;

  return (
    <div
      ref={containerRef}
      onKeyDown={handleKeyDown}
      style={{
        position: 'absolute',
        left: Math.max(clampedX, 10),
        top: Math.max(clampedY, 10),
        width: panelWidth,
        maxHeight: panelMaxHeight,
        background: '#181825',
        border: '1px solid #313244',
        borderRadius: 8,
        boxShadow: '0 8px 32px rgba(0,0,0,0.5)',
        zIndex: 100,
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
        animation: 'picklist-enter 0.15s ease-out',
        fontFamily: 'system-ui, sans-serif',
      }}
    >
      {/* Header */}
      <div
        style={{
          padding: '8px 12px 4px',
          borderBottom: '1px solid #313244',
        }}
      >
        <div style={{ fontSize: 11, color: '#6c7086', marginBottom: 4 }}>
          Connect {connectingFrom.handleType === 'source' ? 'output' : 'input'}: <span style={{ color: '#89b4fa' }}>{typeLabel}</span>
        </div>
        <input
          ref={searchRef}
          type="text"
          placeholder="Search compatible modules..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          style={{
            width: '100%',
            padding: '6px 8px',
            fontSize: 12,
            background: '#1e1e2e',
            border: '1px solid #313244',
            borderRadius: 4,
            color: '#cdd6f4',
            outline: 'none',
            boxSizing: 'border-box',
          }}
        />
      </div>

      {/* List */}
      <div style={{ overflowY: 'auto', flex: 1, padding: '4px 0' }}>
        {grouped.length === 0 && (
          <div style={{ padding: '12px 16px', color: '#6c7086', fontSize: 12, textAlign: 'center' }}>
            No compatible modules found
          </div>
        )}
        {grouped.map((cat) => (
          <div key={cat.key}>
            <div
              style={{
                padding: '4px 12px',
                fontSize: 10,
                fontWeight: 600,
                color: CATEGORY_COLORS[cat.key as ModuleCategory],
                textTransform: 'uppercase',
                letterSpacing: '0.5px',
              }}
            >
              {cat.label}
            </div>
            {cat.types.map((t) => {
              const idx = itemIndex++;
              const isActive = idx === selectedIndex;
              return (
                <div
                  key={t.type}
                  ref={(el) => { itemRefs.current[idx] = el; }}
                  onClick={() => onSelect(t.type)}
                  onMouseEnter={() => setSelectedIndex(idx)}
                  style={{
                    padding: '5px 12px 5px 20px',
                    cursor: 'pointer',
                    fontSize: 12,
                    color: isActive ? '#cdd6f4' : '#bac2de',
                    background: isActive ? '#313244' : 'transparent',
                    display: 'flex',
                    alignItems: 'center',
                    gap: 6,
                    transition: 'background 0.1s',
                  }}
                >
                  <span
                    style={{
                      width: 8,
                      height: 8,
                      borderRadius: '50%',
                      background: CATEGORY_COLORS[cat.key as ModuleCategory],
                      flexShrink: 0,
                    }}
                  />
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ fontWeight: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {t.label}
                    </div>
                    <div style={{ fontSize: 10, color: '#585b70', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {t.type}
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        ))}
      </div>

      {/* Footer hint */}
      <div
        style={{
          padding: '4px 12px',
          borderTop: '1px solid #313244',
          fontSize: 10,
          color: '#585b70',
          display: 'flex',
          gap: 8,
        }}
      >
        <span>Up/Down to navigate</span>
        <span>Enter to select</span>
        <span>Esc to close</span>
      </div>
    </div>
  );
}
