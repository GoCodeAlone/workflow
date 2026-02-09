import { Component, type ReactNode } from 'react';
import { ReactFlowProvider } from '@xyflow/react';
import NodePalette from './components/sidebar/NodePalette.tsx';
import WorkflowCanvas from './components/canvas/WorkflowCanvas.tsx';
import PropertyPanel from './components/properties/PropertyPanel.tsx';
import Toolbar from './components/toolbar/Toolbar.tsx';
import ToastContainer from './components/toast/ToastContainer.tsx';
import AICopilotPanel from './components/ai/AICopilotPanel.tsx';
import ComponentBrowser from './components/dynamic/ComponentBrowser.tsx';
import useWorkflowStore from './store/workflowStore.ts';

function AppLayout() {
  const showAIPanel = useWorkflowStore((s) => s.showAIPanel);
  const showComponentBrowser = useWorkflowStore((s) => s.showComponentBrowser);

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
      <div style={{ display: 'flex', flex: 1, overflow: 'hidden' }}>
        <NodePalette />
        <WorkflowCanvas />
        <PropertyPanel />
        {showAIPanel && <AICopilotPanel />}
        {showComponentBrowser && <ComponentBrowser />}
      </div>
      <ToastContainer />
    </div>
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
      <ReactFlowProvider>
        <AppLayout />
      </ReactFlowProvider>
    </ErrorBoundary>
  );
}
