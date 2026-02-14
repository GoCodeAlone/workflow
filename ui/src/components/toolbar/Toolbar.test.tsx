import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { act } from '@testing-library/react';
import Toolbar from './Toolbar.tsx';
import useWorkflowStore from '../../store/workflowStore.ts';

// Mock the API module
vi.mock('../../utils/api.ts', () => ({
  saveWorkflowConfig: vi.fn(),
  getWorkflowConfig: vi.fn(),
  validateWorkflow: vi.fn(),
}));

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

describe('Toolbar', () => {
  beforeEach(() => {
    resetStore();
  });

  it('renders all expected buttons', () => {
    render(<Toolbar />);

    expect(screen.getByText('Import')).toBeInTheDocument();
    expect(screen.getByText('Load Server')).toBeInTheDocument();
    expect(screen.getByText('Export YAML')).toBeInTheDocument();
    expect(screen.getByText('Save')).toBeInTheDocument();
    expect(screen.getByText('Validate')).toBeInTheDocument();
    expect(screen.getByText('Clear')).toBeInTheDocument();
    expect(screen.getByText('Undo')).toBeInTheDocument();
    expect(screen.getByText('Redo')).toBeInTheDocument();
    expect(screen.getByText('AI Copilot')).toBeInTheDocument();
    expect(screen.getByText('Components')).toBeInTheDocument();
  });

  it('renders the Workflow Editor title', () => {
    render(<Toolbar />);
    expect(screen.getByText('Workflow Editor')).toBeInTheDocument();
  });

  it('shows module count', () => {
    render(<Toolbar />);
    expect(screen.getByText('0 modules')).toBeInTheDocument();
  });

  it('shows correct module count when nodes exist', () => {
    act(() => {
      useWorkflowStore.getState().addNode('http.server', { x: 0, y: 0 });
      useWorkflowStore.getState().addNode('http.router', { x: 100, y: 0 });
    });

    render(<Toolbar />);
    expect(screen.getByText('2 modules')).toBeInTheDocument();
  });

  it('Clear button calls clearCanvas on the store after confirmation', () => {
    // Add nodes first so Clear is enabled
    act(() => {
      useWorkflowStore.getState().addNode('http.server', { x: 0, y: 0 });
    });

    // Mock window.confirm to return true
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);

    render(<Toolbar />);

    const clearButton = screen.getByText('Clear');
    fireEvent.click(clearButton);

    expect(confirmSpy).toHaveBeenCalled();
    expect(useWorkflowStore.getState().nodes).toHaveLength(0);

    confirmSpy.mockRestore();
  });

  it('disables Export YAML, Save, Validate, Clear when no nodes', () => {
    render(<Toolbar />);

    expect(screen.getByText('Export YAML')).toBeDisabled();
    expect(screen.getByText('Save')).toBeDisabled();
    expect(screen.getByText('Validate')).toBeDisabled();
    expect(screen.getByText('Clear')).toBeDisabled();
  });

  it('enables Export YAML, Save, Validate, Clear when nodes exist', () => {
    act(() => {
      useWorkflowStore.getState().addNode('http.server', { x: 0, y: 0 });
    });

    render(<Toolbar />);

    expect(screen.getByText('Export YAML')).not.toBeDisabled();
    expect(screen.getByText('Save')).not.toBeDisabled();
    expect(screen.getByText('Validate')).not.toBeDisabled();
    expect(screen.getByText('Clear')).not.toBeDisabled();
  });

  it('disables Undo when undoStack is empty', () => {
    render(<Toolbar />);
    expect(screen.getByText('Undo')).toBeDisabled();
  });

  it('disables Redo when redoStack is empty', () => {
    render(<Toolbar />);
    expect(screen.getByText('Redo')).toBeDisabled();
  });

  it('enables Undo when undoStack has entries', () => {
    act(() => {
      useWorkflowStore.getState().addNode('http.server', { x: 0, y: 0 });
    });

    render(<Toolbar />);
    expect(screen.getByText('Undo')).not.toBeDisabled();
  });

  it('AI Copilot button toggles AI panel', () => {
    render(<Toolbar />);

    expect(useWorkflowStore.getState().showAIPanel).toBe(false);

    fireEvent.click(screen.getByText('AI Copilot'));

    expect(useWorkflowStore.getState().showAIPanel).toBe(true);
  });

  it('Components button toggles component browser', () => {
    render(<Toolbar />);

    expect(useWorkflowStore.getState().showComponentBrowser).toBe(false);

    fireEvent.click(screen.getByText('Components'));

    expect(useWorkflowStore.getState().showComponentBrowser).toBe(true);
  });
});
