import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import AppNav from './AppNav.tsx';
import useObservabilityStore from '../../store/observabilityStore.ts';
import usePluginStore from '../../store/pluginStore.ts';

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

function resetStores() {
  useObservabilityStore.setState({
    activeView: 'editor',
  });
  // Reset plugin store to use the fallback pages
  usePluginStore.setState({
    plugins: [],
    loaded: true,
    loading: false,
    enabling: {},
    error: null,
    // enabledPages uses FALLBACK_PAGES by default — no need to override
  });
}

describe('AppNav', () => {
  beforeEach(() => {
    resetStores();
  });

  it('renders global navigation items from enabledPages', () => {
    render(<AppNav />);

    // Fallback pages have 6 global pages visible
    // (workflow pages are hidden when no workflow is open;
    //  plugin pages like store-browser come from the plugin system, not FALLBACK_PAGES)
    const buttons = screen.getAllByRole('button');
    expect(buttons.length).toBeGreaterThanOrEqual(6);
  });

  it('renders correct titles on buttons from plugin-derived pages', () => {
    render(<AppNav />);

    // Global pages from FALLBACK_PAGES
    expect(screen.getByTitle('Dashboard')).toBeInTheDocument();
    expect(screen.getByTitle('Editor')).toBeInTheDocument();
    expect(screen.getByTitle('Marketplace')).toBeInTheDocument();
    expect(screen.getByTitle('Templates')).toBeInTheDocument();
    expect(screen.getByTitle('Environments')).toBeInTheDocument();
    expect(screen.getByTitle('Settings')).toBeInTheDocument();

    // Store Browser and Documentation come from the plugin system (not FALLBACK_PAGES),
    // so they only appear when plugins are loaded with those pages registered.
    expect(screen.queryByTitle('Store Browser')).not.toBeInTheDocument();
    expect(screen.queryByTitle('Documentation')).not.toBeInTheDocument();
  });

  it('changes view when clicking a navigation item', () => {
    render(<AppNav />);

    fireEvent.click(screen.getByTitle('Dashboard'));
    expect(useObservabilityStore.getState().activeView).toBe('dashboard');
  });

  it('updates active view on multiple clicks', () => {
    render(<AppNav />);

    fireEvent.click(screen.getByTitle('Marketplace'));
    expect(useObservabilityStore.getState().activeView).toBe('marketplace');

    fireEvent.click(screen.getByTitle('Settings'));
    expect(useObservabilityStore.getState().activeView).toBe('settings');

    fireEvent.click(screen.getByTitle('Editor'));
    expect(useObservabilityStore.getState().activeView).toBe('editor');
  });

  it('renders as a nav element', () => {
    render(<AppNav />);
    expect(screen.getByRole('navigation')).toBeInTheDocument();
  });

  it('renders dynamically when enabledPages change', () => {
    // Override with a custom set of pages
    usePluginStore.setState({
      enabledPages: [
        { id: 'custom-view', label: 'Custom View', icon: '\u{1F680}', category: 'global', order: 0 },
      ],
    });

    render(<AppNav />);

    expect(screen.getByTitle('Custom View')).toBeInTheDocument();
    // Old fallback items should not be present
    expect(screen.queryByTitle('Dashboard')).not.toBeInTheDocument();
  });
});
