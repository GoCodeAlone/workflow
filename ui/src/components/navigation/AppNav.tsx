import { useCallback, useMemo } from 'react';
import useObservabilityStore from '../../store/observabilityStore.ts';
import useWorkflowStore from '../../store/workflowStore.ts';
import usePluginStore, { type UIPageDef } from '../../store/pluginStore.ts';
import type { ActiveView } from '../../types/observability.ts';

// ---------------------------------------------------------------------------
// NavButton — renders a single emoji-icon navigation button
// ---------------------------------------------------------------------------

function NavButton({
  page,
  isActive,
  onClick,
}: {
  page: UIPageDef;
  isActive: boolean;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      title={page.label}
      aria-label={page.label}
      aria-current={isActive ? 'page' : undefined}
      style={{
        width: 40,
        height: 40,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'none',
        border: 'none',
        borderLeft: isActive ? '3px solid #89b4fa' : '3px solid transparent',
        borderRadius: 0,
        cursor: 'pointer',
        padding: 0,
        position: 'relative',
        flexShrink: 0,
        fontSize: 18,
        filter: isActive ? 'none' : 'grayscale(0.6) opacity(0.65)',
      }}
    >
      <span aria-hidden="true">{page.icon}</span>
    </button>
  );
}

// ---------------------------------------------------------------------------
// Divider — visual separator between nav groups
// ---------------------------------------------------------------------------

function NavDivider() {
  return (
    <div
      style={{
        width: 28,
        height: 1,
        background: '#45475a',
        margin: '6px 0 2px',
        flexShrink: 0,
      }}
      aria-hidden="true"
    />
  );
}

// ---------------------------------------------------------------------------
// AppNav — fully dynamic navigation derived from plugin state
// ---------------------------------------------------------------------------

export default function AppNav() {
  const activeView = useObservabilityStore((s) => s.activeView);
  const setActiveView = useObservabilityStore((s) => s.setActiveView);
  const activeWorkflowRecord = useWorkflowStore((s) => s.activeWorkflowRecord);
  const nodes = useWorkflowStore((s) => s.nodes);
  const selectedWorkflowId = useObservabilityStore((s) => s.selectedWorkflowId);

  const enabledPages = usePluginStore((s) => s.enabledPages);

  const hasWorkflowOpen = !!(activeWorkflowRecord || nodes.length > 0 || selectedWorkflowId);

  // Group and sort pages by category
  const { globalPages, pluginPages, workflowPages } = useMemo(() => {
    const global = enabledPages
      .filter((p) => p.category === 'global')
      .sort((a, b) => a.order - b.order);
    const plugin = enabledPages
      .filter((p) => p.category === 'plugin')
      .sort((a, b) => a.order - b.order);
    const workflow = enabledPages
      .filter((p) => p.category === 'workflow')
      .sort((a, b) => a.order - b.order);
    return { globalPages: global, pluginPages: plugin, workflowPages: workflow };
  }, [enabledPages]);

  const handleClick = useCallback(
    (view: ActiveView) => {
      setActiveView(view);
    },
    [setActiveView],
  );

  return (
    <nav
      role="navigation"
      aria-label="Main navigation"
      style={{
        width: 48,
        minWidth: 48,
        background: '#11111b',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        paddingTop: 8,
        gap: 4,
        borderRight: '1px solid #313244',
        position: 'relative',
        zIndex: 10,
        flexShrink: 0,
      }}
    >
      {/* Global navigation */}
      {globalPages.map((page) => (
        <NavButton
          key={page.id}
          page={page}
          isActive={activeView === page.id}
          onClick={() => handleClick(page.id)}
        />
      ))}

      {/* Plugin tools */}
      {pluginPages.length > 0 && (
        <>
          <NavDivider />
          {pluginPages.map((page) => (
            <NavButton
              key={page.id}
              page={page}
              isActive={activeView === page.id}
              onClick={() => handleClick(page.id)}
            />
          ))}
        </>
      )}

      {/* Workflow section -- only visible when a workflow is open */}
      {hasWorkflowOpen && workflowPages.length > 0 && (
        <>
          <NavDivider />
          <div
            style={{
              fontSize: 8,
              fontWeight: 700,
              color: '#585b70',
              textTransform: 'uppercase',
              letterSpacing: 1,
              marginBottom: 2,
              flexShrink: 0,
            }}
            aria-hidden="true"
          >
            WF
          </div>
          {workflowPages.map((page) => (
            <NavButton
              key={page.id}
              page={page}
              isActive={activeView === page.id}
              onClick={() => handleClick(page.id)}
            />
          ))}
        </>
      )}
    </nav>
  );
}
