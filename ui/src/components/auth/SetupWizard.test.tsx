import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';

// Ensure localStorage is available before authStore is imported
vi.hoisted(() => {
  if (typeof globalThis.localStorage === 'undefined' || typeof globalThis.localStorage.getItem !== 'function') {
    const store: Record<string, string> = {};
    globalThis.localStorage = {
      getItem: (key: string) => store[key] ?? null,
      setItem: (key: string, value: string) => { store[key] = value; },
      removeItem: (key: string) => { delete store[key]; },
      clear: () => { Object.keys(store).forEach((k) => delete store[k]); },
      get length() { return Object.keys(store).length; },
      key: (index: number) => Object.keys(store)[index] ?? null,
    };
  }
});

import SetupWizard from './SetupWizard.tsx';
import useAuthStore from '../../store/authStore.ts';

function resetStore() {
  localStorage.clear();
  useAuthStore.setState({
    user: null,
    token: null,
    refreshToken: null,
    isAuthenticated: false,
    isLoading: false,
    error: null,
    setupRequired: true,
    setupLoading: false,
  });
}

describe('SetupWizard', () => {
  beforeEach(() => {
    resetStore();
    vi.restoreAllMocks();
  });

  it('renders the setup form with all fields', () => {
    render(<SetupWizard />);

    expect(screen.getByText('Welcome to Workflow Engine')).toBeInTheDocument();
    expect(screen.getByText('Create your admin account to get started')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Your name')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('admin@example.com')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Min 6 characters')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Confirm password')).toBeInTheDocument();
    expect(screen.getByText('Create Admin Account')).toBeInTheDocument();
  });

  it('shows password mismatch error', async () => {
    render(<SetupWizard />);

    fireEvent.change(screen.getByPlaceholderText('admin@example.com'), { target: { value: 'admin@test.com' } });
    fireEvent.change(screen.getByPlaceholderText('Min 6 characters'), { target: { value: 'password123' } });
    fireEvent.change(screen.getByPlaceholderText('Confirm password'), { target: { value: 'different' } });

    fireEvent.click(screen.getByText('Create Admin Account'));

    await waitFor(() => {
      expect(screen.getByText('Passwords do not match')).toBeInTheDocument();
    });
  });

  it('shows short password error', async () => {
    render(<SetupWizard />);

    fireEvent.change(screen.getByPlaceholderText('admin@example.com'), { target: { value: 'admin@test.com' } });
    fireEvent.change(screen.getByPlaceholderText('Min 6 characters'), { target: { value: 'pw' } });
    fireEvent.change(screen.getByPlaceholderText('Confirm password'), { target: { value: 'pw' } });

    fireEvent.click(screen.getByText('Create Admin Account'));

    await waitFor(() => {
      expect(screen.getByText('Password must be at least 6 characters')).toBeInTheDocument();
    });
  });

  it('calls setupAdmin on valid submit', async () => {
    const setupAdminMock = vi.fn().mockResolvedValue(undefined);
    useAuthStore.setState({ setupAdmin: setupAdminMock } as never);

    const { container } = render(<SetupWizard />);

    fireEvent.change(screen.getByPlaceholderText('Your name'), { target: { value: 'Admin' } });
    fireEvent.change(screen.getByPlaceholderText('admin@example.com'), { target: { value: 'admin@test.com' } });
    fireEvent.change(screen.getByPlaceholderText('Min 6 characters'), { target: { value: 'password123' } });
    fireEvent.change(screen.getByPlaceholderText('Confirm password'), { target: { value: 'password123' } });

    const form = container.querySelector('form')!;
    fireEvent.submit(form);

    await waitFor(() => {
      expect(setupAdminMock).toHaveBeenCalledWith('admin@test.com', 'password123', 'Admin');
    });
  });

  it('displays auth store error', () => {
    useAuthStore.setState({ error: 'Setup failed' });

    render(<SetupWizard />);

    expect(screen.getByText('Setup failed')).toBeInTheDocument();
  });

  it('shows loading state on submit button', () => {
    useAuthStore.setState({ isLoading: true });

    render(<SetupWizard />);

    expect(screen.getByText('Creating account...')).toBeInTheDocument();
  });

  it('disables submit button when loading', () => {
    useAuthStore.setState({ isLoading: true });

    render(<SetupWizard />);

    const submitBtn = screen.getByText('Creating account...');
    expect(submitBtn).toBeDisabled();
  });
});
