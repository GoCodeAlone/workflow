import { useState, useEffect, useMemo, useCallback, useRef } from 'react';
import useMarketplaceStore, { type MarketplacePlugin } from '../../store/marketplaceStore';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const CATEGORY_COLORS: Record<string, string> = {
  Connectors: '#89b4fa',
  Transforms: '#a6e3a1',
  Middleware: '#fab387',
  Storage: '#f9e2af',
  AI: '#cba6f7',
  Monitoring: '#f38ba8',
};

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

function CategoryBadge({ category }: { category: string }) {
  const color = CATEGORY_COLORS[category] || '#89b4fa';
  return (
    <span
      style={{
        display: 'inline-block',
        padding: '2px 8px',
        borderRadius: 4,
        fontSize: 10,
        fontWeight: 600,
        background: color + '22',
        color,
      }}
    >
      {category}
    </span>
  );
}

function TagBadge({ tag }: { tag: string }) {
  return (
    <span
      style={{
        display: 'inline-block',
        padding: '1px 6px',
        borderRadius: 3,
        fontSize: 10,
        background: '#45475a',
        color: '#a6adc8',
      }}
    >
      {tag}
    </span>
  );
}

function PluginCard({
  plugin,
  onAction,
  actionLoading,
  onClick,
}: {
  plugin: MarketplacePlugin;
  onAction: (plugin: MarketplacePlugin) => void;
  actionLoading: boolean;
  onClick: (plugin: MarketplacePlugin) => void;
}) {
  const tags = plugin.tags || [];
  return (
    <div
      onClick={() => onClick(plugin)}
      style={{
        background: '#313244',
        border: '1px solid #45475a',
        borderRadius: 8,
        padding: 16,
        cursor: 'pointer',
        transition: 'border-color 0.15s',
        display: 'flex',
        flexDirection: 'column',
        gap: 8,
      }}
      onMouseEnter={(e) => (e.currentTarget.style.borderColor = '#89b4fa')}
      onMouseLeave={(e) => (e.currentTarget.style.borderColor = '#45475a')}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
        <div>
          <div style={{ color: '#cdd6f4', fontWeight: 600, fontSize: 14 }}>{plugin.name}</div>
          <div style={{ fontSize: 11, color: '#6c7086', marginTop: 2 }}>
            v{plugin.version} by {plugin.author}
          </div>
        </div>
        {tags.length > 0 && <CategoryBadge category={tags[0]} />}
      </div>

      <div style={{ fontSize: 12, color: '#a6adc8', lineHeight: 1.4, flex: 1 }}>
        {plugin.description && plugin.description.length > 120
          ? plugin.description.slice(0, 120) + '...'
          : plugin.description}
      </div>

      <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
        {tags.map((tag) => (
          <TagBadge key={tag} tag={tag} />
        ))}
      </div>

      <div style={{ display: 'flex', justifyContent: 'flex-end', alignItems: 'center', marginTop: 4 }}>
        <button
          disabled={actionLoading}
          onClick={(e) => {
            e.stopPropagation();
            onAction(plugin);
          }}
          style={{
            padding: '4px 14px',
            borderRadius: 4,
            border: 'none',
            fontSize: 12,
            fontWeight: 600,
            cursor: actionLoading ? 'wait' : 'pointer',
            opacity: actionLoading ? 0.6 : 1,
            background: plugin.installed ? '#f38ba822' : '#89b4fa',
            color: plugin.installed ? '#f38ba8' : '#1e1e2e',
          }}
        >
          {actionLoading ? '...' : plugin.installed ? 'Uninstall' : 'Install'}
        </button>
      </div>
    </div>
  );
}

function PluginDetailModal({
  plugin,
  onClose,
  onAction,
  actionLoading,
}: {
  plugin: MarketplacePlugin;
  onClose: () => void;
  onAction: (plugin: MarketplacePlugin) => void;
  actionLoading: boolean;
}) {
  const tags = plugin.tags || [];
  return (
    <div
      onClick={onClose}
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0,0,0,0.6)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1000,
      }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          background: '#1e1e2e',
          border: '1px solid #45475a',
          borderRadius: 12,
          width: '90%',
          maxWidth: 700,
          maxHeight: '85vh',
          overflow: 'auto',
          padding: 24,
        }}
      >
        {/* Header */}
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 16 }}>
          <div>
            <h2 style={{ color: '#cdd6f4', margin: 0, fontSize: 20, fontWeight: 600 }}>{plugin.name}</h2>
            <div style={{ fontSize: 12, color: '#6c7086', marginTop: 4 }}>
              v{plugin.version} by {plugin.author}
            </div>
          </div>
          <button
            onClick={onClose}
            style={{
              background: 'none',
              border: 'none',
              color: '#6c7086',
              fontSize: 20,
              cursor: 'pointer',
              padding: 4,
            }}
          >
            x
          </button>
        </div>

        <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 16 }}>
          <span
            style={{
              padding: '3px 10px',
              borderRadius: 4,
              fontSize: 11,
              fontWeight: 600,
              background: plugin.installed ? '#a6e3a122' : '#45475a',
              color: plugin.installed ? '#a6e3a1' : '#a6adc8',
            }}
          >
            {plugin.installed ? 'Installed' : 'Available'}
          </span>
        </div>

        <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap', marginBottom: 16 }}>
          {tags.map((tag) => (
            <TagBadge key={tag} tag={tag} />
          ))}
        </div>

        {/* Description */}
        <h3 style={{ color: '#cdd6f4', fontSize: 14, fontWeight: 600, marginBottom: 8 }}>Description</h3>
        <p style={{ color: '#a6adc8', fontSize: 13, lineHeight: 1.6, marginBottom: 20 }}>{plugin.description}</p>

        <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
          <button
            disabled={actionLoading}
            onClick={() => onAction(plugin)}
            style={{
              padding: '8px 24px',
              borderRadius: 6,
              border: 'none',
              fontSize: 13,
              fontWeight: 600,
              cursor: actionLoading ? 'wait' : 'pointer',
              opacity: actionLoading ? 0.6 : 1,
              background: plugin.installed ? '#f38ba822' : '#89b4fa',
              color: plugin.installed ? '#f38ba8' : '#1e1e2e',
            }}
          >
            {actionLoading ? '...' : plugin.installed ? 'Uninstall' : 'Install'}
          </button>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export default function Marketplace() {
  const {
    plugins: installed,
    searchResults,
    loading,
    searching,
    installing,
    error,
    fetchInstalled,
    search,
    install,
    uninstall,
    clearError,
  } = useMarketplaceStore();

  const [searchQuery, setSearchQuery] = useState('');
  const [selectedPlugin, setSelectedPlugin] = useState<MarketplacePlugin | null>(null);
  const [view, setView] = useState<'installed' | 'available'>('installed');
  const searchTimer = useRef<ReturnType<typeof setTimeout>>(undefined);

  // Load installed plugins on mount
  useEffect(() => {
    fetchInstalled();
  }, [fetchInstalled]);

  // Debounced search
  const handleSearchChange = useCallback(
    (value: string) => {
      setSearchQuery(value);
      if (searchTimer.current) clearTimeout(searchTimer.current);
      searchTimer.current = setTimeout(() => {
        search(value);
      }, 300);
    },
    [search],
  );

  // Trigger initial search when switching to available tab
  useEffect(() => {
    if (view === 'available' && searchResults.length === 0) {
      search(searchQuery);
    }
  }, [view, search, searchQuery, searchResults.length]);

  const handleAction = useCallback(
    (plugin: MarketplacePlugin) => {
      if (plugin.installed) {
        uninstall(plugin.name);
      } else {
        install(plugin.name, plugin.version);
      }
      // Update selected plugin state if modal is open
      setSelectedPlugin((prev) => {
        if (prev && prev.name === plugin.name) {
          return { ...prev, installed: !prev.installed };
        }
        return prev;
      });
    },
    [install, uninstall],
  );

  // Filter displayed plugins based on view
  const displayedPlugins = useMemo(() => {
    if (view === 'installed') {
      if (!searchQuery) return installed;
      const q = searchQuery.toLowerCase();
      return installed.filter(
        (p) =>
          p.name.toLowerCase().includes(q) ||
          p.description?.toLowerCase().includes(q) ||
          p.tags?.some((t) => t.toLowerCase().includes(q)),
      );
    }
    return searchResults;
  }, [view, installed, searchResults, searchQuery]);

  return (
    <div
      style={{
        flex: 1,
        background: '#1e1e2e',
        overflow: 'auto',
        padding: 24,
      }}
    >
      <h2 style={{ color: '#cdd6f4', margin: '0 0 4px', fontSize: 20, fontWeight: 600 }}>Plugin Marketplace</h2>
      <p style={{ color: '#6c7086', fontSize: 13, margin: '0 0 20px' }}>
        Browse and install modules to extend your workflow engine.
      </p>

      {/* Error banner */}
      {error && (
        <div
          style={{
            background: '#f38ba822',
            border: '1px solid #f38ba8',
            borderRadius: 6,
            padding: '8px 12px',
            marginBottom: 16,
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
          }}
        >
          <span style={{ color: '#f38ba8', fontSize: 13 }}>{error}</span>
          <button
            onClick={clearError}
            style={{ background: 'none', border: 'none', color: '#f38ba8', cursor: 'pointer', fontSize: 14 }}
          >
            x
          </button>
        </div>
      )}

      {/* Tab bar */}
      <div style={{ display: 'flex', gap: 0, marginBottom: 20, borderBottom: '1px solid #45475a' }}>
        {(['installed', 'available'] as const).map((tab) => (
          <button
            key={tab}
            onClick={() => setView(tab)}
            style={{
              padding: '8px 20px',
              background: 'none',
              border: 'none',
              borderBottom: view === tab ? '2px solid #89b4fa' : '2px solid transparent',
              color: view === tab ? '#89b4fa' : '#6c7086',
              fontSize: 13,
              fontWeight: 600,
              cursor: 'pointer',
              textTransform: 'capitalize',
            }}
          >
            {tab} {tab === 'installed' && `(${installed.length})`}
          </button>
        ))}
      </div>

      {/* Search bar */}
      <div style={{ display: 'flex', gap: 12, marginBottom: 20 }}>
        <input
          type="text"
          placeholder={view === 'installed' ? 'Filter installed plugins...' : 'Search available plugins...'}
          value={searchQuery}
          onChange={(e) => handleSearchChange(e.target.value)}
          style={{
            flex: 1,
            minWidth: 200,
            padding: '8px 12px',
            borderRadius: 6,
            border: '1px solid #45475a',
            background: '#313244',
            color: '#cdd6f4',
            fontSize: 13,
            outline: 'none',
          }}
        />
      </div>

      {/* Loading state */}
      {(loading || searching) && (
        <div style={{ color: '#6c7086', fontSize: 13, textAlign: 'center', padding: 40 }}>
          Loading...
        </div>
      )}

      {/* Summary */}
      {!loading && !searching && (
        <div style={{ fontSize: 12, color: '#6c7086', marginBottom: 12 }}>
          Showing {displayedPlugins.length} plugin{displayedPlugins.length !== 1 ? 's' : ''}
          {searchQuery && <> matching &quot;{searchQuery}&quot;</>}
        </div>
      )}

      {/* Plugin grid */}
      {!loading && !searching && (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))',
            gap: 12,
          }}
        >
          {displayedPlugins.map((plugin) => (
            <PluginCard
              key={plugin.name}
              plugin={plugin}
              onAction={handleAction}
              actionLoading={!!installing[plugin.name]}
              onClick={setSelectedPlugin}
            />
          ))}
          {displayedPlugins.length === 0 && (
            <div style={{ color: '#6c7086', fontSize: 13, gridColumn: '1 / -1', padding: 40, textAlign: 'center' }}>
              {view === 'installed'
                ? 'No plugins installed yet.'
                : 'No plugins found. Try a different search term.'}
            </div>
          )}
        </div>
      )}

      {/* Detail modal */}
      {selectedPlugin && (
        <PluginDetailModal
          plugin={selectedPlugin}
          onClose={() => setSelectedPlugin(null)}
          onAction={handleAction}
          actionLoading={!!installing[selectedPlugin.name]}
        />
      )}
    </div>
  );
}
