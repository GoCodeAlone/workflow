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

import LoginPage from './LoginPage.tsx';
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
  });
}

describe('LoginPage', () => {
  beforeEach(() => {
    resetStore();
    vi.restoreAllMocks();
  });

  it('renders the sign-in form with email and password fields', () => {
    render(<LoginPage />);

    expect(screen.getByText('Workflow Engine')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('you@example.com')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Enter password')).toBeInTheDocument();
  });

  it('renders Sign In and Sign Up toggle buttons', () => {
    render(<LoginPage />);

    // There are two "Sign In" elements: the toggle button and the submit button
    expect(screen.getAllByText('Sign In').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('Sign Up').length).toBeGreaterThanOrEqual(1);
  });

  it('renders OAuth buttons', () => {
    render(<LoginPage />);

    expect(screen.getByText('Continue with Google')).toBeInTheDocument();
    expect(screen.getByText('Continue with Okta')).toBeInTheDocument();
    expect(screen.getByText('Continue with Auth0')).toBeInTheDocument();
  });

  it('shows confirm password and display name fields in sign-up mode', () => {
    render(<LoginPage />);

    const signUpButtons = screen.getAllByText('Sign Up');
    fireEvent.click(signUpButtons[0]);

    expect(screen.getByPlaceholderText('Confirm password')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Your name')).toBeInTheDocument();
  });

  it('toggles between sign-in and sign-up mode', () => {
    render(<LoginPage />);

    expect(screen.queryByPlaceholderText('Confirm password')).not.toBeInTheDocument();

    const signUpButtons = screen.getAllByText('Sign Up');
    fireEvent.click(signUpButtons[0]);

    expect(screen.getByPlaceholderText('Confirm password')).toBeInTheDocument();

    fireEvent.click(screen.getByText('Sign In'));

    expect(screen.queryByPlaceholderText('Confirm password')).not.toBeInTheDocument();
  });

  it('shows password mismatch error on sign-up', async () => {
    render(<LoginPage />);

    const signUpButtons = screen.getAllByText('Sign Up');
    fireEvent.click(signUpButtons[0]);

    fireEvent.change(screen.getByPlaceholderText('you@example.com'), { target: { value: 'test@test.com' } });
    fireEvent.change(screen.getByPlaceholderText('Enter password'), { target: { value: 'password123' } });
    fireEvent.change(screen.getByPlaceholderText('Confirm password'), { target: { value: 'different' } });

    fireEvent.click(screen.getByText('Create Account'));

    await waitFor(() => {
      expect(screen.getByText('Passwords do not match')).toBeInTheDocument();
    });
  });

  it('shows short password error on sign-up', async () => {
    render(<LoginPage />);

    const signUpButtons = screen.getAllByText('Sign Up');
    fireEvent.click(signUpButtons[0]);

    fireEvent.change(screen.getByPlaceholderText('you@example.com'), { target: { value: 'test@test.com' } });
    fireEvent.change(screen.getByPlaceholderText('Enter password'), { target: { value: 'pw' } });
    fireEvent.change(screen.getByPlaceholderText('Confirm password'), { target: { value: 'pw' } });

    fireEvent.click(screen.getByText('Create Account'));

    await waitFor(() => {
      expect(screen.getByText('Password must be at least 6 characters')).toBeInTheDocument();
    });
  });

  it('calls login on sign-in form submit', async () => {
    const loginMock = vi.fn().mockResolvedValue(undefined);
    useAuthStore.setState({ login: loginMock } as never);

    const { container } = render(<LoginPage />);

    fireEvent.change(screen.getByPlaceholderText('you@example.com'), { target: { value: 'user@test.com' } });
    fireEvent.change(screen.getByPlaceholderText('Enter password'), { target: { value: 'pass123' } });

    // Submit the form directly
    const form = container.querySelector('form')!;
    fireEvent.submit(form);

    await waitFor(() => {
      expect(loginMock).toHaveBeenCalledWith('user@test.com', 'pass123');
    });
  });

  it('displays auth store error', () => {
    useAuthStore.setState({ error: 'Authentication failed' });

    render(<LoginPage />);

    expect(screen.getByText('Authentication failed')).toBeInTheDocument();
  });

  it('shows loading state on submit button', () => {
    useAuthStore.setState({ isLoading: true });

    render(<LoginPage />);

    expect(screen.getByText('Please wait...')).toBeInTheDocument();
  });

  it('disables submit button when loading', () => {
    useAuthStore.setState({ isLoading: true });

    render(<LoginPage />);

    const submitBtn = screen.getByText('Please wait...');
    expect(submitBtn).toBeDisabled();
  });
});
