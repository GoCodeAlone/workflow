import { useState, useCallback } from 'react';

interface ImportResult {
  success: boolean;
  title?: string;
  version?: string;
  pathCount: number;
  error?: string;
}

interface OpenAPIImportProps {
  onClose: () => void;
  onImport?: (spec: unknown) => void;
}

export default function OpenAPIImport({ onClose, onImport }: OpenAPIImportProps) {
  const [mode, setMode] = useState<'url' | 'file'>('url');
  const [url, setUrl] = useState('');
  const [fileContent, setFileContent] = useState<string | null>(null);
  const [fileName, setFileName] = useState('');
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState<ImportResult | null>(null);

  const handleFileChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    setFileName(file.name);
    const reader = new FileReader();
    reader.onload = (ev) => {
      setFileContent(ev.target?.result as string);
    };
    reader.readAsText(file);
  }, []);

  const handleImport = useCallback(async () => {
    setLoading(true);
    setResult(null);

    try {
      let spec: Record<string, unknown>;

      if (mode === 'url') {
        if (!url.trim()) {
          setResult({ success: false, pathCount: 0, error: 'Please enter a URL' });
          return;
        }
        const res = await fetch(url.trim());
        if (!res.ok) {
          throw new Error(`HTTP ${res.status}: ${res.statusText}`);
        }
        spec = await res.json();
      } else {
        if (!fileContent) {
          setResult({ success: false, pathCount: 0, error: 'Please select a file' });
          return;
        }
        spec = JSON.parse(fileContent);
      }

      // Validate it looks like an OpenAPI spec
      if (!spec.openapi && !spec.swagger) {
        throw new Error('Not a valid OpenAPI/Swagger spec (missing openapi or swagger field)');
      }

      const info = spec.info as Record<string, string> | undefined;
      const paths = spec.paths as Record<string, unknown> | undefined;
      const pathCount = paths ? Object.keys(paths).length : 0;

      setResult({
        success: true,
        title: info?.title || 'Unknown',
        version: info?.version || 'Unknown',
        pathCount,
      });

      if (onImport) {
        onImport(spec);
      }
    } catch (err) {
      setResult({
        success: false,
        pathCount: 0,
        error: err instanceof Error ? err.message : 'Failed to import spec',
      });
    } finally {
      setLoading(false);
    }
  }, [mode, url, fileContent, onImport]);

  return (
    <div
      style={{
        position: 'fixed',
        top: 0,
        left: 0,
        right: 0,
        bottom: 0,
        background: 'rgba(0,0,0,0.6)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1000,
      }}
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        style={{
          background: '#1e293b',
          border: '1px solid #334155',
          borderRadius: 8,
          padding: 24,
          width: 480,
          maxWidth: '90vw',
          color: '#e2e8f0',
        }}
      >
        <h3 style={{ margin: '0 0 16px', fontSize: 16, fontWeight: 600 }}>
          Import OpenAPI Specification
        </h3>

        {/* Mode tabs */}
        <div style={{ display: 'flex', gap: 0, marginBottom: 16 }}>
          <button
            onClick={() => setMode('url')}
            style={{
              padding: '6px 16px',
              background: mode === 'url' ? '#334155' : 'transparent',
              color: mode === 'url' ? '#e2e8f0' : '#94a3b8',
              border: '1px solid #475569',
              borderRadius: '4px 0 0 4px',
              cursor: 'pointer',
              fontSize: 13,
            }}
          >
            From URL
          </button>
          <button
            onClick={() => setMode('file')}
            style={{
              padding: '6px 16px',
              background: mode === 'file' ? '#334155' : 'transparent',
              color: mode === 'file' ? '#e2e8f0' : '#94a3b8',
              border: '1px solid #475569',
              borderLeft: 'none',
              borderRadius: '0 4px 4px 0',
              cursor: 'pointer',
              fontSize: 13,
            }}
          >
            From File
          </button>
        </div>

        {/* Input */}
        {mode === 'url' ? (
          <input
            type="text"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://api.example.com/openapi.json"
            style={{
              width: '100%',
              padding: '8px 10px',
              background: '#0f172a',
              color: '#e2e8f0',
              border: '1px solid #475569',
              borderRadius: 4,
              fontSize: 13,
              boxSizing: 'border-box',
            }}
          />
        ) : (
          <div>
            <input
              type="file"
              accept=".json,.yaml,.yml"
              onChange={handleFileChange}
              style={{ fontSize: 13 }}
            />
            {fileName && (
              <div style={{ fontSize: 12, color: '#94a3b8', marginTop: 4 }}>
                Selected: {fileName}
              </div>
            )}
          </div>
        )}

        {/* Result */}
        {result && (
          <div
            style={{
              marginTop: 12,
              padding: '8px 12px',
              borderRadius: 4,
              background: result.success ? '#052e16' : '#450a0a',
              border: `1px solid ${result.success ? '#166534' : '#991b1b'}`,
              fontSize: 13,
            }}
          >
            {result.success ? (
              <div>
                Imported <strong>{result.title}</strong> v{result.version} with{' '}
                {result.pathCount} path(s)
              </div>
            ) : (
              <div style={{ color: '#fca5a5' }}>{result.error}</div>
            )}
          </div>
        )}

        {/* Buttons */}
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 16 }}>
          <button
            onClick={onClose}
            style={{
              padding: '6px 16px',
              background: 'transparent',
              color: '#94a3b8',
              border: '1px solid #475569',
              borderRadius: 4,
              cursor: 'pointer',
              fontSize: 13,
            }}
          >
            Close
          </button>
          <button
            onClick={handleImport}
            disabled={loading}
            style={{
              padding: '6px 16px',
              background: '#2563eb',
              color: '#fff',
              border: 'none',
              borderRadius: 4,
              cursor: loading ? 'not-allowed' : 'pointer',
              opacity: loading ? 0.6 : 1,
              fontSize: 13,
            }}
          >
            {loading ? 'Importing...' : 'Import'}
          </button>
        </div>
      </div>
    </div>
  );
}
