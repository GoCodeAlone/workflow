import useAuthStore from '../../store/authStore.ts';

interface OAuthButtonProps {
  provider: string;
  label?: string;
}

const PROVIDER_STYLES: Record<string, { bg: string; color: string; label: string }> = {
  google: { bg: '#ffffff', color: '#333333', label: 'Continue with Google' },
  okta: { bg: '#007dc1', color: '#ffffff', label: 'Continue with Okta' },
  auth0: { bg: '#eb5424', color: '#ffffff', label: 'Continue with Auth0' },
};

const DEFAULT_STYLE = { bg: '#585b70', color: '#cdd6f4', label: 'Continue with SSO' };

export default function OAuthButton({ provider, label }: OAuthButtonProps) {
  const oauthLogin = useAuthStore((s) => s.oauthLogin);
  const style = PROVIDER_STYLES[provider] || DEFAULT_STYLE;

  return (
    <button
      onClick={() => oauthLogin(provider)}
      style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 8,
        width: '100%',
        padding: '10px 16px',
        background: style.bg,
        color: style.color,
        border: '1px solid #45475a',
        borderRadius: 6,
        fontSize: 14,
        fontWeight: 500,
        cursor: 'pointer',
      }}
    >
      {label || style.label}
    </button>
  );
}
