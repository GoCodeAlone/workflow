import { useState, useEffect } from 'react';

interface StorageConfig {
  provider: 'local' | 's3' | 'gcs';
  local?: { rootDir: string };
  s3?: { bucket: string; region: string; endpoint: string };
  gcs?: { bucket: string; project: string; credentialsFile: string };
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

const labelStyle: React.CSSProperties = {
  display: 'block',
  marginBottom: 10,
};

const labelTextStyle: React.CSSProperties = {
  color: '#a6adc8',
  fontSize: 11,
  display: 'block',
  marginBottom: 3,
};

export default function StorageSettings() {
  const [config, setConfig] = useState<StorageConfig>({
    provider: 'local',
    local: { rootDir: './data/storage' },
    s3: { bucket: '', region: 'us-east-1', endpoint: '' },
    gcs: { bucket: '', project: '', credentialsFile: '' },
  });
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState('');

  useEffect(() => {
    const load = async () => {
      try {
        const token = localStorage.getItem('auth_token');
        const headers: Record<string, string> = {};
        if (token) headers['Authorization'] = `Bearer ${token}`;
        const res = await fetch('/api/v1/settings/storage', { headers });
        if (res.ok) {
          const data = await res.json();
          setConfig((prev) => ({ ...prev, ...data }));
        }
      } catch {
        // Use defaults
      }
    };
    load();
  }, []);

  const save = async () => {
    setSaving(true);
    setMessage('');
    try {
      const token = localStorage.getItem('auth_token');
      const headers: Record<string, string> = { 'Content-Type': 'application/json' };
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const res = await fetch('/api/v1/settings/storage', {
        method: 'PUT',
        headers,
        body: JSON.stringify(config),
      });
      if (res.ok) {
        setMessage('Saved');
      } else {
        const err = await res.json().catch(() => ({ error: 'Save failed' }));
        setMessage(err.error || 'Save failed');
      }
    } catch {
      setMessage('Network error');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div style={{ padding: 16, maxWidth: 480 }}>
      <h3 style={{ color: '#cdd6f4', margin: '0 0 16px', fontSize: 16 }}>Storage Configuration</h3>

      <label style={labelStyle}>
        <span style={labelTextStyle}>Storage Provider</span>
        <select
          value={config.provider}
          onChange={(e) => setConfig({ ...config, provider: e.target.value as StorageConfig['provider'] })}
          style={inputStyle}
        >
          <option value="local">Local Filesystem</option>
          <option value="s3">Amazon S3</option>
          <option value="gcs">Google Cloud Storage</option>
        </select>
      </label>

      {config.provider === 'local' && (
        <label style={labelStyle}>
          <span style={labelTextStyle}>Root Directory</span>
          <input
            type="text"
            value={config.local?.rootDir ?? ''}
            onChange={(e) => setConfig({ ...config, local: { ...config.local!, rootDir: e.target.value } })}
            placeholder="./data/storage"
            style={inputStyle}
          />
          <span style={{ color: '#585b70', fontSize: 10, display: 'block', marginTop: 2 }}>
            Filesystem path for workspace file storage
          </span>
        </label>
      )}

      {config.provider === 's3' && (
        <>
          <label style={labelStyle}>
            <span style={labelTextStyle}>Bucket</span>
            <input
              type="text"
              value={config.s3?.bucket ?? ''}
              onChange={(e) => setConfig({ ...config, s3: { ...config.s3!, bucket: e.target.value } })}
              placeholder="my-bucket"
              style={inputStyle}
            />
          </label>
          <label style={labelStyle}>
            <span style={labelTextStyle}>Region</span>
            <input
              type="text"
              value={config.s3?.region ?? ''}
              onChange={(e) => setConfig({ ...config, s3: { ...config.s3!, region: e.target.value } })}
              placeholder="us-east-1"
              style={inputStyle}
            />
          </label>
          <label style={labelStyle}>
            <span style={labelTextStyle}>Custom Endpoint (optional)</span>
            <input
              type="text"
              value={config.s3?.endpoint ?? ''}
              onChange={(e) => setConfig({ ...config, s3: { ...config.s3!, endpoint: e.target.value } })}
              placeholder="http://localhost:9000"
              style={inputStyle}
            />
            <span style={{ color: '#585b70', fontSize: 10, display: 'block', marginTop: 2 }}>
              For MinIO or other S3-compatible endpoints
            </span>
          </label>
        </>
      )}

      {config.provider === 'gcs' && (
        <>
          <label style={labelStyle}>
            <span style={labelTextStyle}>Bucket</span>
            <input
              type="text"
              value={config.gcs?.bucket ?? ''}
              onChange={(e) => setConfig({ ...config, gcs: { ...config.gcs!, bucket: e.target.value } })}
              placeholder="my-bucket"
              style={inputStyle}
            />
          </label>
          <label style={labelStyle}>
            <span style={labelTextStyle}>GCP Project</span>
            <input
              type="text"
              value={config.gcs?.project ?? ''}
              onChange={(e) => setConfig({ ...config, gcs: { ...config.gcs!, project: e.target.value } })}
              placeholder="my-project"
              style={inputStyle}
            />
          </label>
          <label style={labelStyle}>
            <span style={labelTextStyle}>Credentials File</span>
            <input
              type="text"
              value={config.gcs?.credentialsFile ?? ''}
              onChange={(e) => setConfig({ ...config, gcs: { ...config.gcs!, credentialsFile: e.target.value } })}
              placeholder="credentials/gcs-key.json"
              style={inputStyle}
            />
          </label>
        </>
      )}

      <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginTop: 16 }}>
        <button
          onClick={save}
          disabled={saving}
          style={{
            padding: '8px 16px',
            background: '#89b4fa',
            border: 'none',
            borderRadius: 6,
            color: '#1e1e2e',
            cursor: saving ? 'wait' : 'pointer',
            fontSize: 12,
            fontWeight: 600,
          }}
        >
          {saving ? 'Saving...' : 'Save'}
        </button>
        {message && (
          <span style={{ color: message === 'Saved' ? '#a6e3a1' : '#f38ba8', fontSize: 12 }}>
            {message}
          </span>
        )}
      </div>
    </div>
  );
}
