import { useEffect, useState } from 'react';
import useObservabilityStore from '../../store/observabilityStore.ts';
import { apiListWorkflows } from '../../utils/api.ts';
import type { ApiWorkflowRecord } from '../../utils/api.ts';

export default function WorkflowPickerBar() {
  const selectedWorkflowId = useObservabilityStore((s) => s.selectedWorkflowId);
  const setSelectedWorkflowId = useObservabilityStore((s) => s.setSelectedWorkflowId);
  const [workflows, setWorkflows] = useState<ApiWorkflowRecord[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    apiListWorkflows()
      .then((data) => {
        if (!cancelled) setWorkflows(data);
      })
      .catch(() => {
        // ignore — workflows may not be available
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => { cancelled = true; };
  }, []);

  return (
    <div
      style={{
        padding: '6px 16px',
        background: '#181825',
        borderBottom: '1px solid #313244',
        display: 'flex',
        alignItems: 'center',
        gap: 10,
        flexShrink: 0,
      }}
    >
      <span style={{ color: '#a6adc8', fontSize: 11, fontWeight: 600 }}>Workflow:</span>
      <select
        value={selectedWorkflowId ?? ''}
        onChange={(e) => setSelectedWorkflowId(e.target.value || null)}
        style={{
          padding: '4px 8px',
          background: '#1e1e2e',
          border: '1px solid #313244',
          borderRadius: 4,
          color: '#cdd6f4',
          fontSize: 12,
          outline: 'none',
          minWidth: 200,
        }}
      >
        <option value="">
          {loading ? 'Loading...' : '-- Select a workflow --'}
        </option>
        {workflows.map((wf) => (
          <option key={wf.id} value={wf.id}>
            {wf.name} {wf.is_system ? '(System)' : ''}
          </option>
        ))}
      </select>
      {selectedWorkflowId && (
        <button
          onClick={() => setSelectedWorkflowId(null)}
          style={{
            background: 'none',
            border: 'none',
            color: '#585b70',
            cursor: 'pointer',
            fontSize: 11,
            padding: '2px 6px',
          }}
        >
          Clear
        </button>
      )}
    </div>
  );
}
