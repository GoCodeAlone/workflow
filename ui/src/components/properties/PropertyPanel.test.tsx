import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { act } from '@testing-library/react';
import PropertyPanel from './PropertyPanel.tsx';
import useWorkflowStore from '../../store/workflowStore.ts';

function resetStore() {
  useWorkflowStore.setState({
    nodes: [],
    edges: [],
    selectedNodeId: null,
    nodeCounter: 0,
    undoStack: [],
    redoStack: [],
    toasts: [],
    showAIPanel: false,
    showComponentBrowser: false,
  });
}

describe('PropertyPanel', () => {
  beforeEach(() => {
    resetStore();
  });

  it('shows placeholder text when no node is selected', () => {
    render(<PropertyPanel />);
    expect(screen.getByText('Select a node to edit its properties')).toBeInTheDocument();
  });

  it('shows node name when a node is selected', () => {
    act(() => {
      useWorkflowStore.getState().addNode('http.server', { x: 0, y: 0 });
    });

    const nodeId = useWorkflowStore.getState().nodes[0].id;

    act(() => {
      useWorkflowStore.getState().setSelectedNode(nodeId);
    });

    render(<PropertyPanel />);

    // The name input should contain the label
    const nameInput = screen.getByDisplayValue('HTTP Server 1');
    expect(nameInput).toBeInTheDocument();
  });

  it('shows module type when a node is selected', () => {
    act(() => {
      useWorkflowStore.getState().addNode('http.server', { x: 0, y: 0 });
    });

    const nodeId = useWorkflowStore.getState().nodes[0].id;

    act(() => {
      useWorkflowStore.getState().setSelectedNode(nodeId);
    });

    render(<PropertyPanel />);

    expect(screen.getByText('http.server')).toBeInTheDocument();
  });

  it('shows config fields when a node is selected', () => {
    act(() => {
      useWorkflowStore.getState().addNode('http.server', { x: 0, y: 0 });
    });

    const nodeId = useWorkflowStore.getState().nodes[0].id;

    act(() => {
      useWorkflowStore.getState().setSelectedNode(nodeId);
    });

    render(<PropertyPanel />);

    // HTTP Server has Address, Read Timeout, Write Timeout fields
    expect(screen.getByText('Address')).toBeInTheDocument();
    expect(screen.getByText('Read Timeout')).toBeInTheDocument();
    expect(screen.getByText('Write Timeout')).toBeInTheDocument();
  });

  it('shows the Properties header when a node is selected', () => {
    act(() => {
      useWorkflowStore.getState().addNode('http.server', { x: 0, y: 0 });
      useWorkflowStore.getState().setSelectedNode(
        useWorkflowStore.getState().nodes[0].id
      );
    });

    render(<PropertyPanel />);
    expect(screen.getByText('Properties')).toBeInTheDocument();
  });

  it('editing a text field calls updateNodeConfig', () => {
    act(() => {
      useWorkflowStore.getState().addNode('http.server', { x: 0, y: 0 });
    });

    const nodeId = useWorkflowStore.getState().nodes[0].id;

    act(() => {
      useWorkflowStore.getState().setSelectedNode(nodeId);
    });

    render(<PropertyPanel />);

    const addressInput = screen.getByDisplayValue(':8080');
    fireEvent.change(addressInput, { target: { value: ':9090' } });

    const updatedNode = useWorkflowStore.getState().nodes.find((n) => n.id === nodeId);
    expect(updatedNode?.data.config.address).toBe(':9090');
  });

  it('editing the name field updates node label', () => {
    act(() => {
      useWorkflowStore.getState().addNode('http.server', { x: 0, y: 0 });
    });

    const nodeId = useWorkflowStore.getState().nodes[0].id;

    act(() => {
      useWorkflowStore.getState().setSelectedNode(nodeId);
    });

    render(<PropertyPanel />);

    const nameInput = screen.getByDisplayValue('HTTP Server 1');
    fireEvent.change(nameInput, { target: { value: 'My Custom Server' } });

    const updatedNode = useWorkflowStore.getState().nodes.find((n) => n.id === nodeId);
    expect(updatedNode?.data.label).toBe('My Custom Server');
  });

  it('close button clears selection', () => {
    act(() => {
      useWorkflowStore.getState().addNode('http.server', { x: 0, y: 0 });
      useWorkflowStore.getState().setSelectedNode(
        useWorkflowStore.getState().nodes[0].id
      );
    });

    render(<PropertyPanel />);

    // The close button has 'x' text
    const closeButton = screen.getByText('x');
    fireEvent.click(closeButton);

    expect(useWorkflowStore.getState().selectedNodeId).toBeNull();
  });

  it('Delete Node button removes the node', () => {
    act(() => {
      useWorkflowStore.getState().addNode('http.server', { x: 0, y: 0 });
      useWorkflowStore.getState().setSelectedNode(
        useWorkflowStore.getState().nodes[0].id
      );
    });

    render(<PropertyPanel />);

    fireEvent.click(screen.getByText('Delete Node'));

    expect(useWorkflowStore.getState().nodes).toHaveLength(0);
  });

  it('shows select fields for types with options', () => {
    act(() => {
      useWorkflowStore.getState().addNode('http.middleware.auth', { x: 0, y: 0 });
      useWorkflowStore.getState().setSelectedNode(
        useWorkflowStore.getState().nodes[0].id
      );
    });

    render(<PropertyPanel />);

    // Auth Middleware has Auth Type with select options
    expect(screen.getByText('Auth Type')).toBeInTheDocument();
    // Should have a select element with options
    const select = screen.getByDisplayValue('jwt');
    expect(select.tagName).toBe('SELECT');
  });

  it('shows number fields for numeric config', () => {
    act(() => {
      useWorkflowStore.getState().addNode('http.middleware.ratelimit', { x: 0, y: 0 });
      useWorkflowStore.getState().setSelectedNode(
        useWorkflowStore.getState().nodes[0].id
      );
    });

    render(<PropertyPanel />);

    expect(screen.getByText('Requests/sec')).toBeInTheDocument();
    const rpsInput = screen.getByDisplayValue('100');
    expect(rpsInput).toHaveAttribute('type', 'number');
  });
});
