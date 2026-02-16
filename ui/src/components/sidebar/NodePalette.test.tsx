import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ReactFlowProvider } from '@xyflow/react';
import NodePalette from './NodePalette.tsx';
import { CATEGORIES, MODULE_TYPES } from '../../types/workflow.ts';

function renderPalette() {
  return render(
    <ReactFlowProvider>
      <NodePalette />
    </ReactFlowProvider>,
  );
}

describe('NodePalette', () => {
  it('renders the Modules heading', () => {
    renderPalette();
    expect(screen.getByText('Modules')).toBeInTheDocument();
  });

  it('renders all categories', () => {
    renderPalette();

    for (const cat of CATEGORIES) {
      const matches = screen.getAllByText(cat.label);
      expect(matches.length).toBeGreaterThanOrEqual(1);
    }
  });

  it('renders module items with correct labels after expanding categories', () => {
    renderPalette();

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
    renderPalette();

    fireEvent.click(screen.getByText('HTTP'));
    fireEvent.click(screen.getByText('Middleware'));
    fireEvent.click(screen.getByText('Messaging'));
    fireEvent.click(screen.getByText('Observability'));

    expect(screen.getByText('HTTP Server')).toBeInTheDocument();
    expect(screen.getByText('HTTP Router')).toBeInTheDocument();
    expect(screen.getByText('Rate Limiter')).toBeInTheDocument();
    expect(screen.getByText('CORS Middleware')).toBeInTheDocument();
    expect(screen.getByText('Message Broker')).toBeInTheDocument();
    expect(screen.getByText('Health Checker')).toBeInTheDocument();
    expect(screen.getByText('Metrics Collector')).toBeInTheDocument();
  });

  it('module items have draggable attribute', () => {
    renderPalette();

    fireEvent.click(screen.getByText('HTTP'));

    const serverItem = screen.getByText('HTTP Server');
    expect(serverItem.closest('[draggable="true"]')).toBeTruthy();
  });

  it('categories start collapsed and are expandable', () => {
    renderPalette();

    expect(screen.queryByText('HTTP Server')).not.toBeInTheDocument();

    fireEvent.click(screen.getByText('HTTP'));

    expect(screen.getByText('HTTP Server')).toBeInTheDocument();
  });

  it('categories are collapsible after expanding', () => {
    renderPalette();

    fireEvent.click(screen.getByText('HTTP'));
    expect(screen.getByText('HTTP Server')).toBeInTheDocument();

    fireEvent.click(screen.getByText('HTTP'));
    expect(screen.queryByText('HTTP Server')).not.toBeInTheDocument();
  });

  it('expanding one category does not affect others', () => {
    renderPalette();

    fireEvent.click(screen.getByText('HTTP'));

    expect(screen.getByText('HTTP Server')).toBeInTheDocument();
    expect(screen.queryByText('Message Broker')).not.toBeInTheDocument();
  });

  it('shows count of types per category', () => {
    renderPalette();

    const httpTypes = MODULE_TYPES.filter((t) => t.category === 'http');
    const countElements = screen.getAllByText(String(httpTypes.length));
    expect(countElements.length).toBeGreaterThan(0);
  });
});
