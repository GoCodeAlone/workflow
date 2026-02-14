import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import NodePalette from './NodePalette.tsx';
import { CATEGORIES, MODULE_TYPES } from '../../types/workflow.ts';

describe('NodePalette', () => {
  it('renders the Modules heading', () => {
    render(<NodePalette />);
    expect(screen.getByText('Modules')).toBeInTheDocument();
  });

  it('renders all 10 categories', () => {
    render(<NodePalette />);

    for (const cat of CATEGORIES) {
      // Some category labels may also appear as module labels (e.g. "State Machine"),
      // so use getAllByText and verify at least one exists
      const matches = screen.getAllByText(cat.label);
      expect(matches.length).toBeGreaterThanOrEqual(1);
    }
  });

  it('renders module items with correct labels after expanding categories', () => {
    render(<NodePalette />);

    // Categories start collapsed — expand them first
    fireEvent.click(screen.getByText('Middleware'));
    fireEvent.click(screen.getByText('Messaging'));
    fireEvent.click(screen.getByText('Scheduling'));
    fireEvent.click(screen.getByText('Events'));

    expect(screen.getByText('Auth Middleware')).toBeInTheDocument();
    expect(screen.getByText('Message Broker')).toBeInTheDocument();
    expect(screen.getByText('Scheduler')).toBeInTheDocument();
    expect(screen.getByText('Event Logger')).toBeInTheDocument();
    expect(screen.getByText('Rate Limiter')).toBeInTheDocument();
    expect(screen.getByText('CORS Middleware')).toBeInTheDocument();
  });

  it('renders module types for a few categories after expanding', () => {
    render(<NodePalette />);

    // Expand HTTP and Middleware categories (unambiguous names)
    fireEvent.click(screen.getByText('HTTP'));
    fireEvent.click(screen.getByText('Middleware'));
    fireEvent.click(screen.getByText('Messaging'));
    fireEvent.click(screen.getByText('Observability'));

    // Check HTTP modules
    expect(screen.getByText('HTTP Server')).toBeInTheDocument();
    expect(screen.getByText('HTTP Router')).toBeInTheDocument();
    // Check Middleware modules
    expect(screen.getByText('Rate Limiter')).toBeInTheDocument();
    expect(screen.getByText('CORS Middleware')).toBeInTheDocument();
    // Check Messaging modules
    expect(screen.getByText('Message Broker')).toBeInTheDocument();
    // Check Observability modules
    expect(screen.getByText('Health Checker')).toBeInTheDocument();
    expect(screen.getByText('Metrics Collector')).toBeInTheDocument();
  });

  it('module items have draggable attribute', () => {
    render(<NodePalette />);

    // Expand HTTP category first
    fireEvent.click(screen.getByText('HTTP'));

    const serverItem = screen.getByText('HTTP Server');
    expect(serverItem.closest('[draggable="true"]')).toBeTruthy();
  });

  it('categories start collapsed and are expandable', () => {
    render(<NodePalette />);

    // HTTP Server should NOT be visible initially (collapsed by default)
    expect(screen.queryByText('HTTP Server')).not.toBeInTheDocument();

    // Click the HTTP category header to expand
    fireEvent.click(screen.getByText('HTTP'));

    // HTTP Server should now be visible
    expect(screen.getByText('HTTP Server')).toBeInTheDocument();
  });

  it('categories are collapsible after expanding', () => {
    render(<NodePalette />);

    // Expand
    fireEvent.click(screen.getByText('HTTP'));
    expect(screen.getByText('HTTP Server')).toBeInTheDocument();

    // Collapse
    fireEvent.click(screen.getByText('HTTP'));
    expect(screen.queryByText('HTTP Server')).not.toBeInTheDocument();
  });

  it('expanding one category does not affect others', () => {
    render(<NodePalette />);

    // Expand HTTP only
    fireEvent.click(screen.getByText('HTTP'));

    // HTTP items visible
    expect(screen.getByText('HTTP Server')).toBeInTheDocument();

    // Messaging items should still be hidden (collapsed)
    expect(screen.queryByText('Message Broker')).not.toBeInTheDocument();
  });

  it('shows count of types per category', () => {
    render(<NodePalette />);

    // HTTP category has 6 types
    const httpTypes = MODULE_TYPES.filter((t) => t.category === 'http');
    const countElements = screen.getAllByText(String(httpTypes.length));
    expect(countElements.length).toBeGreaterThan(0);
  });
});
