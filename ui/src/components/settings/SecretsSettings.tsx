import { useState } from 'react';

interface ProviderConfig {
  enabled: boolean;
  address?: string;
  token?: string;
  mountPath?: string;
  namespace?: string;
  region?: string;
  accessKeyId?: string;
  secretAccessKey?: string;
}

interface SecretsConfig {
  vault: ProviderConfig;
  aws: ProviderConfig;
}

const defaultConfig: SecretsConfig = {
  vault: { enabled: false, address: '', token: '', mountPath: 'secret', namespace: '' },
  aws: { enabled: false, region: 'us-east-1', accessKeyId: '', secretAccessKey: '' },
};

export default function SecretsSettings() {
  const [config, setConfig] = useState<SecretsConfig>(defaultConfig);
  const [saved, setSaved] = useState(false);

  const updateProvider = (provider: keyof SecretsConfig, field: string, value: unknown) => {
    setSaved(false);
    setConfig((prev) => ({
      ...prev,
      [provider]: { ...prev[provider], [field]: value },
    }));
  };

  const handleSave = () => {
    // Store in localStorage for now; in production this would POST to the server
    localStorage.setItem('secrets_config', JSON.stringify(config));
    setSaved(true);
    setTimeout(() => setSaved(false), 2000);
  };

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 16 }}>
        <h3 style={{ color: '#cdd6f4', margin: 0, fontSize: 16, fontWeight: 600 }}>
          Secrets Providers
        </h3>
      </div>

      <p style={{ color: '#6c7086', fontSize: 13, marginBottom: 20 }}>
        Configure external secrets providers. Secret references in module configs
        (e.g. <code style={{ color: '#89b4fa' }}>${'${vault:secret/path#field}'}</code> or{' '}
        <code style={{ color: '#89b4fa' }}>${'${aws-sm:my-secret}'}</code>) will be resolved
        at startup.
      </p>

      {/* Vault Provider */}
      <div style={{ background: '#313244', borderRadius: 8, padding: 16, marginBottom: 16 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 12 }}>
          <input
            type="checkbox"
            checked={config.vault.enabled}
            onChange={(e) => updateProvider('vault', 'enabled', e.target.checked)}
          />
          <span style={{ color: '#cdd6f4', fontSize: 14, fontWeight: 600 }}>
            HashiCorp Vault
          </span>
        </div>

        {config.vault.enabled && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            <label>
              <span style={{ color: '#a6adc8', fontSize: 12, display: 'block', marginBottom: 4 }}>
                Vault Address
              </span>
              <input
                type="text"
                value={config.vault.address ?? ''}
                onChange={(e) => updateProvider('vault', 'address', e.target.value)}
                placeholder="https://vault.example.com:8200"
                style={inputStyle}
              />
            </label>
            <label>
              <span style={{ color: '#a6adc8', fontSize: 12, display: 'block', marginBottom: 4 }}>
                Token
              </span>
              <input
                type="password"
                value={config.vault.token ?? ''}
                onChange={(e) => updateProvider('vault', 'token', e.target.value)}
                placeholder="hvs.xxxxx"
                style={inputStyle}
              />
            </label>
            <label>
              <span style={{ color: '#a6adc8', fontSize: 12, display: 'block', marginBottom: 4 }}>
                Mount Path
              </span>
              <input
                type="text"
                value={config.vault.mountPath ?? 'secret'}
                onChange={(e) => updateProvider('vault', 'mountPath', e.target.value)}
                placeholder="secret"
                style={inputStyle}
              />
            </label>
            <label>
              <span style={{ color: '#a6adc8', fontSize: 12, display: 'block', marginBottom: 4 }}>
                Namespace (optional)
              </span>
              <input
                type="text"
                value={config.vault.namespace ?? ''}
                onChange={(e) => updateProvider('vault', 'namespace', e.target.value)}
                placeholder="admin"
                style={inputStyle}
              />
            </label>
          </div>
        )}
      </div>

      {/* AWS Secrets Manager Provider */}
      <div style={{ background: '#313244', borderRadius: 8, padding: 16, marginBottom: 16 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 12 }}>
          <input
            type="checkbox"
            checked={config.aws.enabled}
            onChange={(e) => updateProvider('aws', 'enabled', e.target.checked)}
          />
          <span style={{ color: '#cdd6f4', fontSize: 14, fontWeight: 600 }}>
            AWS Secrets Manager
          </span>
        </div>

        {config.aws.enabled && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            <label>
              <span style={{ color: '#a6adc8', fontSize: 12, display: 'block', marginBottom: 4 }}>
                AWS Region
              </span>
              <input
                type="text"
                value={config.aws.region ?? 'us-east-1'}
                onChange={(e) => updateProvider('aws', 'region', e.target.value)}
                placeholder="us-east-1"
                style={inputStyle}
              />
            </label>
            <label>
              <span style={{ color: '#a6adc8', fontSize: 12, display: 'block', marginBottom: 4 }}>
                Access Key ID (optional, uses default chain if empty)
              </span>
              <input
                type="password"
                value={config.aws.accessKeyId ?? ''}
                onChange={(e) => updateProvider('aws', 'accessKeyId', e.target.value)}
                placeholder="AKIA..."
                style={inputStyle}
              />
            </label>
            <label>
              <span style={{ color: '#a6adc8', fontSize: 12, display: 'block', marginBottom: 4 }}>
                Secret Access Key (optional)
              </span>
              <input
                type="password"
                value={config.aws.secretAccessKey ?? ''}
                onChange={(e) => updateProvider('aws', 'secretAccessKey', e.target.value)}
                placeholder="secret key"
                style={inputStyle}
              />
            </label>
          </div>
        )}
      </div>

      <button
        onClick={handleSave}
        style={{
          background: saved ? '#a6e3a1' : '#89b4fa',
          border: 'none',
          borderRadius: 6,
          color: '#1e1e2e',
          padding: '8px 20px',
          fontSize: 13,
          fontWeight: 600,
          cursor: 'pointer',
        }}
      >
        {saved ? 'Saved' : 'Save Configuration'}
      </button>
    </div>
  );
}

const inputStyle: React.CSSProperties = {
  width: '100%',
  background: '#1e1e2e',
  border: '1px solid #45475a',
  borderRadius: 6,
  color: '#cdd6f4',
  padding: '8px 12px',
  fontSize: 13,
  outline: 'none',
  boxSizing: 'border-box',
};
