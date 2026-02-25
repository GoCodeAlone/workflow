import { Component, type ReactNode, useEffect, useState, useCallback } from 'react';
import { ReactFlowProvider } from '@xyflow/react';
import NodePalette from './components/sidebar/NodePalette.tsx';
import WorkflowCanvas from './components/canvas/WorkflowCanvas.tsx';
import PropertyPanel from './components/properties/PropertyPanel.tsx';
import Toolbar from './components/toolbar/Toolbar.tsx';
import WorkflowTabs from './components/tabs/WorkflowTabs.tsx';
import ToastContainer from './components/toast/ToastContainer.tsx';
import AICopilotPanel from './components/ai/AICopilotPanel.tsx';
import ComponentBrowser from './components/dynamic/ComponentBrowser.tsx';
import LoginPage from './components/auth/LoginPage.tsx';
import SetupWizard from './components/auth/SetupWizard.tsx';
import ProjectSwitcher from './components/projects/ProjectSwitcher.tsx';
import WorkflowList from './components/workflows/WorkflowList.tsx';
import AppNav from './components/navigation/AppNav.tsx';
import SystemDashboard from './components/dashboard/SystemDashboard.tsx';
import WorkflowDashboard from './components/dashboard/WorkflowDashboard.tsx';
import LogViewer from './components/logs/LogViewer.tsx';
import EventInspector from './components/events/EventInspector.tsx';
import IAMSettings from './components/iam/IAMSettings.tsx';
import Marketplace from './components/marketplace/Marketplace.tsx';
import Templates from './components/templates/Templates.tsx';
import Environments from './components/environments/Environments.tsx';
import FlagManager from './components/featureflags/FlagManager.tsx';
import StoreBrowserPage from './components/storebrowser/StoreBrowserPage.tsx';
import DocsPage from './components/docmanager/DocsPage.tsx';
import ScaffoldPage from './components/scaffold/ScaffoldPage.tsx';
import TemplatePage from './components/dynamic/TemplatePage.tsx';
import WorkflowPickerBar from './components/shared/WorkflowPickerBar.tsx';
import CollapsiblePanel from './components/layout/CollapsiblePanel.tsx';
import useWorkflowStore from './store/workflowStore.ts';
import useAuthStore from './store/authStore.ts';
import useObservabilityStore from './store/observabilityStore.ts';
import useModuleSchemaStore from './store/moduleSchemaStore.ts';
import usePluginStore from './store/pluginStore.ts';
import type { UIPageDef } from './store/pluginStore.ts';
import useUILayoutStore, { PANEL_WIDTH_LIMITS } from './store/uiLayoutStore.ts';
import { parseYaml } from './utils/serialization.ts';
import type { ApiProject, ApiWorkflowRecord } from './utils/api.ts';

function EditorView() {
  const showAIPanel = useWorkflowStore((s) => s.showAIPanel);
  const showComponentBrowser = useWorkflowStore((s) => s.showComponentBrowser);
  const importFromConfig = useWorkflowStore((s) => s.importFromConfig);
  const clearCanvas = useWorkflowStore((s) => s.clearCanvas);
  const setActiveWorkflowRecord = useWorkflowStore((s) => s.setActiveWorkflowRecord);
  const activeWorkflowRecord = useWorkflowStore((s) => s.activeWorkflowRecord);
  const renameTab = useWorkflowStore((s) => s.renameTab);
  const activeTabId = useWorkflowStore((s) => s.activeTabId);
  const setSelectedWorkflowId = useObservabilityStore((s) => s.setSelectedWorkflowId);

  const projectSwitcherCollapsed = useUILayoutStore((s) => s.projectSwitcherCollapsed);
  const nodePaletteCollapsed = useUILayoutStore((s) => s.nodePaletteCollapsed);
  const propertyPanelCollapsed = useUILayoutStore((s) => s.propertyPanelCollapsed);
  const toggleProjectSwitcher = useUILayoutStore((s) => s.toggleProjectSwitcher);
  const toggleNodePalette = useUILayoutStore((s) => s.toggleNodePalette);
  const togglePropertyPanel = useUILayoutStore((s) => s.togglePropertyPanel);
  const panelWidths = useUILayoutStore((s) => s.panelWidths);
  const setPanelWidth = useUILayoutStore((s) => s.setPanelWidth);

  const nodes = useWorkflowStore((s) => s.nodes);
  const [selectedProject, setSelectedProject] = useState<ApiProject | null>(null);

  // Sync activeWorkflowRecord to observability selectedWorkflowId
  // Only set (not clear) — clearing is handled by explicit close actions
  useEffect(() => {
    if (activeWorkflowRecord?.id) {
      setSelectedWorkflowId(activeWorkflowRecord.id);
    }
  }, [activeWorkflowRecord, setSelectedWorkflowId]);

  // Keyboard shortcuts: Ctrl+1/2/3 toggle panels
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey)) return;
      const target = e.target as HTMLElement;
      const isInput = target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT';
      if (isInput) return;

      if (e.key === '1') {
        e.preventDefault();
        toggleProjectSwitcher();
      } else if (e.key === '2') {
        e.preventDefault();
        toggleNodePalette();
      } else if (e.key === '3') {
        e.preventDefault();
        togglePropertyPanel();
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [toggleProjectSwitcher, toggleNodePalette, togglePropertyPanel]);

  // Show editor canvas when a v1 workflow is active OR when nodes are on the canvas
  // (e.g. loaded via "Load Server" or "Import" which don't set activeWorkflowRecord)
  const subview = (activeWorkflowRecord || nodes.length > 0) ? 'editor' : 'projects';

  const handleSelectProject = useCallback((project: ApiProject | null) => {
    setSelectedProject(project);
  }, []);

  const handleOpenWorkflow = useCallback((wf: ApiWorkflowRecord) => {
    // Always clear canvas first to prevent stale nodes from previous workflow
    clearCanvas();
    // Parse config_yaml and load into canvas
    if (wf.config_yaml) {
      try {
        const config = parseYaml(wf.config_yaml);
        importFromConfig(config);
      } catch {
        // Invalid config — canvas already cleared above
      }
    }
    setActiveWorkflowRecord(wf);
    renameTab(activeTabId, wf.name);
  }, [clearCanvas, importFromConfig, setActiveWorkflowRecord, renameTab, activeTabId]);

  return (
    <>
      <CollapsiblePanel
        collapsed={projectSwitcherCollapsed}
        onToggle={toggleProjectSwitcher}
        side="left"
        panelName="Projects"
        width={panelWidths.projectSwitcher}
        onResize={(w) => setPanelWidth('projectSwitcher', w)}
        minWidth={PANEL_WIDTH_LIMITS.projectSwitcher.min}
        maxWidth={PANEL_WIDTH_LIMITS.projectSwitcher.max}
      >
        <ProjectSwitcher
          selectedProjectId={selectedProject?.id ?? null}
          onSelectProject={handleSelectProject}
        />
      </CollapsiblePanel>

      {subview === 'projects' && (
        <WorkflowList
          projectId={selectedProject?.id}
          projectName={selectedProject?.name}
          onOpenWorkflow={handleOpenWorkflow}
        />
      )}

      {subview === 'editor' && (
        <>
          <CollapsiblePanel
            collapsed={nodePaletteCollapsed}
            onToggle={toggleNodePalette}
            side="left"
            panelName="Module Palette"
            width={panelWidths.nodePalette}
            onResize={(w) => setPanelWidth('nodePalette', w)}
            minWidth={PANEL_WIDTH_LIMITS.nodePalette.min}
            maxWidth={PANEL_WIDTH_LIMITS.nodePalette.max}
          >
            <NodePalette />
          </CollapsiblePanel>
          <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden', position: 'relative', zIndex: 0 }}>
            <WorkflowCanvas />
          </div>
          <CollapsiblePanel
            collapsed={propertyPanelCollapsed}
            onToggle={togglePropertyPanel}
            side="right"
            panelName="Properties"
            width={panelWidths.propertyPanel}
            onResize={(w) => setPanelWidth('propertyPanel', w)}
            minWidth={PANEL_WIDTH_LIMITS.propertyPanel.min}
            maxWidth={PANEL_WIDTH_LIMITS.propertyPanel.max}
          >
            <PropertyPanel />
          </CollapsiblePanel>
          {showAIPanel && <AICopilotPanel />}
          {showComponentBrowser && <ComponentBrowser />}
        </>
      )}
    </>
  );
}

function ObservabilityView({ children }: { children: ReactNode }) {
  const selectedWorkflowId = useObservabilityStore((s) => s.selectedWorkflowId);

  if (selectedWorkflowId) {
    return (
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', background: '#1e1e2e', overflow: 'hidden' }}>
        <WorkflowPickerBar />
        <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
          {children}
        </div>
      </div>
    );
  }

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', background: '#1e1e2e', overflow: 'hidden' }}>
      <WorkflowPickerBar />
      <div
        style={{
          flex: 1,
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          color: '#6c7086',
          gap: 8,
        }}
      >
        <div style={{ fontSize: 14 }}>No workflow selected.</div>
        <div style={{ fontSize: 12 }}>Select a workflow above, or open one in the Editor first.</div>
      </div>
    </div>
  );
}

function ExecutionsView() {
  const selectedWorkflowId = useObservabilityStore((s) => s.selectedWorkflowId);

  return (
    <ObservabilityView>
      {selectedWorkflowId && <WorkflowDashboard />}
    </ObservabilityView>
  );
}

function LogsView() {
  return (
    <ObservabilityView><LogViewer /></ObservabilityView>
  );
}

function EventsView() {
  return (
    <ObservabilityView><EventInspector /></ObservabilityView>
  );
}

function ValidationBar() {
  const validationErrors = useWorkflowStore((s) => s.validationErrors);
  const clearValidationErrors = useWorkflowStore((s) => s.clearValidationErrors);
  const setSelectedNode = useWorkflowStore((s) => s.setSelectedNode);

  if (validationErrors.length === 0) return null;

  return (
    <div style={{
      background: '#f38ba822',
      borderBottom: '1px solid #f38ba8',
      padding: '4px 16px',
      display: 'flex',
      alignItems: 'center',
      gap: 8,
      fontSize: 12,
      color: '#f38ba8',
      maxHeight: 80,
      overflowY: 'auto',
    }}>
      <span style={{ fontWeight: 600, flexShrink: 0 }}>
        {validationErrors.length} error{validationErrors.length !== 1 ? 's' : ''}:
      </span>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px 12px', flex: 1 }}>
        {validationErrors.map((err, i) => (
          <span
            key={i}
            onClick={() => err.nodeId && setSelectedNode(err.nodeId)}
            style={{
              cursor: err.nodeId ? 'pointer' : 'default',
              textDecoration: err.nodeId ? 'underline' : 'none',
            }}
          >
            {err.message}
          </span>
        ))}
      </div>
      <button
        onClick={clearValidationErrors}
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
        ×
      </button>
    </div>
  );
}

// Registry of all view components keyed by UIPage id.
// Navigation is driven entirely by pages returned from /api/v1/admin/plugins.
// Core views and plugin views are both dispatched through this registry;
// pages with no matching entry fall back to TemplatePage (if a template is set).
const VIEW_REGISTRY: Record<string, React.ComponentType> = {
  // Core views (declared by the admin-core plugin on the backend)
  dashboard: SystemDashboard,
  editor: EditorView,
  executions: ExecutionsView,
  logs: LogsView,
  events: EventsView,
  marketplace: Marketplace,
  templates: Templates,
  environments: Environments,
  settings: IAMSettings,
  // Plugin views (declared by their respective NativePlugins)
  'feature-flags': FlagManager,
  'store-browser': StoreBrowserPage,
  docs: DocsPage,
  scaffold: ScaffoldPage,
};

function AppLayout() {
  const activeView = useObservabilityStore((s) => s.activeView);
  const activeWorkflowRecord = useWorkflowStore((s) => s.activeWorkflowRecord);
  const nodes = useWorkflowStore((s) => s.nodes);
  const selectedWorkflowId = useObservabilityStore((s) => s.selectedWorkflowId);
  const enabledPages = usePluginStore((s) => s.enabledPages);

  // A workflow is "open" if it's loaded in the editor OR selected for observability
  const hasWorkflowOpen = !!(activeWorkflowRecord || nodes.length > 0 || selectedWorkflowId);

  // Note: Workflow-specific views (executions, logs, events) gracefully handle
  // the "no workflow selected" case via ObservabilityView's fallback UI,
  // so we do NOT force-redirect to dashboard when hasWorkflowOpen is false.
  // This avoids view bouncing caused by transient state changes during
  // workflow open/close transitions.

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100vh',
        width: '100vw',
        overflow: 'hidden',
        fontFamily: 'system-ui, -apple-system, sans-serif',
      }}
    >
      {activeView === 'editor' && <Toolbar />}
      {activeView === 'editor' && <ValidationBar />}
      {(activeView === 'editor' || hasWorkflowOpen) && <WorkflowTabs />}
      <div style={{ display: 'flex', flex: 1, overflow: 'hidden', position: 'relative', isolation: 'isolate' }}>
        <AppNav />
        {(() => {
          // Only render a view if the active page is declared by an enabled plugin.
          const page = enabledPages.find((p: UIPageDef) => p.id === activeView);
          if (!page) return null;

          // Use the component from the registry when available; fall back to TemplatePage.
          const ViewComponent = VIEW_REGISTRY[activeView];
          if (ViewComponent) return <ViewComponent />;
          if (page.template) return <TemplatePage page={page} />;
          return null;
        })()}
      </div>
      <ToastContainer />
    </div>
  );
}

function AuthenticatedApp() {
  const { isAuthenticated, loadUser, setTokenFromCallback, setupRequired, setupLoading, checkSetupStatus } = useAuthStore();

  // Determine synchronously whether token validation is needed:
  // Only the `isAuthenticated` path (no OAuth callback) needs async validation via loadUser.
  const needsAsyncValidation = (() => {
    const params = new URLSearchParams(window.location.search);
    return !params.get('token') && isAuthenticated;
  })();

  const [tokenValidated, setTokenValidated] = useState(!needsAsyncValidation);

  useEffect(() => {
    // Check for OAuth callback tokens in URL
    const params = new URLSearchParams(window.location.search);
    const token = params.get('token');
    const refreshToken = params.get('refresh_token');
    if (token && refreshToken) {
      setTokenFromCallback(token, refreshToken);
      // Clean URL
      window.history.replaceState({}, '', window.location.pathname);
    } else if (isAuthenticated) {
      // Validate existing token by loading user profile
      loadUser().then(() => setTokenValidated(true));
    }
    // Check setup status on mount
    checkSetupStatus();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Still checking setup status or validating token
  if (setupRequired === null || setupLoading || !tokenValidated) {
    return (
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          height: '100vh',
          width: '100vw',
          background: '#1e1e2e',
          color: '#a6adc8',
          fontFamily: 'system-ui, -apple-system, sans-serif',
          fontSize: 14,
        }}
      >
        Loading...
      </div>
    );
  }

  // First-time setup needed
  if (setupRequired) {
    return <SetupWizard />;
  }

  if (!isAuthenticated) {
    return <LoginPage />;
  }

  return <AuthenticatedContent />;
}

function AuthenticatedContent() {
  const fetchSchemas = useModuleSchemaStore((s) => s.fetchSchemas);
  const schemasLoaded = useModuleSchemaStore((s) => s.loaded);
  const fetchPlugins = usePluginStore((s) => s.fetchPlugins);
  const pluginsLoaded = usePluginStore((s) => s.loaded);
  const setUserAccess = usePluginStore((s) => s.setUserAccess);
  const user = useAuthStore((s) => s.user);

  useEffect(() => {
    if (!schemasLoaded) fetchSchemas();
  }, [schemasLoaded, fetchSchemas]);

  useEffect(() => {
    if (!pluginsLoaded) fetchPlugins();
  }, [pluginsLoaded, fetchPlugins]);

  // Sync user role/permissions to plugin store for permission-gated navigation
  useEffect(() => {
    if (user) {
      setUserAccess(user.role);
    }
  }, [user, setUserAccess]);

  return (
    <ReactFlowProvider>
      <AppLayout />
    </ReactFlowProvider>
  );
}

class ErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state: { error: Error | null } = { error: null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  render() {
    if (this.state.error) {
      return (
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            height: '100vh',
            background: '#1e1e2e',
            color: '#cdd6f4',
            fontFamily: 'system-ui, sans-serif',
            padding: 40,
          }}
        >
          <h1 style={{ color: '#f38ba8', marginBottom: 16 }}>Something went wrong</h1>
          <p style={{ color: '#a6adc8', marginBottom: 24, maxWidth: 600, textAlign: 'center' }}>
            {this.state.error.message}
          </p>
          <button
            onClick={() => {
              this.setState({ error: null });
              window.location.reload();
            }}
            style={{
              padding: '10px 24px',
              background: '#89b4fa',
              border: 'none',
              borderRadius: 6,
              color: '#1e1e2e',
              fontSize: 14,
              fontWeight: 600,
              cursor: 'pointer',
            }}
          >
            Reload
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}

export default function App() {
  return (
    <ErrorBoundary>
      <AuthenticatedApp />
    </ErrorBoundary>
  );
}
