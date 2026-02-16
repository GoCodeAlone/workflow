import { useState, useEffect, useMemo } from 'react';
import useModuleSchemaStore from '../../store/moduleSchemaStore.ts';
import type { Node } from '@xyflow/react';

interface DelegateServicePickerProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  nodes: Node[];
  currentNodeId: string;
}

export default function DelegateServicePicker({
  value,
  onChange,
  placeholder,
  nodes,
  currentNodeId,
}: DelegateServicePickerProps) {
  const [expanded, setExpanded] = useState(false);
  const services = useModuleSchemaStore((s) => s.services);
  const servicesLoaded = useModuleSchemaStore((s) => s.servicesLoaded);
  const fetchServices = useModuleSchemaStore((s) => s.fetchServices);

  useEffect(() => {
    if (!servicesLoaded) fetchServices();
  }, [servicesLoaded, fetchServices]);

  // Canvas nodes that could be delegates (implement http.Handler pattern)
  const canvasServices = useMemo(() => {
    return nodes
      .filter((n) => n.id !== currentNodeId)
      .map((n) => ({
        name: (n.data as { label: string }).label,
        source: 'canvas' as const,
        type: (n.data as { moduleType?: string }).moduleType || '',
        implements: [] as string[],
      }));
  }, [nodes, currentNodeId]);

  // Server-registered services (any service can be a delegate target)
  const serverServices = useMemo(() => {
    return services.map((s) => ({
      name: s.name,
      source: 'server' as const,
      type: s.type || '',
      implements: s.implements || [],
    }));
  }, [services]);

  // Combine and deduplicate
  const allServices = useMemo(() => {
    const seen = new Set<string>();
    const result: { name: string; source: 'canvas' | 'server'; type: string; implements: string[] }[] = [];
    for (const s of [...canvasServices, ...serverServices]) {
      if (!seen.has(s.name)) {
        seen.add(s.name);
        result.push(s);
      }
    }
    return result.sort((a, b) => a.name.localeCompare(b.name));
  }, [canvasServices, serverServices]);

  const inputStyle: React.CSSProperties = {
    width: '100%',
    padding: '4px 8px',
    background: '#1e1e2e',
    border: '1px solid #313244',
    borderRadius: 4,
    color: '#cdd6f4',
    fontSize: 12,
    boxSizing: 'border-box',
  };

  return (
    <div style={{ position: 'relative' }}>
      <div style={{ display: 'flex', gap: 4 }}>
        <input
          type="text"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          style={{ ...inputStyle, flex: 1 }}
        />
        <button
          onClick={() => setExpanded(!expanded)}
          style={{
            padding: '4px 8px',
            background: expanded ? '#45475a' : '#313244',
            border: '1px solid #45475a',
            borderRadius: 4,
            color: '#cdd6f4',
            fontSize: 11,
            cursor: 'pointer',
          }}
          title="Pick a service"
        >
          {expanded ? '\u25B2' : '\u25BC'}
        </button>
      </div>
      {expanded && (
        <div
          style={{
            marginTop: 4,
            background: '#1e1e2e',
            border: '1px solid #313244',
            borderRadius: 4,
            maxHeight: 160,
            overflowY: 'auto',
          }}
        >
          {allServices.length === 0 ? (
            <div style={{ padding: '8px 10px', color: '#585b70', fontSize: 11 }}>
              No services available
            </div>
          ) : (
            allServices.map((svc) => (
              <div
                key={svc.name}
                onClick={() => {
                  onChange(svc.name);
                  setExpanded(false);
                }}
                style={{
                  padding: '5px 10px',
                  cursor: 'pointer',
                  fontSize: 12,
                  color: value === svc.name ? '#a6e3a1' : '#cdd6f4',
                  background: value === svc.name ? '#313244' : 'transparent',
                  display: 'flex',
                  flexDirection: 'column',
                  gap: 2,
                }}
                onMouseEnter={(e) => {
                  (e.currentTarget as HTMLDivElement).style.background = '#313244';
                }}
                onMouseLeave={(e) => {
                  (e.currentTarget as HTMLDivElement).style.background =
                    value === svc.name ? '#313244' : 'transparent';
                }}
              >
                <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                  <span
                    style={{
                      fontSize: 9,
                      padding: '1px 4px',
                      borderRadius: 3,
                      background: svc.source === 'canvas' ? '#45475a' : '#313244',
                      color: svc.source === 'canvas' ? '#89b4fa' : '#a6adc8',
                    }}
                  >
                    {svc.source === 'canvas' ? 'node' : 'svc'}
                  </span>
                  {svc.name}
                </div>
                {(svc.type || svc.implements.length > 0) && (
                  <div style={{ display: 'flex', alignItems: 'center', gap: 4, paddingLeft: 2 }}>
                    {svc.type && (
                      <span style={{ fontSize: 9, fontFamily: 'monospace', color: '#585b70' }}>
                        {svc.type}
                      </span>
                    )}
                    {svc.implements.map((iface) => (
                      <span
                        key={iface}
                        style={{
                          fontSize: 8,
                          padding: '0 3px',
                          borderRadius: 2,
                          background: '#45475a',
                          color: '#a6adc8',
                        }}
                      >
                        {iface}
                      </span>
                    ))}
                  </div>
                )}
              </div>
            ))
          )}
        </div>
      )}
    </div>
  );
}
