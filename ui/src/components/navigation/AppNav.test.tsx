import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { act } from '@testing-library/react';
import AppNav from './AppNav.tsx';
import useObservabilityStore from '../../store/observabilityStore.ts';

// Mock the API module used by observability store
vi.mock('../../utils/api.ts', () => ({
  apiFetchDashboard: vi.fn(),
  apiFetchWorkflowDashboard: vi.fn(),
  apiFetchExecutions: vi.fn(),
  apiFetchExecutionDetail: vi.fn(),
  apiFetchExecutionSteps: vi.fn(),
  apiFetchLogs: vi.fn(),
  apiFetchEvents: vi.fn(),
  apiTriggerExecution: vi.fn(),
  apiCancelExecution: vi.fn(),
  apiFetchIAMProviders: vi.fn(),
  apiCreateIAMProvider: vi.fn(),
  apiUpdateIAMProvider: vi.fn(),
  apiDeleteIAMProvider: vi.fn(),
  apiTestIAMProvider: vi.fn(),
  apiFetchIAMRoleMappings: vi.fn(),
  apiCreateIAMRoleMapping: vi.fn(),
  apiDeleteIAMRoleMapping: vi.fn(),
  createLogStream: vi.fn(),
  createEventStream: vi.fn(),
}));

function resetStore() {
  useObservabilityStore.setState({
    activeView: 'editor',
  });
}

describe('AppNav', () => {
  beforeEach(() => {
    resetStore();
  });

  it('renders all navigation items', () => {
    render(<AppNav />);

    const buttons = screen.getAllByRole('button');
    // 6 nav items: Editor, Dashboard, Executions, Logs, Events, Settings
    expect(buttons).toHaveLength(6);
  });

  it('renders correct titles on buttons', () => {
    render(<AppNav />);

    expect(screen.getByTitle('Editor')).toBeInTheDocument();
    expect(screen.getByTitle('Dashboard')).toBeInTheDocument();
    expect(screen.getByTitle('Executions')).toBeInTheDocument();
    expect(screen.getByTitle('Logs')).toBeInTheDocument();
    expect(screen.getByTitle('Events')).toBeInTheDocument();
    expect(screen.getByTitle('Settings')).toBeInTheDocument();
  });

  it('changes view when clicking a navigation item', () => {
    render(<AppNav />);

    fireEvent.click(screen.getByTitle('Dashboard'));

    expect(useObservabilityStore.getState().activeView).toBe('dashboard');
  });

  it('updates active view on multiple clicks', () => {
    render(<AppNav />);

    fireEvent.click(screen.getByTitle('Logs'));
    expect(useObservabilityStore.getState().activeView).toBe('logs');

    fireEvent.click(screen.getByTitle('Events'));
    expect(useObservabilityStore.getState().activeView).toBe('events');

    fireEvent.click(screen.getByTitle('Editor'));
    expect(useObservabilityStore.getState().activeView).toBe('editor');
  });

  it('renders as a nav element', () => {
    render(<AppNav />);
    expect(screen.getByRole('navigation')).toBeInTheDocument();
  });
});
