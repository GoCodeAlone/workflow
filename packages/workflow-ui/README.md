# @gocodealoneorg/workflow-ui

Shared UI component library for [GoCodeAlone Workflow](https://github.com/GoCodeAlone/workflow) applications.

Provides reusable React components, stores (Zustand), types, and API client utilities extracted from the workflow admin UI.

## Installation

```bash
npm install @gocodealoneorg/workflow-ui
```

### Peer Dependencies

This package requires the following peer dependencies to be installed in your project:

```bash
npm install react react-dom @xyflow/react zustand
```

## Features

- **Auth** — Login/signup page, OAuth SSO button, first-run setup wizard, and a Zustand auth store
- **Layout** — Resizable/collapsible side panel (`CollapsiblePanel`)
- **API Client** — Typed fetch utilities for all workflow engine REST endpoints
- **Visual Builder** — `WorkflowCanvas` with React Flow–based drag-and-drop node editor and typed node components
- **Observability / Dashboard** — `SystemDashboard` and `WorkflowDashboard` components for monitoring executions and logs
- **Toast Notifications** — `ToastContainer` powered by the workflow store
- **Stores** — Zustand stores for auth, workflow graph state, module schemas, observability, and UI layout
- **Types** — Complete TypeScript types for workflow configs and observability models

## Usage

### Auth components

```tsx
import { LoginPage, SetupWizard, OAuthButton, useAuthStore } from '@gocodealoneorg/workflow-ui';

function App() {
  const { isAuthenticated, setupRequired } = useAuthStore();

  if (setupRequired) return <SetupWizard />;
  if (!isAuthenticated) return <LoginPage />;

  return <YourApp />;
}

// Add an OAuth SSO button inside LoginPage or any page
<OAuthButton provider="google" />
<OAuthButton provider="okta" />
```

### Workflow Visual Builder

```tsx
import { WorkflowCanvas, useWorkflowStore } from '@gocodealoneorg/workflow-ui';
import { ReactFlowProvider } from '@xyflow/react';

function WorkflowEditor() {
  return (
    <ReactFlowProvider>
      <WorkflowCanvas />
    </ReactFlowProvider>
  );
}
```

### Dashboard / Observability

```tsx
import { SystemDashboard, WorkflowDashboard } from '@gocodealoneorg/workflow-ui';

// System-wide overview
<SystemDashboard />

// Per-workflow dashboard
<WorkflowDashboard workflowId="my-workflow-id" />
```

### API Client

```tsx
import {
  apiLogin,
  apiListWorkflows,
  apiGetWorkflow,
  apiFetchExecutions,
} from '@gocodealoneorg/workflow-ui';

// All functions return typed Promises
const workflows = await apiListWorkflows();
const executions = await apiFetchExecutions(workflowId, { status: 'failed' });
```

### Layout

```tsx
import { CollapsiblePanel } from '@gocodealoneorg/workflow-ui';
import { useState } from 'react';

function Sidebar() {
  const [collapsed, setCollapsed] = useState(false);

  return (
    <CollapsiblePanel
      side="left"
      panelName="Sidebar"
      collapsed={collapsed}
      onToggle={() => setCollapsed((c) => !c)}
      width={240}
    >
      {/* sidebar content */}
    </CollapsiblePanel>
  );
}
```

### Toast Notifications

```tsx
import { ToastContainer, useWorkflowStore } from '@gocodealoneorg/workflow-ui';

function App() {
  const addToast = useWorkflowStore((s) => s.addToast);

  return (
    <>
      <button onClick={() => addToast({ id: '1', message: 'Saved!', type: 'success' })}>
        Save
      </button>
      <ToastContainer />
    </>
  );
}
```

## Building

```bash
npm install
npm run build
```

The build outputs to `dist/`:

| File | Format | Use |
|------|--------|-----|
| `dist/workflow-ui.js` | ES module | Modern bundlers (Vite, webpack 5+) |
| `dist/workflow-ui.umd.cjs` | UMD / CommonJS | Legacy bundlers / Node.js |
| `dist/index.d.ts` | TypeScript declarations | Type checking |

## Development (monorepo)

This package lives in `packages/workflow-ui` within the GoCodeAlone/workflow monorepo. The root `package.json` declares a workspace so the main `ui/` application can depend on this package via:

```json
{
  "dependencies": {
    "@gocodealoneorg/workflow-ui": "workspace:*"
  }
}
```

## Contributing

See [CONTRIBUTING.md](../../CONTRIBUTING.md) in the repository root.

## License

MIT — see [LICENSE](../../LICENSE).
