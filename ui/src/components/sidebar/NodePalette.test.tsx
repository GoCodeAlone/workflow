import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import NodePalette from './NodePalette.tsx';
import { CATEGORIES, MODULE_TYPES } from '../../types/workflow.ts';

describe('NodePalette', () => {
  it('renders the Modules heading', () => {
    render(<NodePalette />);
    expect(screen.getByText('Modules')).toBeInTheDocument();
  });

  it('renders all 8 categories', () => {
    render(<NodePalette />);

    for (const cat of CATEGORIES) {
      // Some category labels may also appear as module labels (e.g. "State Machine"),
      // so use getAllByText and verify at least one exists
      const matches = screen.getAllByText(cat.label);
      expect(matches.length).toBeGreaterThanOrEqual(1);
    }
  });

  it('renders module items with correct labels', () => {
    render(<NodePalette />);

    // Check a sample of module labels that are unique across categories
    expect(screen.getByText('Auth Middleware')).toBeInTheDocument();
    expect(screen.getByText('Message Broker')).toBeInTheDocument();
    expect(screen.getByText('Scheduler')).toBeInTheDocument();
    expect(screen.getByText('Event Logger')).toBeInTheDocument();
    expect(screen.getByText('Database')).toBeInTheDocument();
    expect(screen.getByText('Rate Limiter')).toBeInTheDocument();
    expect(screen.getByText('CORS Middleware')).toBeInTheDocument();
    // "State Machine" appears both as category and module label
    expect(screen.getAllByText('State Machine').length).toBeGreaterThanOrEqual(2);
  });

  it('renders all module type labels from MODULE_TYPES', () => {
    render(<NodePalette />);

    for (const mod of MODULE_TYPES) {
      // Use getAllByText since some labels may appear more than once
      const matches = screen.getAllByText(mod.label);
      expect(matches.length).toBeGreaterThanOrEqual(1);
    }
  });

  it('module items have draggable attribute', () => {
    render(<NodePalette />);

    const serverItem = screen.getByText('HTTP Server');
    // The draggable div is the parent of the text
    expect(serverItem.closest('[draggable="true"]')).toBeTruthy();
  });

  it('categories are collapsible', () => {
    render(<NodePalette />);

    // HTTP Server should be visible initially (expanded by default)
    expect(screen.getByText('HTTP Server')).toBeInTheDocument();

    // Click the HTTP category header to collapse
    fireEvent.click(screen.getByText('HTTP'));

    // HTTP Server should no longer be in the document
    expect(screen.queryByText('HTTP Server')).not.toBeInTheDocument();
  });

  it('categories are re-expandable after collapsing', () => {
    render(<NodePalette />);

    // Collapse
    fireEvent.click(screen.getByText('HTTP'));
    expect(screen.queryByText('HTTP Server')).not.toBeInTheDocument();

    // Expand again
    fireEvent.click(screen.getByText('HTTP'));
    expect(screen.getByText('HTTP Server')).toBeInTheDocument();
  });

  it('collapsing one category does not affect others', () => {
    render(<NodePalette />);

    // Collapse HTTP
    fireEvent.click(screen.getByText('HTTP'));

    // Messaging items should still be visible
    expect(screen.getByText('Message Broker')).toBeInTheDocument();
  });

  it('shows count of types per category', () => {
    render(<NodePalette />);

    // HTTP category has 6 types
    const httpTypes = MODULE_TYPES.filter((t) => t.category === 'http');
    const countElements = screen.getAllByText(String(httpTypes.length));
    expect(countElements.length).toBeGreaterThan(0);
  });
});
