import { useCallback } from 'react';
import useObservabilityStore from '../../store/observabilityStore.ts';
import type { ActiveView } from '../../types/observability.ts';

const NAV_ITEMS: { view: ActiveView; label: string; icon: string }[] = [
  { view: 'editor', label: 'Editor', icon: 'M3 3h7v7H3V3zm11 0h7v7h-7V3zM3 14h7v7H3v-7zm11 0h7v7h-7v-7z' },
  { view: 'dashboard', label: 'Dashboard', icon: 'M3 13h8V3H3v10zm0 8h8v-6H3v6zm10 0h8V11h-8v10zm0-18v6h8V3h-8z' },
  { view: 'executions', label: 'Executions', icon: 'M8 5v14l11-7z' },
  { view: 'logs', label: 'Logs', icon: 'M14 2H6c-1.1 0-2 .9-2 2v16c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V8l-6-6zm4 18H6V4h7v5h5v11zm-3-7H9v-2h6v2zm0 4H9v-2h6v2z' },
  { view: 'events', label: 'Events', icon: 'M7 2v11h3v9l7-12h-4l4-8z' },
  { view: 'marketplace', label: 'Marketplace', icon: 'M4 6h16v2H4zm0 5h16v2H4zm0 5h16v2H4zM20 4H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V6c0-1.1-.9-2-2-2zm0 14H4V6h16v12zM10 9l5 3-5 3V9z' },
  { view: 'templates', label: 'Templates', icon: 'M4 4h6v6H4V4zm10 0h6v6h-6V4zM4 14h6v6H4v-6zm10 3.5c0-1.38 1.12-2.5 2.5-2.5s2.5 1.12 2.5 2.5-1.12 2.5-2.5 2.5-2.5-1.12-2.5-2.5zM3 3v8h8V3H3zm6 6H5V5h4v4zm4-6v8h8V3h-8zm6 6h-4V5h4v4zM3 13v8h8v-8H3zm6 6H5v-4h4v4z' },
  { view: 'environments', label: 'Environments', icon: 'M19.35 10.04A7.49 7.49 0 0012 4C9.11 4 6.6 5.64 5.35 8.04A5.994 5.994 0 000 14c0 3.31 2.69 6 6 6h13c2.76 0 5-2.24 5-5 0-2.64-2.05-4.78-4.65-4.96zM19 18H6c-2.21 0-4-1.79-4-4s1.79-4 4-4h.71C7.37 7.69 9.48 6 12 6a5.5 5.5 0 015.45 4.75l.28 1.51 1.53.13A2.994 2.994 0 0122 15c0 1.66-1.34 3-3 3zm-5.55-8h-2.9v3H8l4 4 4-4h-2.55V10z' },
  { view: 'settings', label: 'Settings', icon: 'M19.14 12.94a7.07 7.07 0 000-1.88l2.03-1.58a.49.49 0 00.12-.61l-1.92-3.32a.49.49 0 00-.59-.22l-2.39.96a7.04 7.04 0 00-1.62-.94l-.36-2.54a.48.48 0 00-.48-.41h-3.84a.48.48 0 00-.48.41l-.36 2.54c-.59.24-1.13.57-1.62.94l-2.39-.96a.49.49 0 00-.59.22L2.74 8.87a.48.48 0 00.12.61l2.03 1.58a7.07 7.07 0 000 1.88l-2.03 1.58a.49.49 0 00-.12.61l1.92 3.32c.12.22.37.29.59.22l2.39-.96c.49.37 1.03.7 1.62.94l.36 2.54c.05.24.26.41.48.41h3.84c.24 0 .44-.17.48-.41l.36-2.54c.59-.24 1.13-.57 1.62-.94l2.39.96c.22.08.47 0 .59-.22l1.92-3.32c.12-.22.07-.49-.12-.61l-2.03-1.58zM12 15.6A3.6 3.6 0 1115.6 12 3.6 3.6 0 0112 15.6z' },
];

export default function AppNav() {
  const activeView = useObservabilityStore((s) => s.activeView);
  const setActiveView = useObservabilityStore((s) => s.setActiveView);

  const handleClick = useCallback(
    (view: ActiveView) => {
      setActiveView(view);
    },
    [setActiveView],
  );

  return (
    <nav
      style={{
        width: 48,
        minWidth: 48,
        background: '#11111b',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        paddingTop: 8,
        gap: 4,
        borderRight: '1px solid #313244',
        zIndex: 10,
      }}
    >
      {NAV_ITEMS.map((item) => {
        const isActive = activeView === item.view;
        return (
          <button
            key={item.view}
            onClick={() => handleClick(item.view)}
            title={item.label}
            style={{
              width: 40,
              height: 40,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              background: 'none',
              border: 'none',
              borderLeft: isActive ? '3px solid #89b4fa' : '3px solid transparent',
              borderRadius: 0,
              cursor: 'pointer',
              padding: 0,
            }}
          >
            <svg
              width={20}
              height={20}
              viewBox="0 0 24 24"
              fill={isActive ? '#89b4fa' : '#6c7086'}
            >
              <path d={item.icon} />
            </svg>
          </button>
        );
      })}
    </nav>
  );
}
