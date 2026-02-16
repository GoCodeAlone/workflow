import useStoreBrowserStore from '../../store/storeBrowserStore.ts';
import TableBrowser from './TableBrowser.tsx';
import EventBrowser from './EventBrowser.tsx';
import DLQBrowser from './DLQBrowser.tsx';
import SQLConsole from './SQLConsole.tsx';

const TABS = [
  { key: 'tables' as const, label: 'Tables' },
  { key: 'events' as const, label: 'Events' },
  { key: 'dlq' as const, label: 'DLQ' },
  { key: 'sql' as const, label: 'SQL Console' },
];

export default function StoreBrowserPage() {
  const { activeTab, setActiveTab } = useStoreBrowserStore();

  return (
    <div
      style={{
        flex: 1,
        display: 'flex',
        flexDirection: 'column',
        background: '#1e1e2e',
        overflow: 'hidden',
      }}
    >
      {/* Tab bar */}
      <div
        style={{
          display: 'flex',
          borderBottom: '1px solid #313244',
          background: '#181825',
          flexShrink: 0,
        }}
      >
        {TABS.map((tab) => (
          <button
            key={tab.key}
            onClick={() => setActiveTab(tab.key)}
            style={{
              padding: '10px 20px',
              background: 'transparent',
              border: 'none',
              borderBottom: activeTab === tab.key ? '2px solid #89b4fa' : '2px solid transparent',
              color: activeTab === tab.key ? '#89b4fa' : '#a6adc8',
              fontSize: 13,
              fontWeight: activeTab === tab.key ? 600 : 400,
              cursor: 'pointer',
            }}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* Content */}
      {activeTab === 'tables' && <TableBrowser />}
      {activeTab === 'events' && <EventBrowser />}
      {activeTab === 'dlq' && <DLQBrowser />}
      {activeTab === 'sql' && <SQLConsole />}
    </div>
  );
}
