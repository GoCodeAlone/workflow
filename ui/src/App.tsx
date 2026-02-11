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
import ProjectSwitcher from './components/projects/ProjectSwitcher.tsx';
import WorkflowList from './components/workflows/WorkflowList.tsx';
import AppNav from './components/navigation/AppNav.tsx';
import SystemDashboard from './components/dashboard/SystemDashboard.tsx';
import WorkflowDashboard from './components/dashboard/WorkflowDashboard.tsx';
import LogViewer from './components/logs/LogViewer.tsx';
import EventInspector from './components/events/EventInspector.tsx';
import IAMSettings from './components/iam/IAMSettings.tsx';
import useWorkflowStore from './store/workflowStore.ts';
import useAuthStore from './store/authStore.ts';
import useObservabilityStore from './store/observabilityStore.ts';
import type { ApiProject, ApiWorkflowRecord } from './utils/api.ts';

type EditorSubview = 'projects' | 'editor';

function EditorView() {
  const showAIPanel = useWorkflowStore((s) => s.showAIPanel);
  const showComponentBrowser = useWorkflowStore((s) => s.showComponentBrowser);

  const [subview, setSubview] = useState<EditorSubview>('projects');
  const [selectedProject, setSelectedProject] = useState<ApiProject | null>(null);

  const handleSelectProject = useCallback((project: ApiProject) => {
    setSelectedProject(project);
    setSubview('projects');
  }, []);

  const handleOpenWorkflow = useCallback((_wf: ApiWorkflowRecord) => {
    setSubview('editor');
  }, []);

  const handleBackToProjects = useCallback(() => {
    setSubview('projects');
  }, []);

  return (
    <>
      <ProjectSwitcher
        selectedProjectId={selectedProject?.id ?? null}
        onSelectProject={handleSelectProject}
      />

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
          <NodePalette />
          <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
            <div style={{ padding: '4px 8px', background: '#181825', borderBottom: '1px solid #313244' }}>
              <button
                onClick={handleBackToProjects}
                style={{
                  background: 'none',
                  border: 'none',
                  color: '#89b4fa',
                  cursor: 'pointer',
                  fontSize: 12,
                  padding: '2px 6px',
                }}
              >
                &larr; Back to workflows
              </button>
            </div>
            <WorkflowCanvas />
          </div>
          <PropertyPanel />
          {showAIPanel && <AICopilotPanel />}
          {showComponentBrowser && <ComponentBrowser />}
        </>
      )}
    </>
  );
}

function ExecutionsView() {
  const selectedWorkflowId = useObservabilityStore((s) => s.selectedWorkflowId);

  if (selectedWorkflowId) {
    return <WorkflowDashboard />;
  }

  return (
    <div
      style={{
        flex: 1,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        background: '#1e1e2e',
        color: '#6c7086',
        gap: 8,
      }}
    >
      <div style={{ fontSize: 14 }}>No workflow selected.</div>
      <div style={{ fontSize: 12 }}>Go to Dashboard and click a workflow to view its executions.</div>
    </div>
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
        {activeView === 'logs' && <LogViewer />}
        {activeView === 'events' && <EventInspector />}
        {activeView === 'settings' && <IAMSettings />}
      </div>
      <ToastContainer />
    </div>
  );
}

function AuthenticatedApp() {
  const { isAuthenticated, loadUser, setTokenFromCallback } = useAuthStore();

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
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  if (!isAuthenticated) {
    return <LoginPage />;
  }

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
