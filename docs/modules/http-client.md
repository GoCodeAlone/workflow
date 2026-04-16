# http.client Module

The `http.client` module provides a reusable, authenticated `*http.Client` as a service in the modular DI registry.  Other modules and pipeline steps resolve it by the configured module name to make HTTP requests without embedding auth logic inline.

---

## Configuration

```yaml
modules:
  - name: <module-name>
    type: http.client
    config:
      base_url: ""      # optional: base URL prepended to relative request URLs
      timeout: 30s      # optional: per-request deadline (default: 30s)
      auth:
        type: <auth-type>
        # ... auth-type-specific fields (see below)
```

### `base_url`

When set, callers can use the module's `BaseURL()` accessor to construct full URLs.
The `http.client` module does **not** automatically prepend the base URL to requests —
that responsibility belongs to the caller (e.g. a `step.http_call` referencing this
client via the `client:` field, which is introduced in PR 4).

---

## Auth types

### `none`

Plain `*http.Client` with the configured `timeout`.  No credentials are added.

```yaml
auth:
  type: none
```

### `static_bearer`

Every outgoing request receives `Authorization: Bearer <token>`.

```yaml
auth:
  type: static_bearer
  bearer_token: "my-static-token"         # inline value
  # OR resolve from a secrets provider:
  bearer_token_ref:
    provider: my-secrets
    key: api_token
```

If both `bearer_token` and `bearer_token_ref` are set, the inline value takes precedence.

### `oauth2_client_credentials`

Uses the OAuth2 `client_credentials` grant.  The token is fetched once on the first
request and cached in-process.  On a `401` response the cache is invalidated and the
token is refreshed exactly once.

```yaml
auth:
  type: oauth2_client_credentials
  token_url: "https://example.com/oauth/token"
  client_id: "my-client-id"
  client_secret: "my-client-secret"
  scopes:
    - "api.read"
    - "api.write"
  # Resolve client_id / client_secret from a secrets provider instead:
  # client_id_from_secret:
  #   provider: my-secrets
  #   key: client_id
  # client_secret_from_secret:
  #   provider: my-secrets
  #   key: client_secret
```

### `oauth2_refresh_token`

Persists the full `oauth2.Token` (access token + refresh token + expiry) as a JSON
blob in a named `secrets.Provider`.  This enables:

- Tokens that were obtained out-of-band (e.g. via an OAuth2 callback flow and stored
  via `step.secret_set`) to be picked up without restarting the module.
- Automatic rotation: when the access token expires, the module exchanges the refresh
  token for a new pair and persists the rotated tokens back to the provider.
- 401-triggered refresh: if the upstream rejects the current access token, the module
  invalidates the in-process cache, re-reads the provider (which may have been updated
  externally), and retries the request once.

```yaml
modules:
  - name: zoom-secrets
    type: secrets.keychain
    config:
      service: zoom-mcp

  - name: zoom-client
    type: http.client
    config:
      base_url: "https://api.zoom.us/v2"
      timeout: 30s
      auth:
        type: oauth2_refresh_token
        token_url: "https://zoom.us/oauth/token"
        # Resolve client credentials from the secrets provider at Init time:
        client_id_from_secret:
          provider: zoom-secrets
          key: client_id
        client_secret_from_secret:
          provider: zoom-secrets
          key: client_secret
        # Where the oauth2.Token JSON blob is stored:
        token_secrets: zoom-secrets       # service-registry name of the secrets module
        token_secrets_key: oauth_token    # key within that provider
```

#### Token storage format

The token is stored as the JSON serialisation of `golang.org/x/oauth2.Token`:

```json
{
  "access_token": "...",
  "refresh_token": "...",
  "expiry": "2025-04-16T12:00:00Z",
  "token_type": "Bearer"
}
```

Use `step.secret_set` to write this blob into the provider when you receive tokens
from an OAuth2 callback.

#### Missing token behaviour

If no token is stored at startup the module **starts cleanly** — it does not panic or
fail `Init`.  The first HTTP request will fail with an `*oauth2.RetrieveError`
(HTTP 401, error code `no_token`).  Once a valid token is written to the provider via
`step.secret_set`, subsequent requests succeed without a restart.

---

## secrets.Provider integration

`client_id_from_secret`, `client_secret_from_secret`, and `bearer_token_ref` all use
the `{provider, key}` SecretRef shape.  `provider` must match the service-registry
name of a running secrets module (e.g. `secrets.keychain`, `secrets.aws`,
`secrets.vault`).  Resolution happens at `Start()` time so all secrets modules must
be started before `http.client` modules that reference them.

---

## Using the client in pipeline steps

> **Note:** The `client:` reference on `step.http_call` is introduced in PR 4.
> This section describes the intended future usage.

```yaml
steps:
  - name: list-meetings
    type: step.http_call
    config:
      client: zoom-client     # references the http.client module above
      url: "/users/me/meetings"
      method: GET
```

When `client:` is set, `step.http_call` resolves the `HTTPClient` service from the
DI registry, calls `Client()` to obtain the `*http.Client`, and prepends `BaseURL()`
to the request URL.

---

## Service interface

```go
type HTTPClient interface {
    Client()  *http.Client
    BaseURL() string
}
```

Any module registered in the DI registry that implements this interface can be
referenced as a `client:` dependency.
