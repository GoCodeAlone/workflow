import { useEffect } from 'react';
import useDocManagerStore, { type Doc } from '../../store/docManagerStore.ts';

const inputStyle: React.CSSProperties = {
  width: '100%',
  background: '#313244',
  border: '1px solid #45475a',
  borderRadius: 6,
  color: '#cdd6f4',
  padding: '8px 10px',
  fontSize: 13,
  outline: 'none',
  boxSizing: 'border-box',
};

const selectStyle: React.CSSProperties = {
  ...inputStyle,
  appearance: 'auto' as React.CSSProperties['appearance'],
};

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleDateString(undefined, {
      month: 'short',
      day: 'numeric',
      year: 'numeric',
    });
  } catch {
    return iso;
  }
}

export default function DocList({ onNew }: { onNew: () => void }) {
  const docs = useDocManagerStore((s) => s.docs);
  const selectedDoc = useDocManagerStore((s) => s.selectedDoc);
  const loading = useDocManagerStore((s) => s.loading);
  const filters = useDocManagerStore((s) => s.filters);
  const categories = useDocManagerStore((s) => s.categories);
  const setFilter = useDocManagerStore((s) => s.setFilter);
  const fetchDocs = useDocManagerStore((s) => s.fetchDocs);
  const fetchCategories = useDocManagerStore((s) => s.fetchCategories);
  const fetchDoc = useDocManagerStore((s) => s.fetchDoc);

  useEffect(() => {
    fetchDocs();
    fetchCategories();
  }, [fetchDocs, fetchCategories]);

  useEffect(() => {
    fetchDocs();
  }, [filters, fetchDocs]);

  const handleSearch = (value: string) => {
    setFilter('search', value);
  };

  const handleCategoryFilter = (value: string) => {
    setFilter('category', value);
  };

  const handleSelect = (doc: Doc) => {
    fetchDoc(doc.id);
  };

  return (
    <div
      style={{
        width: 250,
        minWidth: 250,
        background: '#181825',
        borderRight: '1px solid #313244',
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        overflow: 'hidden',
      }}
    >
      {/* Header */}
      <div style={{ padding: '12px 12px 8px' }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 10 }}>
          <span style={{ color: '#cdd6f4', fontSize: 14, fontWeight: 600 }}>Documents</span>
          <button
            onClick={onNew}
            style={{
              background: '#89b4fa',
              border: 'none',
              borderRadius: 6,
              color: '#1e1e2e',
              padding: '5px 12px',
              fontSize: 12,
              fontWeight: 600,
              cursor: 'pointer',
            }}
          >
            + New
          </button>
        </div>

        {/* Search */}
        <input
          type="text"
          placeholder="Search docs..."
          value={filters.search ?? ''}
          onChange={(e) => handleSearch(e.target.value)}
          style={{ ...inputStyle, marginBottom: 6 }}
        />

        {/* Category filter */}
        <select
          value={filters.category ?? ''}
          onChange={(e) => handleCategoryFilter(e.target.value)}
          style={selectStyle}
        >
          <option value="">All categories</option>
          {categories.map((cat) => (
            <option key={cat} value={cat}>
              {cat}
            </option>
          ))}
        </select>
      </div>

      {/* Doc list */}
      <div style={{ flex: 1, overflowY: 'auto', padding: '4px 0' }}>
        {loading ? (
          <div style={{ color: '#6c7086', fontSize: 12, padding: 16, textAlign: 'center' }}>
            Loading...
          </div>
        ) : docs.length === 0 ? (
          <div style={{ color: '#6c7086', fontSize: 12, padding: 16, textAlign: 'center' }}>
            No documents found.
          </div>
        ) : (
          docs.map((doc) => {
            const isActive = selectedDoc?.id === doc.id;
            return (
              <div
                key={doc.id}
                onClick={() => handleSelect(doc)}
                style={{
                  padding: '10px 12px',
                  cursor: 'pointer',
                  background: isActive ? '#313244' : 'transparent',
                  borderLeft: isActive ? '3px solid #89b4fa' : '3px solid transparent',
                  transition: 'background 0.15s',
                }}
                onMouseEnter={(e) => {
                  if (!isActive) (e.currentTarget as HTMLDivElement).style.background = '#1e1e2e';
                }}
                onMouseLeave={(e) => {
                  if (!isActive) (e.currentTarget as HTMLDivElement).style.background = 'transparent';
                }}
              >
                <div
                  style={{
                    color: isActive ? '#cdd6f4' : '#a6adc8',
                    fontSize: 13,
                    fontWeight: isActive ? 600 : 400,
                    marginBottom: 4,
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {doc.title}
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                  {doc.category && (
                    <span
                      style={{
                        background: '#45475a',
                        color: '#89b4fa',
                        fontSize: 10,
                        padding: '1px 6px',
                        borderRadius: 3,
                        fontWeight: 500,
                      }}
                    >
                      {doc.category}
                    </span>
                  )}
                  <span style={{ color: '#6c7086', fontSize: 10 }}>
                    {formatDate(doc.updated_at)}
                  </span>
                </div>
              </div>
            );
          })
        )}
      </div>
    </div>
  );
}
