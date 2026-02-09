import { ReactFlowProvider } from '@xyflow/react';
import NodePalette from './components/sidebar/NodePalette.tsx';
import WorkflowCanvas from './components/canvas/WorkflowCanvas.tsx';
import PropertyPanel from './components/properties/PropertyPanel.tsx';
import Toolbar from './components/toolbar/Toolbar.tsx';

export default function App() {
  return (
    <ReactFlowProvider>
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
        </div>
      </div>
    </ReactFlowProvider>
  );
}
