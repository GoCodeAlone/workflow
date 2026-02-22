import { useEffect } from 'react';
import useWorkflowStore from '../../store/workflowStore.ts';

export interface Toast {
  id: string;
  message: string;
  type: 'success' | 'error' | 'info' | 'warning';
}

const TOAST_COLORS: Record<Toast['type'], { bg: string; border: string; text: string }> = {
  success: { bg: '#1e3a2f', border: '#10b981', text: '#6ee7b7' },
  error: { bg: '#3b1f2b', border: '#ef4444', text: '#fca5a5' },
  info: { bg: '#1e2d40', border: '#3b82f6', text: '#93c5fd' },
  warning: { bg: '#3a2e1e', border: '#f59e0b', text: '#fcd34d' },
};

export default function ToastContainer() {
  const toasts = useWorkflowStore((s) => s.toasts);
  const removeToast = useWorkflowStore((s) => s.removeToast);

  return (
    <div
      style={{
        position: 'fixed',
        bottom: 16,
        right: 16,
        zIndex: 9999,
        display: 'flex',
        flexDirection: 'column',
        gap: 8,
        maxWidth: 400,
      }}
    >
      {toasts.map((toast) => (
        <ToastItem key={toast.id} toast={toast} onDismiss={() => removeToast(toast.id)} />
      ))}
    </div>
  );
}

function ToastItem({ toast, onDismiss }: { toast: Toast; onDismiss: () => void }) {
  const colors = TOAST_COLORS[toast.type];

  useEffect(() => {
    const timer = setTimeout(onDismiss, 4000);
    return () => clearTimeout(timer);
  }, [onDismiss]);

  return (
    <div
      style={{
        background: colors.bg,
        border: `1px solid ${colors.border}`,
        borderRadius: 6,
        padding: '10px 14px',
        color: colors.text,
        fontSize: 13,
        display: 'flex',
        alignItems: 'flex-start',
        gap: 8,
        boxShadow: '0 4px 12px rgba(0,0,0,0.4)',
        animation: 'toastSlideIn 0.2s ease-out',
      }}
    >
      <span style={{ flex: 1, lineHeight: 1.4, whiteSpace: 'pre-wrap' }}>{toast.message}</span>
      <button
        onClick={onDismiss}
        style={{
          background: 'none',
          border: 'none',
          color: colors.text,
          cursor: 'pointer',
          fontSize: 14,
          padding: '0 2px',
          opacity: 0.7,
          lineHeight: 1,
        }}
      >
        x
      </button>
    </div>
  );
}
