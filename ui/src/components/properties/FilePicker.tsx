import { useState, useEffect, useCallback } from 'react';

interface FileInfo {
  name: string;
  path: string;
  size: number;
  modTime: string;
  isDir: boolean;
}

interface FilePickerProps {
  value: string;
  onChange: (path: string) => void;
  projectId?: string;
  placeholder?: string;
  description?: string;
}

const inputStyle: React.CSSProperties = {
  width: '100%',
  padding: '6px 8px',
  background: '#1e1e2e',
  border: '1px solid #313244',
  borderRadius: 4,
  color: '#cdd6f4',
  fontSize: 12,
  outline: 'none',
  boxSizing: 'border-box',
};

export default function FilePicker({ value, onChange, projectId, placeholder, description }: FilePickerProps) {
  const [open, setOpen] = useState(false);
  const [files, setFiles] = useState<FileInfo[]>([]);
  const [currentPrefix, setCurrentPrefix] = useState('');
  const [loading, setLoading] = useState(false);
  const [uploading, setUploading] = useState(false);

  const pid = projectId || 'default';

  const fetchFiles = useCallback(async (prefix: string) => {
    setLoading(true);
    try {
      const token = localStorage.getItem('auth_token');
      const headers: Record<string, string> = {};
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const params = prefix ? `?prefix=${encodeURIComponent(prefix)}` : '';
      const res = await fetch(`/api/v1/workspaces/${pid}/files${params}`, { headers });
      if (res.ok) {
        const data: FileInfo[] = await res.json();
        setFiles(data || []);
      } else {
        setFiles([]);
      }
    } catch {
      setFiles([]);
    } finally {
      setLoading(false);
    }
  }, [pid]);

  useEffect(() => {
    if (open) {
      fetchFiles(currentPrefix);
    }
  }, [open, currentPrefix, fetchFiles]);

  const handleUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    setUploading(true);
    try {
      const token = localStorage.getItem('auth_token');
      const headers: Record<string, string> = {};
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const form = new FormData();
      form.append('file', file);
      if (currentPrefix) {
        form.append('path', currentPrefix + '/' + file.name);
      }
      const res = await fetch(`/api/v1/workspaces/${pid}/files`, {
        method: 'POST',
        headers,
        body: form,
      });
      if (res.ok) {
        const info: FileInfo = await res.json();
        onChange(info.path || file.name);
        fetchFiles(currentPrefix);
      }
    } finally {
      setUploading(false);
    }
  };

  const selectFile = (file: FileInfo) => {
    if (file.isDir) {
      setCurrentPrefix(file.path);
    } else {
      onChange(file.path);
      setOpen(false);
    }
  };

  const goUp = () => {
    const parts = currentPrefix.split('/').filter(Boolean);
    parts.pop();
    setCurrentPrefix(parts.join('/'));
  };

  return (
    <div>
      <div style={{ display: 'flex', gap: 4 }}>
        <input
          type="text"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder || 'Select file...'}
          style={{ ...inputStyle, flex: 1 }}
        />
        <button
          onClick={() => setOpen(!open)}
          style={{
            background: '#313244',
            border: '1px solid #45475a',
            borderRadius: 4,
            color: '#cdd6f4',
            cursor: 'pointer',
            fontSize: 11,
            padding: '4px 8px',
            whiteSpace: 'nowrap',
          }}
        >
          {open ? 'Close' : 'Browse'}
        </button>
      </div>
      {description && (
        <span style={{ color: '#585b70', fontSize: 10, display: 'block', marginTop: 2 }}>{description}</span>
      )}

      {open && (
        <div
          style={{
            background: '#1e1e2e',
            border: '1px solid #313244',
            borderRadius: 4,
            marginTop: 4,
            maxHeight: 200,
            overflowY: 'auto',
            fontSize: 11,
          }}
        >
          {/* Navigation bar */}
          <div style={{ display: 'flex', gap: 4, padding: '4px 6px', borderBottom: '1px solid #313244', alignItems: 'center' }}>
            {currentPrefix && (
              <button
                onClick={goUp}
                style={{
                  background: 'none',
                  border: 'none',
                  color: '#89b4fa',
                  cursor: 'pointer',
                  fontSize: 11,
                  padding: '2px 4px',
                }}
              >
                .. (up)
              </button>
            )}
            <span style={{ color: '#585b70', fontSize: 10, flex: 1 }}>
              /{currentPrefix}
            </span>
            <label
              style={{
                background: '#313244',
                border: '1px solid #45475a',
                borderRadius: 3,
                color: '#cdd6f4',
                cursor: 'pointer',
                fontSize: 10,
                padding: '2px 6px',
              }}
            >
              {uploading ? 'Uploading...' : 'Upload'}
              <input
                type="file"
                onChange={handleUpload}
                style={{ display: 'none' }}
                disabled={uploading}
              />
            </label>
          </div>

          {loading ? (
            <div style={{ padding: 8, color: '#585b70', textAlign: 'center' }}>Loading...</div>
          ) : files.length === 0 ? (
            <div style={{ padding: 8, color: '#585b70', textAlign: 'center' }}>No files</div>
          ) : (
            files.map((f) => (
              <div
                key={f.path}
                onClick={() => selectFile(f)}
                style={{
                  padding: '4px 8px',
                  cursor: 'pointer',
                  display: 'flex',
                  justifyContent: 'space-between',
                  alignItems: 'center',
                  borderBottom: '1px solid #181825',
                }}
                onMouseEnter={(e) => (e.currentTarget.style.background = '#313244')}
                onMouseLeave={(e) => (e.currentTarget.style.background = 'transparent')}
              >
                <span style={{ color: f.isDir ? '#89b4fa' : '#cdd6f4' }}>
                  {f.isDir ? '[dir] ' : ''}{f.name}
                </span>
                {!f.isDir && (
                  <span style={{ color: '#585b70', fontSize: 10 }}>
                    {formatSize(f.size)}
                  </span>
                )}
              </div>
            ))
          )}
        </div>
      )}
    </div>
  );
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
