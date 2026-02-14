import { useEffect, useState } from 'react';
import { apiListVersions } from '../../utils/api.ts';
import type { ApiWorkflowVersion } from '../../utils/api.ts';

interface WorkflowHistoryProps {
  workflowId: string;
  workflowName: string;
  onClose: () => void;
  onRestore?: (configYaml: string, version: number) => void;
}

export default function WorkflowHistory({ workflowId, workflowName, onClose, onRestore }: WorkflowHistoryProps) {
  const [versions, setVersions] = useState<ApiWorkflowVersion[]>([]);
  const [loading, setLoading] = useState(true);
  const [expandedVersion, setExpandedVersion] = useState<number | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    apiListVersions(workflowId)
      .then((data) => {
        if (!cancelled) setVersions(data);
      })
      .catch(() => {
        // ignore
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => { cancelled = true; };
  }, [workflowId]);

  const formatDate = (iso: string) => {
    try {
      const d = new Date(iso);
      return d.toLocaleString(undefined, {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      });
    } catch {
      return iso;
    }
  };

  const formatUser = (userId: string) => {
    if (!userId || userId === 'system') return 'System';
    // Truncate UUID to first 8 chars for readability
    if (userId.length > 20) return userId.slice(0, 8) + '...';
    return userId;
  };

  return (
    <div
      style={{
        position: 'fixed',
        top: 0,
        left: 0,
        right: 0,
        bottom: 0,
        background: 'rgba(0,0,0,0.6)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 9999,
      }}
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        style={{
          background: '#1e1e2e',
          border: '1px solid #313244',
          borderRadius: 10,
          width: 600,
          maxHeight: '80vh',
          display: 'flex',
          flexDirection: 'column',
          overflow: 'hidden',
        }}
      >
        {/* Header */}
        <div
          style={{
            padding: '16px 20px',
            borderBottom: '1px solid #313244',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
          }}
        >
          <div>
            <div style={{ color: '#cdd6f4', fontWeight: 700, fontSize: 16 }}>Edit History</div>
            <div style={{ color: '#6c7086', fontSize: 12, marginTop: 2 }}>{workflowName}</div>
          </div>
          <button
            onClick={onClose}
            style={{
              background: 'none',
              border: 'none',
              color: '#585b70',
              cursor: 'pointer',
              fontSize: 18,
              padding: '0 4px',
            }}
          >
            x
          </button>
        </div>

        {/* Body */}
        <div style={{ flex: 1, overflowY: 'auto', padding: '12px 0' }}>
          {loading && (
            <div style={{ padding: 20, textAlign: 'center', color: '#6c7086', fontSize: 13 }}>
              Loading version history...
            </div>
          )}
          {!loading && versions.length === 0 && (
            <div style={{ padding: 20, textAlign: 'center', color: '#6c7086', fontSize: 13 }}>
              No version history available.
            </div>
          )}
          {!loading &&
            versions.map((v, i) => (
              <div key={v.id} style={{ borderBottom: i < versions.length - 1 ? '1px solid #313244' : undefined }}>
                <div
                  style={{
                    padding: '10px 20px',
                    display: 'flex',
                    alignItems: 'center',
                    gap: 12,
                    cursor: 'pointer',
                  }}
                  onClick={() => setExpandedVersion(expandedVersion === v.version ? null : v.version)}
                >
                  <div
                    style={{
                      width: 32,
                      height: 32,
                      borderRadius: '50%',
                      background: i === 0 ? '#89b4fa' : '#313244',
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      color: i === 0 ? '#1e1e2e' : '#a6adc8',
                      fontSize: 12,
                      fontWeight: 700,
                      flexShrink: 0,
                    }}
                  >
                    v{v.version}
                  </div>
                  <div style={{ flex: 1 }}>
                    <div style={{ color: '#cdd6f4', fontSize: 13 }}>
                      Version {v.version}
                      {i === 0 && (
                        <span
                          style={{
                            marginLeft: 8,
                            padding: '1px 6px',
                            background: '#89b4fa22',
                            color: '#89b4fa',
                            borderRadius: 4,
                            fontSize: 10,
                            fontWeight: 600,
                          }}
                        >
                          Current
                        </span>
                      )}
                    </div>
                    <div style={{ color: '#6c7086', fontSize: 11, marginTop: 2 }}>
                      by {formatUser(v.created_by)} &middot; {formatDate(v.created_at)}
                    </div>
                  </div>
                  {onRestore && i > 0 && (
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        if (window.confirm(`Restore to version ${v.version}? This will create a new version with the old configuration.`)) {
                          onRestore(v.config_yaml, v.version);
                        }
                      }}
                      style={{
                        padding: '4px 10px',
                        background: '#313244',
                        border: '1px solid #45475a',
                        borderRadius: 4,
                        color: '#f9e2af',
                        fontSize: 11,
                        cursor: 'pointer',
                        fontWeight: 500,
                      }}
                    >
                      Restore
                    </button>
                  )}
                  <span style={{ color: '#585b70', fontSize: 14 }}>
                    {expandedVersion === v.version ? '▼' : '▶'}
                  </span>
                </div>
                {expandedVersion === v.version && (
                  <div
                    style={{
                      padding: '0 20px 12px',
                      marginLeft: 44,
                    }}
                  >
                    <pre
                      style={{
                        background: '#181825',
                        border: '1px solid #313244',
                        borderRadius: 6,
                        padding: 10,
                        color: '#a6adc8',
                        fontSize: 11,
                        fontFamily: 'monospace',
                        maxHeight: 200,
                        overflowY: 'auto',
                        whiteSpace: 'pre-wrap',
                        wordBreak: 'break-word',
                        margin: 0,
                      }}
                    >
                      {v.config_yaml || '(empty)'}
                    </pre>
                  </div>
                )}
              </div>
            ))}
        </div>
      </div>
    </div>
  );
}
