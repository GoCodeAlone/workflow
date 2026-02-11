import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { act } from '@testing-library/react';
import WorkflowTabs from './WorkflowTabs.tsx';
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
    tabs: [{ id: 'default', name: 'Workflow 1', nodes: [], edges: [], undoStack: [], redoStack: [], dirty: false }],
    activeTabId: 'default',
    crossWorkflowLinks: [],
  });
}

describe('WorkflowTabs', () => {
  beforeEach(() => {
    resetStore();
  });

  it('renders with default tab', () => {
    render(<WorkflowTabs />);
    expect(screen.getByText('Workflow 1')).toBeInTheDocument();
  });

  it('renders the add tab button', () => {
    render(<WorkflowTabs />);
    expect(screen.getByText('+')).toBeInTheDocument();
  });

  it('adds a new tab when clicking +', () => {
    render(<WorkflowTabs />);

    fireEvent.click(screen.getByText('+'));

    const { tabs } = useWorkflowStore.getState();
    expect(tabs).toHaveLength(2);
  });

  it('shows close button only when multiple tabs exist', () => {
    const { container } = render(<WorkflowTabs />);

    // With only 1 tab, no close button
    const closeButtons = container.querySelectorAll('button');
    const closeTexts = Array.from(closeButtons).filter((b) => b.textContent === 'x');
    expect(closeTexts).toHaveLength(0);

    // Add a second tab
    fireEvent.click(screen.getByText('+'));

    // Re-render with new state (rerender picks up store changes via Zustand)
    const { container: container2 } = render(<WorkflowTabs />);
    const closeButtons2 = container2.querySelectorAll('button');
    const closeTexts2 = Array.from(closeButtons2).filter((b) => b.textContent === 'x');
    expect(closeTexts2.length).toBeGreaterThan(0);
  });

  it('closes a tab when clicking x', () => {
    // Start with two tabs
    act(() => {
      useWorkflowStore.getState().addTab();
    });

    const { container } = render(<WorkflowTabs />);

    const closeButtons = container.querySelectorAll('button');
    const closeBtn = Array.from(closeButtons).find((b) => b.textContent === 'x');
    expect(closeBtn).toBeDefined();

    fireEvent.click(closeBtn!);

    expect(useWorkflowStore.getState().tabs).toHaveLength(1);
  });

  it('switches tab when clicking on a tab', () => {
    // Add a second tab
    act(() => {
      useWorkflowStore.getState().addTab();
    });

    render(<WorkflowTabs />);

    // Click the first tab (Workflow 1)
    fireEvent.click(screen.getByText('Workflow 1'));

    expect(useWorkflowStore.getState().activeTabId).toBe('default');
  });

  it('displays tab names', () => {
    act(() => {
      useWorkflowStore.getState().addTab();
    });

    render(<WorkflowTabs />);

    expect(screen.getByText('Workflow 1')).toBeInTheDocument();
    // Second tab will have a generated name like "Workflow 2" or higher
    const tabs = useWorkflowStore.getState().tabs;
    expect(screen.getByText(tabs[1].name)).toBeInTheDocument();
  });

  it('shows dirty indicator on modified tab', () => {
    useWorkflowStore.setState({
      tabs: [{ id: 'default', name: 'Workflow 1', nodes: [], edges: [], undoStack: [], redoStack: [], dirty: true }],
      activeTabId: 'default',
    });

    render(<WorkflowTabs />);
    expect(screen.getByText('Workflow 1 *')).toBeInTheDocument();
  });

  it('renders scroll buttons', () => {
    render(<WorkflowTabs />);
    // Left and right scroll arrows (unicode triangles)
    const buttons = screen.getAllByRole('button');
    // Should have at least: left arrow, right arrow, add button
    expect(buttons.length).toBeGreaterThanOrEqual(3);
  });

  it('renders multiple tabs correctly', () => {
    act(() => {
      useWorkflowStore.getState().addTab();
      useWorkflowStore.getState().addTab();
    });

    render(<WorkflowTabs />);

    const { tabs } = useWorkflowStore.getState();
    expect(tabs).toHaveLength(3);
    // All tab names should be in the document
    for (const tab of tabs) {
      expect(screen.getByText(tab.name)).toBeInTheDocument();
    }
  });
});
