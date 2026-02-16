import { useState, useMemo, useCallback } from 'react';
import usePluginStore, { type PluginInfo } from '../../store/pluginStore';
import useMarketplaceStore from '../../store/marketplaceStore';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Given a plugin and the full plugin list, return names of plugins that depend on it. */
function getDependents(pluginName: string, allPlugins: PluginInfo[]): string[] {
  return allPlugins
    .filter((p) => p.dependencies?.includes(pluginName))
    .map((p) => p.name);
}

/** Given a plugin, return its dependency names. */
function getDependencies(plugin: PluginInfo): string[] {
  return plugin.dependencies ?? [];
}

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

function DependencyBadge({ name, enabled }: { name: string; enabled: boolean }) {
  return (
    <span
      style={{
        display: 'inline-block',
        padding: '1px 6px',
        borderRadius: 3,
        fontSize: 10,
        background: enabled ? '#a6e3a122' : '#45475a',
        color: enabled ? '#a6e3a1' : '#a6adc8',
        border: `1px solid ${enabled ? '#a6e3a144' : '#585b70'}`,
      }}
    >
      {name}
    </span>
  );
}

function StatusBadge({ enabled }: { enabled: boolean }) {
  return (
    <span
      style={{
        display: 'inline-block',
        padding: '2px 8px',
        borderRadius: 4,
        fontSize: 10,
        fontWeight: 600,
        background: enabled ? '#a6e3a122' : '#45475a',
        color: enabled ? '#a6e3a1' : '#a6adc8',
      }}
    >
      {enabled ? 'Enabled' : 'Disabled'}
    </span>
  );
}

function PluginCard({
  plugin,
  allPlugins,
  onToggle,
  actionLoading,
  onClick,
}: {
  plugin: PluginInfo;
  allPlugins: PluginInfo[];
  onToggle: (plugin: PluginInfo) => void;
  actionLoading: boolean;
  onClick: (plugin: PluginInfo) => void;
}) {
  const deps = getDependencies(plugin);
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
            v{plugin.version}
          </div>
        </div>
        <StatusBadge enabled={plugin.enabled} />
      </div>

      <div style={{ fontSize: 12, color: '#a6adc8', lineHeight: 1.4, flex: 1 }}>
        {plugin.description && plugin.description.length > 120
          ? plugin.description.slice(0, 120) + '...'
          : plugin.description}
      </div>

      {deps.length > 0 && (
        <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap', alignItems: 'center' }}>
          <span style={{ fontSize: 10, color: '#6c7086' }}>deps:</span>
          {deps.map((d) => {
            const depPlugin = allPlugins.find((p) => p.name === d);
            return <DependencyBadge key={d} name={d} enabled={depPlugin?.enabled ?? false} />;
          })}
        </div>
      )}

      <div style={{ display: 'flex', justifyContent: 'flex-end', alignItems: 'center', marginTop: 4 }}>
        <button
          disabled={actionLoading}
          onClick={(e) => {
            e.stopPropagation();
            onToggle(plugin);
          }}
          style={{
            padding: '4px 14px',
            borderRadius: 4,
            border: 'none',
            fontSize: 12,
            fontWeight: 600,
            cursor: actionLoading ? 'wait' : 'pointer',
            opacity: actionLoading ? 0.6 : 1,
            background: plugin.enabled ? '#f38ba822' : '#89b4fa',
            color: plugin.enabled ? '#f38ba8' : '#1e1e2e',
          }}
        >
          {actionLoading ? '...' : plugin.enabled ? 'Disable' : 'Enable'}
        </button>
      </div>
    </div>
  );
}

function PluginDetailModal({
  plugin,
  allPlugins,
  onClose,
  onToggle,
  actionLoading,
}: {
  plugin: PluginInfo;
  allPlugins: PluginInfo[];
  onClose: () => void;
  onToggle: (plugin: PluginInfo) => void;
  actionLoading: boolean;
}) {
  const deps = getDependencies(plugin);
  const dependents = getDependents(plugin.name, allPlugins);
  const enabledDependents = dependents.filter((d) => {
    const p = allPlugins.find((pl) => pl.name === d);
    return p?.enabled;
  });

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
              v{plugin.version}
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
          <StatusBadge enabled={plugin.enabled} />
        </div>

        {/* Description */}
        <h3 style={{ color: '#cdd6f4', fontSize: 14, fontWeight: 600, marginBottom: 8 }}>Description</h3>
        <p style={{ color: '#a6adc8', fontSize: 13, lineHeight: 1.6, marginBottom: 20 }}>{plugin.description}</p>

        {/* Dependencies */}
        {deps.length > 0 && (
          <div style={{ marginBottom: 16 }}>
            <h3 style={{ color: '#cdd6f4', fontSize: 14, fontWeight: 600, marginBottom: 8 }}>Dependencies</h3>
            <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
              {deps.map((d) => {
                const depPlugin = allPlugins.find((p) => p.name === d);
                return <DependencyBadge key={d} name={d} enabled={depPlugin?.enabled ?? false} />;
              })}
            </div>
            {!plugin.enabled && deps.some((d) => !allPlugins.find((p) => p.name === d)?.enabled) && (
              <div style={{ fontSize: 11, color: '#f9e2af', marginTop: 6 }}>
                Enabling this plugin will also enable its disabled dependencies.
              </div>
            )}
          </div>
        )}

        {/* Dependents (relevant when disabling) */}
        {plugin.enabled && enabledDependents.length > 0 && (
          <div style={{ marginBottom: 16 }}>
            <h3 style={{ color: '#cdd6f4', fontSize: 14, fontWeight: 600, marginBottom: 8 }}>Enabled Dependents</h3>
            <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
              {enabledDependents.map((d) => (
                <DependencyBadge key={d} name={d} enabled={true} />
              ))}
            </div>
            <div style={{ fontSize: 11, color: '#f38ba8', marginTop: 6 }}>
              Disabling this plugin may also disable the plugins listed above.
            </div>
          </div>
        )}

        {/* UI Pages */}
        {(plugin.uiPages?.length ?? 0) > 0 && (
          <div style={{ marginBottom: 20 }}>
            <h3 style={{ color: '#cdd6f4', fontSize: 14, fontWeight: 600, marginBottom: 8 }}>UI Pages</h3>
            <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
              {plugin.uiPages.map((page) => (
                <span
                  key={page.id}
                  style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: 4,
                    padding: '2px 8px',
                    borderRadius: 4,
                    fontSize: 11,
                    background: '#45475a',
                    color: '#cdd6f4',
                  }}
                >
                  <span>{page.icon}</span> {page.label}
                </span>
              ))}
            </div>
          </div>
        )}

        <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
          <button
            disabled={actionLoading}
            onClick={() => onToggle(plugin)}
            style={{
              padding: '8px 24px',
              borderRadius: 6,
              border: 'none',
              fontSize: 13,
              fontWeight: 600,
              cursor: actionLoading ? 'wait' : 'pointer',
              opacity: actionLoading ? 0.6 : 1,
              background: plugin.enabled ? '#f38ba822' : '#89b4fa',
              color: plugin.enabled ? '#f38ba8' : '#1e1e2e',
            }}
          >
            {actionLoading ? '...' : plugin.enabled ? 'Disable' : 'Enable'}
          </button>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main Marketplace component
// ---------------------------------------------------------------------------

export default function Marketplace() {
  const {
    plugins,
    enabling,
    error: pluginError,
    enablePlugin,
    disablePlugin,
    clearError: clearPluginError,
  } = usePluginStore();

  const { searchQuery, setSearchQuery } = useMarketplaceStore();
  const [selectedPlugin, setSelectedPlugin] = useState<PluginInfo | null>(null);
  const [view, setView] = useState<'enabled' | 'available'>('enabled');

  const error = pluginError;
  const clearError = clearPluginError;

  const handleToggle = useCallback(
    (plugin: PluginInfo) => {
      if (plugin.enabled) {
        disablePlugin(plugin.name);
      } else {
        enablePlugin(plugin.name);
      }
    },
    [enablePlugin, disablePlugin],
  );

  // Split plugins into enabled vs disabled
  const { enabledPlugins, disabledPlugins } = useMemo(() => {
    const enabled = plugins.filter((p) => p.enabled);
    const disabled = plugins.filter((p) => !p.enabled);
    return { enabledPlugins: enabled, disabledPlugins: disabled };
  }, [plugins]);

  // Filter by search
  const displayedPlugins = useMemo(() => {
    const source = view === 'enabled' ? enabledPlugins : disabledPlugins;
    if (!searchQuery) return source;
    const q = searchQuery.toLowerCase();
    return source.filter(
      (p) =>
        p.name.toLowerCase().includes(q) ||
        p.description?.toLowerCase().includes(q),
    );
  }, [view, enabledPlugins, disabledPlugins, searchQuery]);

  // Keep selectedPlugin in sync with latest plugin data
  const currentSelectedPlugin = useMemo(() => {
    if (!selectedPlugin) return null;
    return plugins.find((p) => p.name === selectedPlugin.name) ?? selectedPlugin;
  }, [selectedPlugin, plugins]);

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
        Enable and disable plugins to customize your workflow engine.
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
        {(['enabled', 'available'] as const).map((tab) => (
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
            }}
          >
            {tab === 'enabled' ? `Enabled (${enabledPlugins.length})` : `Available (${disabledPlugins.length})`}
          </button>
        ))}
      </div>

      {/* Search bar */}
      <div style={{ display: 'flex', gap: 12, marginBottom: 20 }}>
        <input
          type="text"
          placeholder={view === 'enabled' ? 'Filter enabled plugins...' : 'Search available plugins...'}
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
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

      {/* Summary */}
      <div style={{ fontSize: 12, color: '#6c7086', marginBottom: 12 }}>
        Showing {displayedPlugins.length} plugin{displayedPlugins.length !== 1 ? 's' : ''}
        {searchQuery && <> matching &quot;{searchQuery}&quot;</>}
      </div>

      {/* Plugin grid */}
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
            allPlugins={plugins}
            onToggle={handleToggle}
            actionLoading={!!enabling[plugin.name]}
            onClick={setSelectedPlugin}
          />
        ))}
        {displayedPlugins.length === 0 && (
          <div style={{ color: '#6c7086', fontSize: 13, gridColumn: '1 / -1', padding: 40, textAlign: 'center' }}>
            {view === 'enabled'
              ? 'No plugins enabled yet.'
              : 'No available plugins to enable.'}
          </div>
        )}
      </div>

      {/* Detail modal */}
      {currentSelectedPlugin && (
        <PluginDetailModal
          plugin={currentSelectedPlugin}
          allPlugins={plugins}
          onClose={() => setSelectedPlugin(null)}
          onToggle={handleToggle}
          actionLoading={!!enabling[currentSelectedPlugin.name]}
        />
      )}
    </div>
  );
}
