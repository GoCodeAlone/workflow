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

// Core pages that were previously in FALLBACK_PAGES — now declared by the
// admin-core plugin. Tests set them explicitly so they don't depend on
// backend plugin state.
const CORE_PAGES = [
  { id: 'dashboard',    label: 'Dashboard',    icon: '\u{1F4CA}', category: 'global',   order: 0 },
  { id: 'editor',       label: 'Editor',       icon: '\u{1F4DD}', category: 'global',   order: 1 },
  { id: 'marketplace',  label: 'Marketplace',  icon: '\u{1F6D2}', category: 'global',   order: 2 },
  { id: 'templates',    label: 'Templates',    icon: '\u{1F4C4}', category: 'global',   order: 3 },
  { id: 'environments', label: 'Environments', icon: '\u2601\uFE0F', category: 'global', order: 4 },
  { id: 'settings',     label: 'Settings',     icon: '\u2699\uFE0F', category: 'global', order: 6 },
  { id: 'executions',   label: 'Executions',   icon: '\u25B6\uFE0F', category: 'workflow', order: 0 },
  { id: 'logs',         label: 'Logs',         icon: '\u{1F4C3}', category: 'workflow', order: 1 },
  { id: 'events',       label: 'Events',       icon: '\u26A1',    category: 'workflow', order: 2 },
];

function resetStores() {
  useObservabilityStore.setState({
    activeView: 'editor',
  });
  // Populate enabledPages with core pages (mirroring what the admin-core plugin provides)
  usePluginStore.setState({
    plugins: [],
    loaded: true,
    loading: false,
    enabling: {},
    error: null,
    enabledPages: CORE_PAGES,
  });
}

describe('AppNav', () => {
  beforeEach(() => {
    resetStores();
  });

  it('renders global navigation items from enabledPages', () => {
    render(<AppNav />);

    // CORE_PAGES has 6 global pages visible
    // (workflow pages are hidden when no workflow is open)
    const buttons = screen.getAllByRole('button');
    expect(buttons.length).toBeGreaterThanOrEqual(6);
  });

  it('renders correct titles on buttons from plugin-derived pages', () => {
    render(<AppNav />);

    // Global pages from CORE_PAGES (provided by admin-core plugin)
    expect(screen.getByTitle('Dashboard')).toBeInTheDocument();
    expect(screen.getByTitle('Editor')).toBeInTheDocument();
    expect(screen.getByTitle('Marketplace')).toBeInTheDocument();
    expect(screen.getByTitle('Templates')).toBeInTheDocument();
    expect(screen.getByTitle('Environments')).toBeInTheDocument();
    expect(screen.getByTitle('Settings')).toBeInTheDocument();

    // Store Browser and Documentation come from separate NativePlugins,
    // so they only appear when those plugins are loaded with their pages.
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
