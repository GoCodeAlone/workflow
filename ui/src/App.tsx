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
import WorkflowPickerBar from './components/shared/WorkflowPickerBar.tsx';
import CollapsiblePanel from './components/layout/CollapsiblePanel.tsx';
import useWorkflowStore from './store/workflowStore.ts';
import useAuthStore from './store/authStore.ts';
import useObservabilityStore from './store/observabilityStore.ts';
import useModuleSchemaStore from './store/moduleSchemaStore.ts';
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
  useEffect(() => {
    setSelectedWorkflowId(activeWorkflowRecord?.id ?? null);
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

  const handleSelectProject = useCallback((project: ApiProject) => {
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

      {subview === 'projects' && selectedProject && (
        <WorkflowList
          projectId={selectedProject.id}
          projectName={selectedProject.name}
          onOpenWorkflow={handleOpenWorkflow}
        />
      )}

      {subview === 'projects' && !selectedProject && (
        <div
          style={{
            flex: 1,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: '#6c7086',
            fontSize: 14,
            background: '#1e1e2e',
          }}
        >
          Select a project from the sidebar to view its workflows.
        </div>
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
          <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
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

function AppLayout() {
  const activeView = useObservabilityStore((s) => s.activeView);

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
      <Toolbar />
      {activeView === 'editor' && <WorkflowTabs />}
      <div style={{ display: 'flex', flex: 1, overflow: 'hidden' }}>
        <AppNav />
        {activeView === 'editor' && <EditorView />}
        {activeView === 'dashboard' && <SystemDashboard />}
        {activeView === 'executions' && <ExecutionsView />}
        {activeView === 'logs' && (
          <ObservabilityView><LogViewer /></ObservabilityView>
        )}
        {activeView === 'events' && (
          <ObservabilityView><EventInspector /></ObservabilityView>
        )}
        {activeView === 'settings' && <IAMSettings />}
      </div>
      <ToastContainer />
    </div>
  );
}

function AuthenticatedApp() {
  const { isAuthenticated, loadUser, setTokenFromCallback, setupRequired, setupLoading, checkSetupStatus } = useAuthStore();

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
      loadUser();
    }
    // Check setup status on mount
    checkSetupStatus();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Still checking setup status
  if (setupRequired === null || setupLoading) {
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

  useEffect(() => {
    if (!schemasLoaded) fetchSchemas();
  }, [schemasLoaded, fetchSchemas]);

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
