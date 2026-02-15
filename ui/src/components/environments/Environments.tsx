import { useState, useCallback } from 'react';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type EnvName = 'development' | 'staging' | 'production';
type EnvStatus = 'healthy' | 'degraded' | 'down';

interface DeployedWorkflow {
  id: string;
  name: string;
  version: string;
  deployedAt: string;
  status: 'running' | 'stopped' | 'error';
}

interface Environment {
  name: EnvName;
  label: string;
  status: EnvStatus;
  workflows: DeployedWorkflow[];
  lastDeployment: string;
}

interface PromotionRecord {
  id: string;
  workflowName: string;
  fromEnv: EnvName;
  toEnv: EnvName;
  version: string;
  promotedBy: string;
  promotedAt: string;
  status: 'success' | 'failed' | 'rolled-back';
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const ENV_STATUS_COLORS: Record<EnvStatus, string> = {
  healthy: '#a6e3a1',
  degraded: '#f9e2af',
  down: '#f38ba8',
};

const WORKFLOW_STATUS_COLORS: Record<string, string> = {
  running: '#a6e3a1',
  stopped: '#6c7086',
  error: '#f38ba8',
};

const PROMOTION_STATUS_COLORS: Record<string, string> = {
  success: '#a6e3a1',
  failed: '#f38ba8',
  'rolled-back': '#f9e2af',
};

const INITIAL_ENVIRONMENTS: Environment[] = [
  {
    name: 'development',
    label: 'Development',
    status: 'healthy',
    lastDeployment: '2026-02-15T10:30:00Z',
    workflows: [
      { id: 'dev-1', name: 'REST API Gateway', version: '1.5.0', deployedAt: '2026-02-15T10:30:00Z', status: 'running' },
      { id: 'dev-2', name: 'Event Pipeline', version: '2.1.0-beta', deployedAt: '2026-02-15T09:15:00Z', status: 'running' },
      { id: 'dev-3', name: 'Chat Application', version: '0.8.0-alpha', deployedAt: '2026-02-14T16:45:00Z', status: 'running' },
      { id: 'dev-4', name: 'Data ETL Pipeline', version: '1.3.0-rc1', deployedAt: '2026-02-14T14:20:00Z', status: 'error' },
      { id: 'dev-5', name: 'Webhook Processor', version: '1.1.0', deployedAt: '2026-02-13T11:00:00Z', status: 'running' },
    ],
  },
  {
    name: 'staging',
    label: 'Staging',
    status: 'degraded',
    lastDeployment: '2026-02-14T18:00:00Z',
    workflows: [
      { id: 'stg-1', name: 'REST API Gateway', version: '1.4.2', deployedAt: '2026-02-14T18:00:00Z', status: 'running' },
      { id: 'stg-2', name: 'Event Pipeline', version: '2.0.1', deployedAt: '2026-02-13T14:30:00Z', status: 'running' },
      { id: 'stg-3', name: 'Webhook Processor', version: '1.0.3', deployedAt: '2026-02-12T10:00:00Z', status: 'running' },
      { id: 'stg-4', name: 'Data ETL Pipeline', version: '1.2.0', deployedAt: '2026-02-11T09:00:00Z', status: 'error' },
    ],
  },
  {
    name: 'production',
    label: 'Production',
    status: 'healthy',
    lastDeployment: '2026-02-12T14:00:00Z',
    workflows: [
      { id: 'prod-1', name: 'REST API Gateway', version: '1.4.0', deployedAt: '2026-02-12T14:00:00Z', status: 'running' },
      { id: 'prod-2', name: 'Event Pipeline', version: '2.0.0', deployedAt: '2026-02-10T16:00:00Z', status: 'running' },
      { id: 'prod-3', name: 'Webhook Processor', version: '1.0.2', deployedAt: '2026-02-08T11:30:00Z', status: 'running' },
    ],
  },
];

const INITIAL_HISTORY: PromotionRecord[] = [
  { id: 'h1', workflowName: 'REST API Gateway', fromEnv: 'staging', toEnv: 'production', version: '1.4.0', promotedBy: 'admin', promotedAt: '2026-02-12T14:00:00Z', status: 'success' },
  { id: 'h2', workflowName: 'Event Pipeline', fromEnv: 'staging', toEnv: 'production', version: '2.0.0', promotedBy: 'admin', promotedAt: '2026-02-10T16:00:00Z', status: 'success' },
  { id: 'h3', workflowName: 'Data ETL Pipeline', fromEnv: 'development', toEnv: 'staging', version: '1.2.0', promotedBy: 'dev-lead', promotedAt: '2026-02-11T09:00:00Z', status: 'success' },
  { id: 'h4', workflowName: 'REST API Gateway', fromEnv: 'development', toEnv: 'staging', version: '1.4.2', promotedBy: 'dev-lead', promotedAt: '2026-02-14T18:00:00Z', status: 'success' },
  { id: 'h5', workflowName: 'Chat Application', fromEnv: 'development', toEnv: 'staging', version: '0.7.0', promotedBy: 'dev-lead', promotedAt: '2026-02-09T12:00:00Z', status: 'failed' },
  { id: 'h6', workflowName: 'Webhook Processor', fromEnv: 'staging', toEnv: 'production', version: '1.0.2', promotedBy: 'admin', promotedAt: '2026-02-08T11:30:00Z', status: 'success' },
  { id: 'h7', workflowName: 'Event Pipeline', fromEnv: 'development', toEnv: 'staging', version: '1.9.0', promotedBy: 'dev-lead', promotedAt: '2026-02-07T10:00:00Z', status: 'rolled-back' },
];

const ENV_ORDER: EnvName[] = ['development', 'staging', 'production'];

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function formatDate(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

function getNextEnv(env: EnvName): EnvName | null {
  const idx = ENV_ORDER.indexOf(env);
  return idx < ENV_ORDER.length - 1 ? ENV_ORDER[idx + 1] : null;
}

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

function StatusIndicator({ status }: { status: EnvStatus }) {
  const color = ENV_STATUS_COLORS[status];
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
      <span
        style={{
          width: 8,
          height: 8,
          borderRadius: '50%',
          background: color,
          display: 'inline-block',
          boxShadow: `0 0 6px ${color}66`,
        }}
      />
      <span style={{ fontSize: 11, color, fontWeight: 600, textTransform: 'capitalize' }}>{status}</span>
    </span>
  );
}

function WorkflowStatusBadge({ status }: { status: string }) {
  const color = WORKFLOW_STATUS_COLORS[status] || '#6c7086';
  return (
    <span
      style={{
        display: 'inline-block',
        padding: '1px 6px',
        borderRadius: 3,
        fontSize: 10,
        fontWeight: 600,
        background: color + '22',
        color,
        textTransform: 'capitalize',
      }}
    >
      {status}
    </span>
  );
}

function EnvironmentColumn({
  env,
  onPromote,
}: {
  env: Environment;
  onPromote: (workflow: DeployedWorkflow, from: EnvName) => void;
}) {
  const nextEnv = getNextEnv(env.name);

  return (
    <div
      style={{
        flex: 1,
        minWidth: 280,
        background: '#313244',
        borderRadius: 8,
        border: '1px solid #45475a',
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
      }}
    >
      {/* Column header */}
      <div
        style={{
          padding: '12px 16px',
          background: '#181825',
          borderBottom: '1px solid #45475a',
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
        }}
      >
        <div>
          <div style={{ color: '#cdd6f4', fontWeight: 600, fontSize: 14 }}>{env.label}</div>
          <div style={{ fontSize: 11, color: '#6c7086', marginTop: 2 }}>
            Last deploy: {formatDate(env.lastDeployment)}
          </div>
        </div>
        <StatusIndicator status={env.status} />
      </div>

      {/* Workflow list */}
      <div style={{ flex: 1, overflow: 'auto', padding: 8 }}>
        {env.workflows.map((wf) => (
          <div
            key={wf.id}
            style={{
              padding: '10px 12px',
              borderRadius: 6,
              background: '#1e1e2e',
              marginBottom: 6,
              border: '1px solid transparent',
              transition: 'border-color 0.15s',
            }}
            onMouseEnter={(e) => (e.currentTarget.style.borderColor = '#45475a')}
            onMouseLeave={(e) => (e.currentTarget.style.borderColor = 'transparent')}
          >
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 4 }}>
              <span style={{ color: '#cdd6f4', fontSize: 13, fontWeight: 500 }}>{wf.name}</span>
              <WorkflowStatusBadge status={wf.status} />
            </div>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <div style={{ fontSize: 11, color: '#6c7086' }}>
                <span style={{ color: '#89b4fa', fontWeight: 500 }}>v{wf.version}</span>
                <span style={{ margin: '0 6px' }}>|</span>
                {formatDate(wf.deployedAt)}
              </div>
              {nextEnv && (
                <button
                  onClick={() => onPromote(wf, env.name)}
                  title={`Promote to ${nextEnv}`}
                  style={{
                    padding: '2px 8px',
                    borderRadius: 3,
                    border: '1px solid #45475a',
                    fontSize: 10,
                    fontWeight: 600,
                    cursor: 'pointer',
                    background: 'transparent',
                    color: '#89b4fa',
                    transition: 'background 0.15s',
                  }}
                  onMouseEnter={(e) => (e.currentTarget.style.background = '#89b4fa22')}
                  onMouseLeave={(e) => (e.currentTarget.style.background = 'transparent')}
                >
                  Promote
                </button>
              )}
            </div>
          </div>
        ))}
        {env.workflows.length === 0 && (
          <div style={{ color: '#6c7086', fontSize: 12, textAlign: 'center', padding: 20 }}>
            No workflows deployed
          </div>
        )}
      </div>

      {/* Summary footer */}
      <div
        style={{
          padding: '8px 16px',
          borderTop: '1px solid #45475a',
          background: '#181825',
          fontSize: 11,
          color: '#6c7086',
        }}
      >
        {env.workflows.length} workflow{env.workflows.length !== 1 ? 's' : ''} deployed
      </div>
    </div>
  );
}

function PromotionConfirmDialog({
  workflow,
  fromEnv,
  toEnv,
  onConfirm,
  onCancel,
}: {
  workflow: DeployedWorkflow;
  fromEnv: string;
  toEnv: string;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  return (
    <div
      onClick={onCancel}
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0,0,0,0.6)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1000,
      }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          background: '#1e1e2e',
          border: '1px solid #45475a',
          borderRadius: 12,
          padding: 24,
          width: '90%',
          maxWidth: 440,
        }}
      >
        <h3 style={{ color: '#cdd6f4', margin: '0 0 12px', fontSize: 16, fontWeight: 600 }}>
          Confirm Promotion
        </h3>
        <p style={{ color: '#a6adc8', fontSize: 13, lineHeight: 1.5, marginBottom: 8 }}>
          You are about to promote:
        </p>
        <div
          style={{
            background: '#313244',
            borderRadius: 6,
            padding: 12,
            marginBottom: 16,
          }}
        >
          <div style={{ color: '#cdd6f4', fontWeight: 600, fontSize: 14, marginBottom: 4 }}>{workflow.name}</div>
          <div style={{ fontSize: 12, color: '#6c7086' }}>
            Version <span style={{ color: '#89b4fa' }}>v{workflow.version}</span>
          </div>
          <div style={{ fontSize: 12, color: '#6c7086', marginTop: 4, display: 'flex', alignItems: 'center', gap: 6 }}>
            <span style={{ textTransform: 'capitalize' }}>{fromEnv}</span>
            <span style={{ color: '#89b4fa' }}>&rarr;</span>
            <span style={{ textTransform: 'capitalize', fontWeight: 600, color: '#f9e2af' }}>{toEnv}</span>
          </div>
        </div>

        {toEnv === 'production' && (
          <div
            style={{
              background: '#f38ba822',
              border: '1px solid #f38ba844',
              borderRadius: 6,
              padding: 10,
              marginBottom: 16,
              fontSize: 12,
              color: '#f38ba8',
              lineHeight: 1.4,
            }}
          >
            Warning: You are promoting to production. This action will affect live traffic.
          </div>
        )}

        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
          <button
            onClick={onCancel}
            style={{
              padding: '8px 20px',
              borderRadius: 6,
              border: '1px solid #45475a',
              fontSize: 13,
              fontWeight: 600,
              cursor: 'pointer',
              background: 'transparent',
              color: '#a6adc8',
            }}
          >
            Cancel
          </button>
          <button
            onClick={onConfirm}
            style={{
              padding: '8px 24px',
              borderRadius: 6,
              border: 'none',
              fontSize: 13,
              fontWeight: 600,
              cursor: 'pointer',
              background: toEnv === 'production' ? '#f9e2af' : '#89b4fa',
              color: '#1e1e2e',
            }}
          >
            Promote
          </button>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export default function Environments() {
  const [environments, setEnvironments] = useState<Environment[]>(INITIAL_ENVIRONMENTS);
  const [history, setHistory] = useState<PromotionRecord[]>(INITIAL_HISTORY);
  const [promotionTarget, setPromotionTarget] = useState<{
    workflow: DeployedWorkflow;
    fromEnv: EnvName;
    toEnv: EnvName;
  } | null>(null);

  const handlePromote = useCallback((workflow: DeployedWorkflow, fromEnv: EnvName) => {
    const toEnv = getNextEnv(fromEnv);
    if (!toEnv) return;
    setPromotionTarget({ workflow, fromEnv, toEnv });
  }, []);

  const confirmPromotion = useCallback(() => {
    if (!promotionTarget) return;
    const { workflow, fromEnv, toEnv } = promotionTarget;

    // Add workflow to target environment
    setEnvironments((prev) =>
      prev.map((env) => {
        if (env.name !== toEnv) return env;
        const now = new Date().toISOString();
        // Replace existing workflow with same name or add new
        const existingIdx = env.workflows.findIndex((w) => w.name === workflow.name);
        const newWf: DeployedWorkflow = {
          id: `${toEnv.slice(0, 3)}-${Date.now()}`,
          name: workflow.name,
          version: workflow.version,
          deployedAt: now,
          status: 'running',
        };
        const workflows =
          existingIdx >= 0
            ? env.workflows.map((w, i) => (i === existingIdx ? newWf : w))
            : [...env.workflows, newWf];
        return { ...env, workflows, lastDeployment: now };
      }),
    );

    // Add to history
    const record: PromotionRecord = {
      id: `h-${Date.now()}`,
      workflowName: workflow.name,
      fromEnv,
      toEnv,
      version: workflow.version,
      promotedBy: 'admin',
      promotedAt: new Date().toISOString(),
      status: 'success',
    };
    setHistory((prev) => [record, ...prev]);

    setPromotionTarget(null);
  }, [promotionTarget]);

  return (
    <div
      style={{
        flex: 1,
        background: '#1e1e2e',
        overflow: 'auto',
        padding: 24,
      }}
    >
      <h2 style={{ color: '#cdd6f4', margin: '0 0 4px', fontSize: 20, fontWeight: 600 }}>
        Environment Promotion
      </h2>
      <p style={{ color: '#6c7086', fontSize: 13, margin: '0 0 20px' }}>
        Manage deployments across environments and promote workflows from dev to production.
      </p>

      {/* Environment columns */}
      <div style={{ display: 'flex', gap: 16, marginBottom: 32, minHeight: 300 }}>
        {environments.map((env) => (
          <EnvironmentColumn key={env.name} env={env} onPromote={handlePromote} />
        ))}
      </div>

      {/* Promotion arrows between columns (visual hint) */}
      <div style={{ display: 'flex', justifyContent: 'center', gap: 12, marginBottom: 24 }}>
        <span style={{ fontSize: 12, color: '#6c7086' }}>
          Development
          <span style={{ color: '#89b4fa', margin: '0 8px' }}>&rarr;</span>
          Staging
          <span style={{ color: '#89b4fa', margin: '0 8px' }}>&rarr;</span>
          Production
        </span>
      </div>

      {/* Deployment history */}
      <h3 style={{ color: '#cdd6f4', fontSize: 14, fontWeight: 600, marginBottom: 12 }}>Deployment History</h3>
      <div style={{ background: '#313244', borderRadius: 8, overflow: 'hidden' }}>
        {/* Table header */}
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: '2fr 1fr 1.5fr 1fr 1fr 1fr',
            padding: '10px 16px',
            background: '#181825',
            fontSize: 11,
            color: '#a6adc8',
            fontWeight: 600,
          }}
        >
          <span>Workflow</span>
          <span>Version</span>
          <span>Promotion</span>
          <span>By</span>
          <span>Date</span>
          <span>Status</span>
        </div>

        {/* Table body */}
        {history.map((record, i) => (
          <div
            key={record.id}
            style={{
              display: 'grid',
              gridTemplateColumns: '2fr 1fr 1.5fr 1fr 1fr 1fr',
              padding: '8px 16px',
              borderBottom: i < history.length - 1 ? '1px solid #45475a' : 'none',
              fontSize: 13,
              background: i % 2 === 0 ? 'transparent' : '#181825',
              alignItems: 'center',
            }}
          >
            <span style={{ color: '#cdd6f4' }}>{record.workflowName}</span>
            <span style={{ color: '#89b4fa', fontSize: 12 }}>v{record.version}</span>
            <span style={{ color: '#a6adc8', fontSize: 12, textTransform: 'capitalize' }}>
              {record.fromEnv} &rarr; {record.toEnv}
            </span>
            <span style={{ color: '#a6adc8', fontSize: 12 }}>{record.promotedBy}</span>
            <span style={{ color: '#6c7086', fontSize: 12 }}>{formatDate(record.promotedAt)}</span>
            <span>
              <span
                style={{
                  display: 'inline-block',
                  padding: '1px 6px',
                  borderRadius: 3,
                  fontSize: 10,
                  fontWeight: 600,
                  background: (PROMOTION_STATUS_COLORS[record.status] || '#6c7086') + '22',
                  color: PROMOTION_STATUS_COLORS[record.status] || '#6c7086',
                  textTransform: 'capitalize',
                }}
              >
                {record.status}
              </span>
            </span>
          </div>
        ))}
        {history.length === 0 && (
          <div style={{ color: '#6c7086', fontSize: 13, padding: 20, textAlign: 'center' }}>
            No deployment history.
          </div>
        )}
      </div>

      {/* Promotion confirmation dialog */}
      {promotionTarget && (
        <PromotionConfirmDialog
          workflow={promotionTarget.workflow}
          fromEnv={promotionTarget.fromEnv}
          toEnv={promotionTarget.toEnv}
          onConfirm={confirmPromotion}
          onCancel={() => setPromotionTarget(null)}
        />
      )}
    </div>
  );
}
