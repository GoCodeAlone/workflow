# Visual Workflow Editor Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extract the visual workflow editor from `workflow/ui` into a standalone npm package (`@gocodealone/workflow-editor`), then embed it in VS Code and JetBrains IDE plugins as a split-pane visual preview alongside YAML text editing.

**Architecture:** The editor is extracted from `workflow/ui/src/` (canvas, nodes, properties, palette, stores, serialization utils) into a new `workflow-editor` repo published to GitHub Packages npm. Six coupling points are fixed (direct API calls → callback props, store singletons → parameter injection). VS Code uses Webview API, JetBrains uses JCEF — both communicate with the editor via a shared message protocol. A CI notification chain (`workflow-release` → `editor-release`) keeps all consumers in sync.

**Tech Stack:** React 19, @xyflow/react 12, Zustand 5, Vite 7 (library mode), TypeScript 5.9, esbuild (VS Code), Gradle/Kotlin (JetBrains), JCEF

**Design doc:** `docs/plans/2026-03-12-workflow-editor-design.md`

---

## Phase 1: Create `workflow-editor` Package

### Task 1: Scaffold the `workflow-editor` repo

**Files:**
- Create: `workflow-editor/package.json`
- Create: `workflow-editor/vite.config.ts`
- Create: `workflow-editor/tsconfig.json`
- Create: `workflow-editor/tsconfig.build.json`
- Create: `workflow-editor/.npmrc`
- Create: `workflow-editor/.gitignore`

**Step 1: Create the repo on GitHub**

```bash
gh repo create GoCodeAlone/workflow-editor --public --clone
cd workflow-editor
```

**Step 2: Create package.json**

Follow the same pattern as `@gocodealone/workflow-ui` (`publishConfig.registry: https://npm.pkg.github.com`).

```json
{
  "name": "@gocodealone/workflow-editor",
  "version": "0.1.0",
  "type": "module",
  "main": "./dist/index.cjs",
  "module": "./dist/index.js",
  "types": "./dist/index.d.ts",
  "exports": {
    ".": {
      "types": "./dist/index.d.ts",
      "import": "./dist/index.js",
      "require": "./dist/index.cjs"
    },
    "./stores": {
      "types": "./dist/stores/index.d.ts",
      "import": "./dist/stores/index.js",
      "require": "./dist/stores/index.cjs"
    },
    "./utils": {
      "types": "./dist/utils/index.d.ts",
      "import": "./dist/utils/index.js",
      "require": "./dist/utils/index.cjs"
    },
    "./types": {
      "types": "./dist/types/index.d.ts",
      "import": "./dist/types/index.js",
      "require": "./dist/types/index.cjs"
    }
  },
  "files": ["dist"],
  "scripts": {
    "build": "vite build && tsc --project tsconfig.build.json --emitDeclarationOnly",
    "test": "vitest run",
    "test:watch": "vitest",
    "lint": "eslint src/"
  },
  "peerDependencies": {
    "@xyflow/react": "^12.0.0",
    "react": "^18.0.0 || ^19.0.0",
    "react-dom": "^18.0.0 || ^19.0.0",
    "zustand": "^4.0.0 || ^5.0.0"
  },
  "dependencies": {
    "dagre": "^0.8.5",
    "js-yaml": "^4.1.1"
  },
  "devDependencies": {
    "@testing-library/jest-dom": "^6.9.1",
    "@testing-library/react": "^16.3.2",
    "@testing-library/user-event": "^14.6.1",
    "@types/dagre": "^0.7.52",
    "@types/js-yaml": "^4.0.9",
    "@types/react": "^19.2.7",
    "@types/react-dom": "^19.2.3",
    "@vitejs/plugin-react": "^5.1.1",
    "@xyflow/react": "^12.10.1",
    "eslint": "^9.39.1",
    "jsdom": "^26.1.0",
    "react": "^19.2.0",
    "react-dom": "^19.2.0",
    "typescript": "~5.9.3",
    "vite": "^7.3.1",
    "vitest": "^4.0.18",
    "zustand": "^5.0.11"
  },
  "publishConfig": {
    "registry": "https://npm.pkg.github.com"
  },
  "repository": {
    "type": "git",
    "url": "https://github.com/GoCodeAlone/workflow-editor.git"
  },
  "license": "Apache-2.0"
}
```

**Step 3: Create vite.config.ts**

Multi-entry library build, same pattern as workflow-ui:

```ts
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { resolve } from 'path';

export default defineConfig({
  plugins: [react()],
  build: {
    lib: {
      entry: {
        index: resolve(__dirname, 'src/index.ts'),
        'stores/index': resolve(__dirname, 'src/stores/index.ts'),
        'utils/index': resolve(__dirname, 'src/utils/index.ts'),
        'types/index': resolve(__dirname, 'src/types/index.ts'),
      },
      formats: ['es', 'cjs'],
    },
    rollupOptions: {
      external: [
        'react',
        'react-dom',
        'react/jsx-runtime',
        'zustand',
        'zustand/middleware',
        '@xyflow/react',
      ],
    },
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
  },
});
```

**Step 4: Create tsconfig.json and tsconfig.build.json**

`tsconfig.json`:
```json
{
  "compilerOptions": {
    "target": "ES2022",
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "moduleResolution": "bundler",
    "jsx": "react-jsx",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "forceConsistentCasingInFileNames": true,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "declaration": true,
    "declarationDir": "./dist",
    "outDir": "./dist",
    "rootDir": "./src",
    "verbatimModuleSyntax": true,
    "allowImportingTsExtensions": true,
    "noEmit": true
  },
  "include": ["src"]
}
```

`tsconfig.build.json`:
```json
{
  "extends": "./tsconfig.json",
  "compilerOptions": {
    "declaration": true,
    "declarationDir": "./dist",
    "emitDeclarationOnly": true,
    "noEmit": false
  },
  "include": ["src"],
  "exclude": ["src/**/*.test.ts", "src/**/*.test.tsx", "src/test/**"]
}
```

**Step 5: Create .npmrc**

```
@gocodealone:registry=https://npm.pkg.github.com
```

**Step 6: Create .gitignore**

```
node_modules/
dist/
*.tsbuildinfo
```

**Step 7: Create test setup**

Create `src/test/setup.ts`:
```ts
import '@testing-library/jest-dom';
```

**Step 8: npm install and verify build scaffolding compiles**

```bash
npm install
npx tsc --noEmit  # expect errors (no src yet) — just verify config is valid
```

**Step 9: Commit**

```bash
git add -A
git commit -m "chore: scaffold workflow-editor package"
```

---

### Task 2: Extract TypeScript types

**Files:**
- Copy: `workflow/ui/src/types/workflow.ts` → `workflow-editor/src/types/workflow.ts`
- Create: `workflow-editor/src/types/index.ts`
- Create: `workflow-editor/src/types/editor.ts` (new host integration types)

**Step 1: Copy the types file**

Copy `workflow/ui/src/types/workflow.ts` to `workflow-editor/src/types/workflow.ts` verbatim. This file contains all core types (`ModuleConfig`, `WorkflowConfig`, `WorkflowEdgeType`, `ModuleCategory`, `ConfigFieldDef`, `IOSignature`, `MODULE_TYPES`, `MODULE_TYPE_MAP`, `CATEGORIES`, `CATEGORY_COLORS`, `WorkflowTab`, etc.) and only depends on `@xyflow/react` types.

**Step 2: Create editor integration types**

Create `workflow-editor/src/types/editor.ts`:

```ts
/**
 * Props for the top-level WorkflowEditor component.
 * The host environment (IDE webview, browser app) provides these callbacks.
 */
export interface WorkflowEditorProps {
  /** Initial YAML content to load */
  initialYaml?: string;
  /** Called when the editor produces updated YAML (graph edit, node add/remove, etc.) */
  onChange?: (yaml: string) => void;
  /** Called when user triggers save (Ctrl+S or toolbar button) */
  onSave?: (yaml: string) => Promise<void>;
  /** Called when user clicks a node — host should navigate to the YAML line */
  onNavigateToSource?: (line: number, col: number) => void;
  /** Called when editor needs schema data (module types, step types) */
  onSchemaRequest?: () => Promise<ModuleSchemaData | null>;
  /** Called when editor needs plugin schemas */
  onPluginSchemaRequest?: () => Promise<PluginSchemaData[] | null>;
}

/** Schema data the host provides for built-in module/step types */
export interface ModuleSchemaData {
  modules: Record<string, ServerModuleSchema>;
  services?: string[];
}

/** Schema data for a single external plugin */
export interface PluginSchemaData {
  pluginName: string;
  pluginIcon?: string;
  pluginColor?: string;
  modules: Record<string, ServerModuleSchema>;
}

/** Server-side module schema (matches moduleSchemaStore's existing format) */
export interface ServerModuleSchema {
  label?: string;
  category?: string;
  configFields?: import('./workflow').ConfigFieldDef[];
  defaultConfig?: Record<string, unknown>;
  ioSignature?: import('./workflow').IOSignature;
  maxIncoming?: number;
  maxOutgoing?: number;
}
```

**Step 3: Create types barrel export**

Create `workflow-editor/src/types/index.ts`:

```ts
export * from './workflow';
export * from './editor';
```

**Step 4: Verify types compile**

```bash
cd workflow-editor
npx tsc --noEmit
```

Expected: PASS (types only depend on `@xyflow/react` types which are in devDependencies)

**Step 5: Commit**

```bash
git add src/types/
git commit -m "feat: extract workflow types and add editor integration types"
```

---

### Task 3: Extract utility functions

**Files:**
- Copy: `workflow/ui/src/utils/autoLayout.ts` → `workflow-editor/src/utils/autoLayout.ts`
- Copy: `workflow/ui/src/utils/connectionCompatibility.ts` → `workflow-editor/src/utils/connectionCompatibility.ts`
- Copy: `workflow/ui/src/utils/grouping.ts` → `workflow-editor/src/utils/grouping.ts`
- Copy: `workflow/ui/src/utils/snapToConnect.ts` → `workflow-editor/src/utils/snapToConnect.ts`
- Copy: `workflow/ui/src/utils/importExport.ts` → `workflow-editor/src/utils/importExport.ts`
- Copy+Modify: `workflow/ui/src/utils/serialization.ts` → `workflow-editor/src/utils/serialization.ts`
- Create: `workflow-editor/src/utils/index.ts`

**Step 1: Copy pure utility files**

Copy these files verbatim — they have no coupling issues:
- `autoLayout.ts` — only deps: `dagre`, `@xyflow/react`
- `connectionCompatibility.ts` — pure logic
- `grouping.ts` — pure logic
- `snapToConnect.ts` — pure logic
- `importExport.ts` — config import/export

**Step 2: Copy and fix serialization.ts**

Copy `workflow/ui/src/utils/serialization.ts` to `workflow-editor/src/utils/serialization.ts`.

**Fix the coupling:** Find the `getModuleTypeMap()` helper function (around line 19-22) that calls `useModuleSchemaStore.getState()`:

```ts
// BEFORE (in workflow/ui)
function getModuleTypeMap() {
  const store = useModuleSchemaStore.getState();
  return store.loaded ? store.moduleTypeMap : MODULE_TYPE_MAP;
}
```

Replace with a parameter-based approach. Add a `moduleTypeMap` parameter to `configToNodes` and `nodesToConfig`, and remove the `getModuleTypeMap()` function:

```ts
// AFTER (in workflow-editor)
// Remove getModuleTypeMap() entirely.
// Instead, configToNodes and nodesToConfig accept an optional moduleTypeMap parameter.
// If not provided, fall back to the static MODULE_TYPE_MAP.
```

In `configToNodes(config, moduleTypeMap?)`:
- Replace all calls to `getModuleTypeMap()` with `moduleTypeMap ?? MODULE_TYPE_MAP`

In `nodesToConfig(nodes, edges, moduleTypeMap?)`:
- Same replacement

In `nodeComponentType(moduleType)`:
- This function doesn't use the store — leave as-is

Also remove the import of `useModuleSchemaStore` from serialization.ts entirely.

**Step 3: Copy existing tests**

Copy `workflow/ui/src/utils/serialization.test.ts` → `workflow-editor/src/utils/serialization.test.ts`
Copy `workflow/ui/src/utils/grouping.test.ts` → `workflow-editor/src/utils/grouping.test.ts`

Update any imports to use relative paths within the new package.

**Step 4: Create utils barrel export**

Create `workflow-editor/src/utils/index.ts`:

```ts
export {
  configToNodes,
  nodesToConfig,
  configToYaml,
  parseYaml,
  nodeComponentType,
  extractWorkflowEdges,
  multiConfigToTabs,
  nodesToMultiConfig,
} from './serialization';
export { layoutNodes } from './autoLayout';
export {
  getCompatibleTargets,
  isCompatibleConnection,
} from './connectionCompatibility';
export { computeContainerView, autoGroupOrphanedNodes } from './grouping';
export { findSnapCandidate } from './snapToConnect';
```

**Step 5: Run tests**

```bash
cd workflow-editor
npx vitest run
```

Expected: All serialization and grouping tests pass.

**Step 6: Commit**

```bash
git add src/utils/
git commit -m "feat: extract utility functions with decoupled serialization"
```

---

### Task 4: Extract Zustand stores

**Files:**
- Copy+Modify: `workflow/ui/src/store/workflowStore.ts` → `workflow-editor/src/stores/workflowStore.ts`
- Copy+Modify: `workflow/ui/src/store/moduleSchemaStore.ts` → `workflow-editor/src/stores/moduleSchemaStore.ts`
- Copy: `workflow/ui/src/store/uiLayoutStore.ts` → `workflow-editor/src/stores/uiLayoutStore.ts`
- Create: `workflow-editor/src/stores/index.ts`

**Step 1: Copy and fix moduleSchemaStore**

Copy `workflow/ui/src/store/moduleSchemaStore.ts` to `workflow-editor/src/stores/moduleSchemaStore.ts`.

**Fix:** The `fetchSchemas()` method calls `GET /api/v1/admin/schemas/modules` directly. Add a `loadSchemas()` action for host injection, and make `fetchSchemas()` accept an optional URL parameter:

```ts
// Add to the store actions:
loadSchemas: (schemas: Record<string, ServerModuleSchema>) => void;
loadPluginSchemas: (plugins: PluginSchemaData[]) => void;
```

`loadSchemas` implementation:
```ts
loadSchemas: (schemas) => {
  // Same merge logic as fetchSchemas but without the fetch call
  const merged = mergeWithStaticTypes(schemas);
  set({ serverSchemas: schemas, moduleTypes: merged.types, moduleTypeMap: merged.map, loaded: true, loading: false });
},
```

`loadPluginSchemas` implementation:
```ts
loadPluginSchemas: (plugins) => {
  // Append plugin types to existing moduleTypes/moduleTypeMap
  for (const plugin of plugins) {
    for (const [type, schema] of Object.entries(plugin.modules)) {
      const info = schemaToModuleTypeInfo(type, schema, plugin.pluginName);
      // Add to moduleTypes array and moduleTypeMap
    }
  }
  set({ moduleTypes: [...get().moduleTypes], moduleTypeMap: { ...get().moduleTypeMap } });
},
```

Keep the existing `fetchSchemas()` as-is for backward compat (browser apps will still call it). It just becomes one way to load schemas alongside `loadSchemas()`.

**Step 2: Copy and fix workflowStore**

Copy `workflow/ui/src/store/workflowStore.ts` to `workflow-editor/src/stores/workflowStore.ts`.

**Fix 1:** Remove `ApiWorkflowRecord` import. Replace the type with a generic:

```ts
// BEFORE
import type { ApiWorkflowRecord } from '../utils/api';
// ...
activeWorkflowRecord: ApiWorkflowRecord | null;

// AFTER
activeWorkflowRecord: Record<string, unknown> | null;
```

**Fix 2:** The `addNode()` method calls `useModuleSchemaStore.getState().moduleTypeMap`. This is fine — both stores are co-packaged. Just update the import path.

**Fix 3:** The `exportToConfig()` and `importFromConfig()` calls to serialization utils now need to pass `moduleTypeMap`:

```ts
// BEFORE
const { nodes, edges } = configToNodes(config);

// AFTER
const moduleTypeMap = useModuleSchemaStore.getState().moduleTypeMap;
const { nodes, edges } = configToNodes(config, moduleTypeMap);
```

Same for `nodesToConfig`:
```ts
const moduleTypeMap = useModuleSchemaStore.getState().moduleTypeMap;
const config = nodesToConfig(nodes, edges, moduleTypeMap);
```

**Step 3: Copy uiLayoutStore verbatim**

No changes needed — zero coupling.

**Step 4: Create stores barrel export**

Create `workflow-editor/src/stores/index.ts`:

```ts
export { useWorkflowStore } from './workflowStore';
export { useModuleSchemaStore } from './moduleSchemaStore';
export { useUILayoutStore, PANEL_WIDTH_LIMITS } from './uiLayoutStore';
```

**Step 5: Verify compilation**

```bash
npx tsc --noEmit
```

**Step 6: Commit**

```bash
git add src/stores/
git commit -m "feat: extract Zustand stores with host-injectable schema loading"
```

---

### Task 5: Extract canvas components

**Files:**
- Copy+Modify: `workflow/ui/src/components/canvas/WorkflowCanvas.tsx` → `workflow-editor/src/components/canvas/WorkflowCanvas.tsx`
- Copy: `workflow/ui/src/components/canvas/DeletableEdge.tsx` → `workflow-editor/src/components/canvas/DeletableEdge.tsx`
- Copy: `workflow/ui/src/components/canvas/EdgeContextMenu.tsx` → `workflow-editor/src/components/canvas/EdgeContextMenu.tsx`
- Copy: `workflow/ui/src/components/canvas/NodeContextMenu.tsx` → `workflow-editor/src/components/canvas/NodeContextMenu.tsx`
- Copy: `workflow/ui/src/components/canvas/ConnectionPicklist.tsx` → `workflow-editor/src/components/canvas/ConnectionPicklist.tsx`

**Step 1: Copy supporting canvas components verbatim**

Copy `DeletableEdge.tsx`, `EdgeContextMenu.tsx`, `NodeContextMenu.tsx`, `ConnectionPicklist.tsx` — no coupling issues.

**Step 2: Copy and fix WorkflowCanvas.tsx**

Copy `workflow/ui/src/components/canvas/WorkflowCanvas.tsx` to `workflow-editor/src/components/canvas/WorkflowCanvas.tsx`.

**Fix: Replace `saveWorkflowConfig` with callback prop.**

The Ctrl+S handler (around line 389) calls `saveWorkflowConfig(config)` directly from `utils/api.ts`. Change it to use a callback:

```ts
// BEFORE
import { saveWorkflowConfig } from '../../utils/api';
// ...
// Inside keyboard handler:
const config = exportToConfig();
saveWorkflowConfig(config)
  .then(() => addToast('Workflow saved to server', 'success'))
  .catch((err: Error) => addToast(`Save failed: ${err.message}`, 'error'));

// AFTER — remove the api import, accept onSave as a prop
interface WorkflowCanvasProps {
  onSave?: (yaml: string) => Promise<void>;
  onNavigateToSource?: (line: number, col: number) => void;
}
// ...
// Inside keyboard handler:
if (props.onSave) {
  const config = exportToConfig();
  const yaml = configToYaml(config);
  props.onSave(yaml)
    .then(() => addToast('Workflow saved', 'success'))
    .catch((err: Error) => addToast(`Save failed: ${err.message}`, 'error'));
}
```

**Step 3: Update all import paths**

All imports should reference the package-internal paths (`../../stores/workflowStore`, `../../types/workflow`, etc.).

**Step 4: Verify compilation**

```bash
npx tsc --noEmit
```

**Step 5: Commit**

```bash
git add src/components/canvas/
git commit -m "feat: extract canvas components with callback-based save"
```

---

### Task 6: Extract node components

**Files:**
- Copy: All 14 files from `workflow/ui/src/components/nodes/` → `workflow-editor/src/components/nodes/`

**Step 1: Copy all node component files verbatim**

All node components (`BaseNode.tsx`, `ConditionalNode.tsx`, `EventProcessorNode.tsx`, `GroupNode.tsx`, `HTTPRouterNode.tsx`, `HTTPServerNode.tsx`, `InfrastructureNode.tsx`, `IntegrationNode.tsx`, `MessagingBrokerNode.tsx`, `MiddlewareNode.tsx`, `SchedulerNode.tsx`, `StateMachineNode.tsx`, `TriggerNode.tsx`, `index.ts`) are pure display components with no API dependencies. Copy as-is.

**Step 2: Update import paths in index.ts**

The `index.ts` re-exports `nodeTypes` map. Verify imports resolve to the new paths.

**Step 3: Verify compilation**

```bash
npx tsc --noEmit
```

**Step 4: Commit**

```bash
git add src/components/nodes/
git commit -m "feat: extract node components"
```

---

### Task 7: Extract property panel and sub-editors

**Files:**
- Copy: All 10 files from `workflow/ui/src/components/properties/` → `workflow-editor/src/components/properties/`

**Step 1: Copy all property component files**

Copy: `PropertyPanel.tsx`, `ArrayFieldEditor.tsx`, `MapFieldEditor.tsx`, `MiddlewareChainEditor.tsx`, `RoutePipelineEditor.tsx`, `SqlEditor.tsx`, `FieldPicker.tsx`, `FilePicker.tsx`, `DelegateServicePicker.tsx`, `PropertyPanel.test.tsx`.

These components only read from `workflowStore` and `moduleSchemaStore` — no API calls.

**Step 2: Update import paths**

All imports should reference `../../stores/`, `../../types/`, etc.

**Step 3: Run existing tests**

```bash
npx vitest run src/components/properties/
```

**Step 4: Commit**

```bash
git add src/components/properties/
git commit -m "feat: extract property panel and sub-editors"
```

---

### Task 8: Extract NodePalette and Toolbar

**Files:**
- Copy: `workflow/ui/src/components/sidebar/NodePalette.tsx` → `workflow-editor/src/components/sidebar/NodePalette.tsx`
- Copy+Modify: `workflow/ui/src/components/toolbar/Toolbar.tsx` → `workflow-editor/src/components/toolbar/Toolbar.tsx`

**Step 1: Copy NodePalette verbatim**

No coupling — reads from `moduleSchemaStore`, drag-and-drop via DataTransfer.

**Step 2: Copy and fix Toolbar**

The Toolbar makes 6+ direct API calls (`apiUpdateWorkflow`, `apiLoadWorkflowFromPath`, `apiDeployWorkflow`, `apiStopWorkflow`, `getWorkflowConfig`, `validateWorkflow`). Convert all to callback props:

```ts
interface ToolbarProps {
  onSave?: (yaml: string) => Promise<void>;
  onValidate?: (yaml: string) => Promise<{ valid: boolean; errors?: string[] }>;
  onDeploy?: () => Promise<void>;
  onStop?: () => Promise<void>;
  onLoadFromServer?: () => Promise<string | null>; // returns YAML or null
  onImportFromPath?: (path: string) => Promise<string | null>;
  showServerControls?: boolean; // hide deploy/stop/load in IDE mode
}
```

Remove all imports from `../../utils/api.ts`. Replace each API call site with the corresponding callback:

```ts
// BEFORE
const config = exportToConfig();
const yaml = configToYaml(config);
await apiUpdateWorkflow(activeWorkflowRecord.id, { name: activeWorkflowRecord.name, config_yaml: yaml });

// AFTER
if (props.onSave) {
  const config = exportToConfig();
  const yaml = configToYaml(config);
  await props.onSave(yaml);
}
```

If `showServerControls` is false (IDE mode), hide the Deploy/Stop/Load from Server buttons.

**Step 3: Update import paths**

**Step 4: Verify compilation**

```bash
npx tsc --noEmit
```

**Step 5: Commit**

```bash
git add src/components/sidebar/ src/components/toolbar/
git commit -m "feat: extract NodePalette and Toolbar with callback props"
```

---

### Task 9: Create WorkflowEditor wrapper and public API

**Files:**
- Create: `workflow-editor/src/components/WorkflowEditor.tsx`
- Create: `workflow-editor/src/index.ts`

**Step 1: Create the WorkflowEditor wrapper component**

This is the main entry point that composes canvas + palette + property panel + toolbar with the host callback interface:

```tsx
import { ReactFlowProvider } from '@xyflow/react';
import type { WorkflowEditorProps } from '../types/editor';
import { WorkflowCanvas } from './canvas/WorkflowCanvas';
import { NodePalette } from './sidebar/NodePalette';
import { PropertyPanel } from './properties/PropertyPanel';
import { Toolbar } from './toolbar/Toolbar';
import { useWorkflowStore } from '../stores/workflowStore';
import { useModuleSchemaStore } from '../stores/moduleSchemaStore';
import { useUILayoutStore } from '../stores/uiLayoutStore';
import { parseYaml, configToYaml } from '../utils/serialization';
import { useEffect, useRef } from 'react';

export function WorkflowEditor(props: WorkflowEditorProps) {
  const { initialYaml, onChange, onSave, onNavigateToSource, onSchemaRequest, onPluginSchemaRequest } = props;
  const initialized = useRef(false);
  const importFromConfig = useWorkflowStore((s) => s.importFromConfig);
  const loadSchemas = useModuleSchemaStore((s) => s.loadSchemas);
  const loadPluginSchemas = useModuleSchemaStore((s) => s.loadPluginSchemas);

  // Load initial YAML
  useEffect(() => {
    if (initialYaml && !initialized.current) {
      initialized.current = true;
      const config = parseYaml(initialYaml);
      if (config) importFromConfig(config);
    }
  }, [initialYaml, importFromConfig]);

  // Request schemas from host
  useEffect(() => {
    if (onSchemaRequest) {
      onSchemaRequest().then((data) => {
        if (data) loadSchemas(data.modules);
      });
    }
    if (onPluginSchemaRequest) {
      onPluginSchemaRequest().then((plugins) => {
        if (plugins) loadPluginSchemas(plugins);
      });
    }
  }, [onSchemaRequest, onPluginSchemaRequest, loadSchemas, loadPluginSchemas]);

  return (
    <ReactFlowProvider>
      <div style={{ display: 'flex', height: '100%', width: '100%' }}>
        <NodePalette />
        <div style={{ flex: 1, position: 'relative' }}>
          <Toolbar
            onSave={onSave}
            showServerControls={false}
          />
          <WorkflowCanvas
            onSave={onSave}
            onNavigateToSource={onNavigateToSource}
          />
        </div>
        <PropertyPanel />
      </div>
    </ReactFlowProvider>
  );
}
```

**Step 2: Create the root barrel export**

Create `workflow-editor/src/index.ts`:

```ts
// Main component
export { WorkflowEditor } from './components/WorkflowEditor';

// Individual components (for custom layouts)
export { WorkflowCanvas } from './components/canvas/WorkflowCanvas';
export { NodePalette } from './components/sidebar/NodePalette';
export { PropertyPanel } from './components/properties/PropertyPanel';
export { Toolbar } from './components/toolbar/Toolbar';
export { nodeTypes } from './components/nodes';

// Re-export sub-paths
export * from './types';
export * from './stores';
export * from './utils';
```

**Step 3: Build the package**

```bash
npm run build
```

Expected: Vite builds ES + CJS bundles, tsc emits declarations. No errors.

**Step 4: Run all tests**

```bash
npm test
```

Expected: All extracted tests pass.

**Step 5: Commit**

```bash
git add src/components/WorkflowEditor.tsx src/index.ts
git commit -m "feat: add WorkflowEditor wrapper and public API"
```

---

### Task 10: Add CI workflows

**Files:**
- Create: `workflow-editor/.github/workflows/publish.yml`
- Create: `workflow-editor/.github/workflows/sync-schema.yml`
- Create: `workflow-editor/.github/workflows/build.yml`

**Step 1: Create build.yml (CI on push/PR)**

```yaml
name: Build

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: '22'
          registry-url: 'https://npm.pkg.github.com'
          scope: '@gocodealone'
      - run: npm ci
        env:
          NODE_AUTH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      - run: npx tsc --noEmit
      - run: npm test
      - run: npm run build
```

**Step 2: Create publish.yml (publish on tag + dispatch editor-release)**

```yaml
name: Publish

on:
  push:
    tags: ['v*']

permissions:
  contents: write
  packages: write

jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-node@v4
        with:
          node-version: '22'
          registry-url: 'https://npm.pkg.github.com'
          scope: '@gocodealone'

      - run: npm ci
        env:
          NODE_AUTH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      - run: npm test
      - run: npm run build
      - run: npm publish
        env:
          NODE_AUTH_TOKEN: ${{ secrets.GITHUB_TOKEN }}

  notify-consumers:
    name: Notify Consumers
    runs-on: ubuntu-latest
    needs: publish
    strategy:
      matrix:
        repo:
          - 'GoCodeAlone/workflow-vscode'
          - 'GoCodeAlone/workflow-jetbrains'
          - 'GoCodeAlone/workflow-plugin-admin'
    steps:
      - name: Dispatch editor-release to ${{ matrix.repo }}
        uses: peter-evans/repository-dispatch@v4
        with:
          token: ${{ secrets.REPO_DISPATCH_TOKEN }}
          repository: ${{ matrix.repo }}
          event-type: editor-release
          client-payload: '{"version": "${{ github.ref_name }}"}'
```

**Step 3: Create sync-schema.yml (listen for workflow-release)**

```yaml
name: Sync Schema on Workflow Release

on:
  repository_dispatch:
    types: [workflow-release]

permissions:
  contents: write
  packages: write

jobs:
  sync:
    name: Update types and publish
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'

      - uses: actions/setup-node@v4
        with:
          node-version: '22'
          registry-url: 'https://npm.pkg.github.com'
          scope: '@gocodealone'

      - name: Install wfctl
        env:
          GOPRIVATE: github.com/GoCodeAlone/*
          GONOSUMCHECK: github.com/GoCodeAlone/*
          GOFLAGS: -buildvcs=false
        run: |
          WORKFLOW_VERSION="${{ github.event.client_payload.version }}"
          go install "github.com/GoCodeAlone/workflow/cmd/wfctl@${WORKFLOW_VERSION}"

      - name: Regenerate type catalogue
        run: wfctl schema --output schemas/workflow-config.schema.json

      - name: Bump version
        run: |
          WORKFLOW_VERSION="${{ github.event.client_payload.version }}"
          EDITOR_VERSION="${WORKFLOW_VERSION#v}"
          npm version "${EDITOR_VERSION}" --no-git-tag-version

      - run: npm ci
        env:
          NODE_AUTH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      - run: npx tsc --noEmit
      - run: npm test
      - run: npm run build

      - name: Commit and tag
        run: |
          WORKFLOW_VERSION="${{ github.event.client_payload.version }}"
          git config user.name "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"
          git add -A
          git diff --cached --quiet && echo "No changes" && exit 0
          git commit -m "chore: sync types to workflow ${WORKFLOW_VERSION}"
          git push
          git tag "${WORKFLOW_VERSION}"
          git push origin "${WORKFLOW_VERSION}"
```

**Step 4: Create schemas directory placeholder**

```bash
mkdir -p schemas
echo '{}' > schemas/workflow-config.schema.json
```

**Step 5: Commit**

```bash
git add .github/ schemas/
git commit -m "ci: add build, publish, and sync-schema workflows"
```

---

### Task 11: Write integration tests for the editor

**Files:**
- Create: `workflow-editor/src/components/WorkflowEditor.test.tsx`
- Create: `workflow-editor/src/utils/serialization.test.ts` (if not already copied)

**Step 1: Write WorkflowEditor render test**

```tsx
import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { WorkflowEditor } from './WorkflowEditor';

describe('WorkflowEditor', () => {
  it('renders without crashing', () => {
    render(<WorkflowEditor />);
    // NodePalette should be visible
    expect(document.querySelector('[data-testid="node-palette"]') || document.body).toBeTruthy();
  });

  it('loads initial YAML and renders nodes', async () => {
    const yaml = `
modules:
  - name: web
    type: http.server
    config:
      address: ":8080"
  - name: router
    type: http.router
    config:
      routes: []
    dependsOn:
      - web
`;
    render(<WorkflowEditor initialYaml={yaml} />);
    // After loading, nodes should be created in the store
    // The canvas should render ReactFlow nodes
  });

  it('calls onSave with YAML when save triggered', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    render(<WorkflowEditor initialYaml="modules: []" onSave={onSave} />);
    // Trigger save via store or keyboard simulation
  });

  it('calls onSchemaRequest on mount', async () => {
    const onSchemaRequest = vi.fn().mockResolvedValue({ modules: {}, services: [] });
    render(<WorkflowEditor onSchemaRequest={onSchemaRequest} />);
    expect(onSchemaRequest).toHaveBeenCalled();
  });
});
```

**Step 2: Write YAML round-trip test**

```ts
import { describe, it, expect } from 'vitest';
import { configToNodes, nodesToConfig, configToYaml, parseYaml } from './serialization';
import { MODULE_TYPE_MAP } from '../types/workflow';

describe('serialization round-trip', () => {
  it('converts YAML → nodes → YAML preserving structure', () => {
    const yaml = `
modules:
  - name: web
    type: http.server
    config:
      address: ":8080"
  - name: router
    type: http.router
    config:
      routes: []
    dependsOn:
      - web
`;
    const config = parseYaml(yaml);
    expect(config).toBeTruthy();
    const { nodes, edges } = configToNodes(config!, MODULE_TYPE_MAP);
    expect(nodes.length).toBe(2);
    expect(edges.length).toBeGreaterThanOrEqual(1);

    const roundTripped = nodesToConfig(nodes, edges, MODULE_TYPE_MAP);
    expect(roundTripped.modules).toHaveLength(2);
    expect(roundTripped.modules[0].name).toBe('web');
    expect(roundTripped.modules[1].name).toBe('router');
    expect(roundTripped.modules[1].dependsOn).toContain('web');
  });
});
```

**Step 3: Run tests**

```bash
npm test
```

Expected: All tests pass.

**Step 4: Commit**

```bash
git add src/components/WorkflowEditor.test.tsx src/utils/
git commit -m "test: add integration tests for WorkflowEditor and serialization"
```

---

### Task 12: Initial publish

**Step 1: Final build**

```bash
npm run build
```

**Step 2: Tag and push**

```bash
git tag v0.1.0
git push origin main --tags
```

This triggers `publish.yml` → publishes `@gocodealone/workflow-editor@0.1.0` to GitHub Packages.

---

## Phase 2: Update `workflow/ui` to Consume the Package

### Task 13: Replace inline editor code with package import

**Files:**
- Modify: `workflow/ui/package.json` — add `@gocodealone/workflow-editor` dependency
- Modify: `workflow/ui/src/` — replace local component imports with package imports
- Delete: `workflow/ui/src/types/workflow.ts` (now in package)
- Delete: `workflow/ui/src/utils/serialization.ts`, `autoLayout.ts`, `connectionCompatibility.ts`, `grouping.ts`, `snapToConnect.ts` (now in package)
- Delete: `workflow/ui/src/store/workflowStore.ts`, `moduleSchemaStore.ts`, `uiLayoutStore.ts` (now in package)
- Delete: `workflow/ui/src/components/canvas/`, `nodes/`, `properties/`, `sidebar/NodePalette.tsx` (now in package)

**Step 1: Add the dependency**

```bash
cd /Users/jon/workspace/workflow/ui
npm install @gocodealone/workflow-editor@0.1.0
```

**Step 2: Update imports throughout workflow/ui/src/**

Replace all local imports with package imports. For example:

```ts
// BEFORE
import { useWorkflowStore } from '../store/workflowStore';
import { WorkflowConfig } from '../types/workflow';
import { configToYaml } from '../utils/serialization';

// AFTER
import { useWorkflowStore } from '@gocodealone/workflow-editor/stores';
import type { WorkflowConfig } from '@gocodealone/workflow-editor/types';
import { configToYaml } from '@gocodealone/workflow-editor/utils';
```

The app-shell files that remain (`App.tsx`, `utils/api.ts`, `store/authStore.ts`, `store/pluginStore.ts`, etc.) update their imports to use the package.

The `Toolbar` in the app-shell now wraps the package's Toolbar with the API callbacks:

```tsx
import { Toolbar as EditorToolbar } from '@gocodealone/workflow-editor';
import { apiUpdateWorkflow, apiDeployWorkflow, apiStopWorkflow } from '../utils/api';

function AppToolbar() {
  return (
    <EditorToolbar
      onSave={async (yaml) => { await apiUpdateWorkflow(record.id, { config_yaml: yaml }); }}
      onDeploy={async () => { await apiDeployWorkflow(record.id); }}
      onStop={async () => { await apiStopWorkflow(record.id); }}
      showServerControls={true}
    />
  );
}
```

**Step 3: Delete extracted files**

Remove all files that are now provided by the package (listed above).

**Step 4: Build and test**

```bash
npm run build
npm test
```

**Step 5: Commit**

```bash
git add -A
git commit -m "refactor: consume @gocodealone/workflow-editor package"
```

---

## Phase 3: VS Code Extension — Visual Editor

### Task 14: Add webview infrastructure to workflow-vscode

**Files:**
- Create: `workflow-vscode/src/visual-editor.ts`
- Modify: `workflow-vscode/src/extension.ts`
- Modify: `workflow-vscode/package.json`

**Step 1: Add the visual editor command and webview contribution to package.json**

Add to `contributes.commands`:
```json
{
  "command": "workflow.openVisualEditor",
  "title": "Workflow: Open Visual Editor",
  "icon": "$(graph)"
}
```

Add editor title button:
```json
"menus": {
  "editor/title": [
    {
      "command": "workflow.openVisualEditor",
      "when": "resourceExtname == .yaml || resourceExtname == .yml",
      "group": "navigation"
    }
  ]
}
```

Add settings:
```json
"workflow.configPaths": {
  "type": "array",
  "items": { "type": "string" },
  "default": [],
  "description": "Glob patterns for workflow config files (e.g. config/app.yaml)"
}
```

**Step 2: Create visual-editor.ts**

```ts
import * as vscode from 'vscode';
import * as path from 'path';
import * as fs from 'fs';

export class WorkflowVisualEditorProvider {
  private panel: vscode.WebviewPanel | undefined;
  private document: vscode.TextDocument | undefined;
  private updatingFromEditor = false;
  private updatingFromWebview = false;

  constructor(private context: vscode.ExtensionContext) {}

  public open(document: vscode.TextDocument) {
    this.document = document;

    if (this.panel) {
      this.panel.reveal(vscode.ViewColumn.Beside);
      this.sendYamlToEditor(document.getText());
      return;
    }

    this.panel = vscode.window.createWebviewPanel(
      'workflowVisualEditor',
      'Workflow: ' + path.basename(document.fileName),
      vscode.ViewColumn.Beside,
      {
        enableScripts: true,
        retainContextWhenHidden: true,
        localResourceRoots: [
          vscode.Uri.joinPath(this.context.extensionUri, 'webview-dist'),
        ],
      }
    );

    this.panel.webview.html = this.getHtml(this.panel.webview);
    this.sendYamlToEditor(document.getText());
    this.setupMessageHandling();
    this.setupDocumentSync();

    this.panel.onDidDispose(() => {
      this.panel = undefined;
    });
  }

  private setupMessageHandling() {
    this.panel!.webview.onDidReceiveMessage((msg) => {
      switch (msg.type) {
        case 'yamlUpdated':
          this.handleYamlFromWebview(msg.content);
          break;
        case 'navigateToLine':
          this.navigateToLine(msg.line, msg.col);
          break;
        case 'requestSchemas':
          this.sendSchemas();
          break;
        case 'ready':
          this.sendYamlToEditor(this.document!.getText());
          this.sendSchemas();
          break;
      }
    });
  }

  private setupDocumentSync() {
    // Watch for text editor changes
    vscode.workspace.onDidChangeTextDocument((e) => {
      if (e.document === this.document && !this.updatingFromWebview) {
        this.updatingFromEditor = true;
        this.sendYamlToEditor(e.document.getText());
        this.updatingFromEditor = false;
      }
    });

    // Watch for cursor position changes
    vscode.window.onDidChangeTextEditorSelection((e) => {
      if (e.textEditor.document === this.document) {
        const pos = e.selections[0].active;
        this.panel?.webview.postMessage({
          type: 'cursorMoved',
          line: pos.line + 1,
          col: pos.character + 1,
        });
      }
    });
  }

  private sendYamlToEditor(content: string) {
    this.panel?.webview.postMessage({ type: 'yamlChanged', content });
  }

  private async handleYamlFromWebview(content: string) {
    if (!this.document || this.updatingFromEditor) return;
    this.updatingFromWebview = true;

    const edit = new vscode.WorkspaceEdit();
    edit.replace(
      this.document.uri,
      new vscode.Range(0, 0, this.document.lineCount, 0),
      content
    );
    await vscode.workspace.applyEdit(edit);

    this.updatingFromWebview = false;
  }

  private navigateToLine(line: number, col: number) {
    if (!this.document) return;
    const editor = vscode.window.visibleTextEditors.find(
      (e) => e.document === this.document
    );
    if (editor) {
      const pos = new vscode.Position(line - 1, col - 1);
      editor.selection = new vscode.Selection(pos, pos);
      editor.revealRange(new vscode.Range(pos, pos), vscode.TextEditorRevealType.InCenter);
    }
  }

  private sendSchemas() {
    // Load schemas from bundled JSON file
    const schemaPath = path.join(this.context.extensionPath, 'schemas', 'workflow-config.schema.json');
    try {
      const content = fs.readFileSync(schemaPath, 'utf-8');
      const schema = JSON.parse(content);
      this.panel?.webview.postMessage({ type: 'schemasLoaded', schemas: schema });
    } catch {
      // Schema file not available — editor degrades gracefully
    }
  }

  private getHtml(webview: vscode.Webview): string {
    const scriptUri = webview.asWebviewUri(
      vscode.Uri.joinPath(this.context.extensionUri, 'webview-dist', 'index.js')
    );
    const styleUri = webview.asWebviewUri(
      vscode.Uri.joinPath(this.context.extensionUri, 'webview-dist', 'index.css')
    );
    const nonce = getNonce();

    return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta http-equiv="Content-Security-Policy"
    content="default-src 'none'; style-src ${webview.cspSource} 'unsafe-inline'; script-src 'nonce-${nonce}';">
  <link rel="stylesheet" href="${styleUri}">
  <style>html, body, #root { height: 100%; margin: 0; overflow: hidden; }</style>
</head>
<body>
  <div id="root"></div>
  <script nonce="${nonce}" src="${scriptUri}"></script>
</body>
</html>`;
  }
}

function getNonce(): string {
  let text = '';
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  for (let i = 0; i < 32; i++) {
    text += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return text;
}
```

**Step 3: Register the command in extension.ts**

Add to `activate()`:

```ts
import { WorkflowVisualEditorProvider } from './visual-editor.js';

const editorProvider = new WorkflowVisualEditorProvider(context);

context.subscriptions.push(
  vscode.commands.registerCommand('workflow.openVisualEditor', () => {
    const editor = vscode.window.activeTextEditor;
    if (editor && isWorkflowFile(editor.document)) {
      editorProvider.open(editor.document);
    }
  })
);
```

**Step 4: Add file detection**

Add `isWorkflowFile()` function that checks:
1. `workflow.configPaths` setting (glob match)
2. Content detection (`modules:` + `workflows:` as top-level keys)

```ts
function isWorkflowFile(document: vscode.TextDocument): boolean {
  // Layer 1: explicit configPaths setting
  const configPaths: string[] = vscode.workspace.getConfiguration('workflow').get('configPaths', []);
  if (configPaths.length > 0) {
    const relative = vscode.workspace.asRelativePath(document.uri);
    for (const pattern of configPaths) {
      if (vscode.languages.match({ pattern }, document) > 0) return true;
    }
  }

  // Layer 2: content detection
  const text = document.getText(new vscode.Range(0, 0, 50, 0));
  return /^modules:/m.test(text) && /^workflows:/m.test(text);
}
```

**Step 5: Commit**

```bash
git add src/visual-editor.ts src/extension.ts package.json
git commit -m "feat: add visual editor webview infrastructure"
```

---

### Task 15: Bundle the editor for VS Code webview

**Files:**
- Create: `workflow-vscode/webview-src/index.tsx`
- Create: `workflow-vscode/webview-src/bridge.ts`
- Create: `workflow-vscode/webview-src/vite.config.ts`
- Modify: `workflow-vscode/package.json` (add build scripts + deps)

**Step 1: Add webview dependencies to package.json**

Add to `devDependencies`:
```json
"@gocodealone/workflow-editor": "^0.1.0",
"@xyflow/react": "^12.10.1",
"@vitejs/plugin-react": "^5.1.1",
"react": "^19.2.0",
"react-dom": "^19.2.0",
"zustand": "^5.0.11",
"vite": "^7.3.1"
```

Add `.npmrc` for GitHub Packages:
```
@gocodealone:registry=https://npm.pkg.github.com
```

Update scripts:
```json
"build": "node esbuild.config.js && npm run build:webview",
"build:webview": "cd webview-src && npx vite build --outDir ../webview-dist"
```

**Step 2: Create webview bridge**

Create `webview-src/bridge.ts`:

```ts
// VS Code webview ↔ editor bridge
// vscode API is available via acquireVsCodeApi()

const vscode = acquireVsCodeApi();

export interface BridgeCallbacks {
  onYamlChanged: (content: string) => void;
  onCursorMoved: (line: number, col: number) => void;
  onSchemasLoaded: (schemas: unknown) => void;
}

let callbacks: BridgeCallbacks | null = null;

export function initBridge(cb: BridgeCallbacks) {
  callbacks = cb;

  window.addEventListener('message', (event) => {
    const msg = event.data;
    switch (msg.type) {
      case 'yamlChanged':
        callbacks?.onYamlChanged(msg.content);
        break;
      case 'cursorMoved':
        callbacks?.onCursorMoved(msg.line, msg.col);
        break;
      case 'schemasLoaded':
        callbacks?.onSchemasLoaded(msg.schemas);
        break;
    }
  });

  // Tell host we're ready
  vscode.postMessage({ type: 'ready' });
}

export function sendYamlUpdated(content: string) {
  vscode.postMessage({ type: 'yamlUpdated', content });
}

export function sendNavigateToLine(line: number, col: number) {
  vscode.postMessage({ type: 'navigateToLine', line, col });
}

export function sendRequestSchemas() {
  vscode.postMessage({ type: 'requestSchemas' });
}
```

**Step 3: Create webview entry point**

Create `webview-src/index.tsx`:

```tsx
import React, { useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { WorkflowEditor } from '@gocodealone/workflow-editor';
import { initBridge, sendYamlUpdated, sendNavigateToLine, sendRequestSchemas } from './bridge';
import '@xyflow/react/dist/style.css';

function App() {
  const [yaml, setYaml] = useState<string>('');

  useEffect(() => {
    initBridge({
      onYamlChanged: (content) => setYaml(content),
      onCursorMoved: (line, col) => {
        // TODO: highlight corresponding node in editor
      },
      onSchemasLoaded: (schemas) => {
        // Inject into moduleSchemaStore
      },
    });
  }, []);

  return (
    <WorkflowEditor
      initialYaml={yaml}
      onChange={(newYaml) => sendYamlUpdated(newYaml)}
      onSave={async (newYaml) => sendYamlUpdated(newYaml)}
      onNavigateToSource={(line, col) => sendNavigateToLine(line, col)}
      onSchemaRequest={async () => {
        sendRequestSchemas();
        return null; // schemas arrive async via bridge callback
      }}
    />
  );
}

createRoot(document.getElementById('root')!).render(<App />);
```

**Step 4: Create Vite config for webview build**

Create `webview-src/vite.config.ts`:

```ts
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../webview-dist',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        entryFileNames: 'index.js',
        assetFileNames: 'index.[ext]',
      },
    },
  },
});
```

**Step 5: Build and verify**

```bash
cd workflow-vscode
npm install
npm run build
ls webview-dist/  # should contain index.js + index.css
```

**Step 6: Commit**

```bash
git add webview-src/ webview-dist/ .npmrc package.json
git commit -m "feat: bundle workflow-editor for VS Code webview"
```

---

### Task 16: Add sync-editor CI workflow to VS Code extension

**Files:**
- Create: `workflow-vscode/.github/workflows/sync-editor.yml`

**Step 1: Create the workflow**

```yaml
name: Sync Editor on Editor Release

on:
  repository_dispatch:
    types: [editor-release]

permissions:
  contents: write

jobs:
  sync:
    name: Update editor dependency
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-node@v4
        with:
          node-version: '22'
          registry-url: 'https://npm.pkg.github.com'
          scope: '@gocodealone'

      - name: Update editor package
        run: |
          EDITOR_VERSION="${{ github.event.client_payload.version }}"
          npm install "@gocodealone/workflow-editor@${EDITOR_VERSION#v}"
        env:
          NODE_AUTH_TOKEN: ${{ secrets.GITHUB_TOKEN }}

      - run: npm ci
        env:
          NODE_AUTH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      - run: npx tsc --noEmit
      - run: npm run build

      - name: Commit and tag
        run: |
          EDITOR_VERSION="${{ github.event.client_payload.version }}"
          git config user.name "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"
          git add -A
          git diff --cached --quiet && echo "No changes" && exit 0
          git commit -m "chore: sync editor to ${EDITOR_VERSION}"
          git push
          git tag "${EDITOR_VERSION}"
          git push origin "${EDITOR_VERSION}"
```

**Step 2: Commit**

```bash
git add .github/workflows/sync-editor.yml
git commit -m "ci: add sync-editor workflow for editor-release dispatch"
```

---

## Phase 4: JetBrains Plugin — Visual Editor

### Task 17: Add JCEF webview infrastructure to workflow-jetbrains

**Files:**
- Create: `workflow-jetbrains/src/main/kotlin/com/gocodalone/workflow/ide/editor/WorkflowVisualEditorProvider.kt`
- Create: `workflow-jetbrains/src/main/kotlin/com/gocodalone/workflow/ide/editor/WorkflowBridge.kt`
- Create: `workflow-jetbrains/src/main/kotlin/com/gocodalone/workflow/ide/editor/WorkflowFileDetector.kt`
- Modify: `workflow-jetbrains/src/main/resources/META-INF/plugin.xml`

**Step 1: Create WorkflowFileDetector**

```kotlin
package com.gocodalone.workflow.ide.editor

import com.gocodalone.workflow.ide.WorkflowBundle
import com.gocodalone.workflow.ide.settings.WorkflowSettings
import com.intellij.openapi.project.Project
import com.intellij.openapi.vfs.VirtualFile
import java.nio.file.FileSystems

class WorkflowFileDetector {
    companion object {
        fun isWorkflowFile(project: Project, file: VirtualFile): Boolean {
            // Layer 1: explicit configPaths from settings
            val settings = WorkflowSettings.getInstance()
            val configPaths = settings.configPaths
            if (configPaths.isNotEmpty()) {
                val projectBase = project.basePath ?: return false
                val relativePath = file.path.removePrefix("$projectBase/")
                for (pattern in configPaths) {
                    val matcher = FileSystems.getDefault().getPathMatcher("glob:$pattern")
                    if (matcher.matches(java.nio.file.Path.of(relativePath))) return true
                }
            }

            // Layer 2: content detection
            if (!file.name.endsWith(".yaml") && !file.name.endsWith(".yml")) return false
            try {
                val content = String(file.contentsToByteArray(), Charsets.UTF_8)
                val lines = content.lineSequence().take(50).toList()
                val hasModules = lines.any { it.trimStart().startsWith("modules:") }
                val hasWorkflows = lines.any { it.trimStart().startsWith("workflows:") }
                return hasModules && hasWorkflows
            } catch (_: Exception) {
                return false
            }
        }
    }
}
```

**Step 2: Create WorkflowBridge**

```kotlin
package com.gocodalone.workflow.ide.editor

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.editor.Editor
import com.intellij.openapi.editor.LogicalPosition
import com.intellij.openapi.editor.ScrollType
import com.intellij.openapi.fileEditor.FileDocumentManager
import com.intellij.openapi.fileEditor.FileEditorManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.ui.jcef.JBCefBrowser
import com.intellij.ui.jcef.JBCefJSQuery
import org.cef.browser.CefBrowser
import org.cef.handler.CefLoadHandlerAdapter

class WorkflowBridge(
    private val project: Project,
    private val file: VirtualFile,
    private val browser: JBCefBrowser,
) {
    private val yamlUpdatedQuery = JBCefJSQuery.create(browser)
    private val navigateQuery = JBCefJSQuery.create(browser)
    private val schemaRequestQuery = JBCefJSQuery.create(browser)
    private var updatingFromEditor = false
    private var updatingFromWebview = false

    fun initialize() {
        // Register JS→Kotlin message handlers
        yamlUpdatedQuery.addHandler { content ->
            handleYamlFromWebview(content)
            JBCefJSQuery.Response("")
        }

        navigateQuery.addHandler { data ->
            val parts = data.split(",")
            if (parts.size == 2) {
                navigateToLine(parts[0].toInt(), parts[1].toInt())
            }
            JBCefJSQuery.Response("")
        }

        schemaRequestQuery.addHandler {
            sendSchemas()
            JBCefJSQuery.Response("")
        }

        // Inject bridge functions after page load
        browser.jbCefClient.addLoadHandler(object : CefLoadHandlerAdapter() {
            override fun onLoadEnd(b: CefBrowser?, frame: org.cef.browser.CefFrame?, httpStatusCode: Int) {
                injectBridge()
                sendYamlToEditor()
            }
        }, browser.cefBrowser)
    }

    private fun injectBridge() {
        val js = """
            window.hostBridge = {
                sendYamlUpdated: function(content) {
                    ${yamlUpdatedQuery.inject("content")}
                },
                sendNavigateToLine: function(line, col) {
                    ${navigateQuery.inject("line + ',' + col")}
                },
                sendRequestSchemas: function() {
                    ${schemaRequestQuery.inject("''")}
                }
            };
            window.dispatchEvent(new Event('hostBridgeReady'));
        """.trimIndent()
        browser.cefBrowser.executeJavaScript(js, "", 0)
    }

    fun sendYamlToEditor() {
        if (updatingFromWebview) return
        updatingFromEditor = true
        val content = String(file.contentsToByteArray(), Charsets.UTF_8)
            .replace("\\", "\\\\")
            .replace("`", "\\`")
            .replace("\$", "\\\$")
        browser.cefBrowser.executeJavaScript(
            "window.onYamlChanged && window.onYamlChanged(`$content`);",
            "", 0
        )
        updatingFromEditor = false
    }

    private fun handleYamlFromWebview(content: String) {
        if (updatingFromEditor) return
        updatingFromWebview = true
        ApplicationManager.getApplication().invokeLater {
            ApplicationManager.getApplication().runWriteAction {
                val document = FileDocumentManager.getInstance().getDocument(file) ?: return@runWriteAction
                document.setText(content)
            }
            updatingFromWebview = false
        }
    }

    private fun navigateToLine(line: Int, col: Int) {
        ApplicationManager.getApplication().invokeLater {
            val editors = FileEditorManager.getInstance(project).openFile(file, true)
            val textEditor = editors.firstOrNull() ?: return@invokeLater
            // Find the text editor component
            val editor: Editor = (textEditor as? com.intellij.openapi.fileEditor.TextEditor)?.editor ?: return@invokeLater
            val pos = LogicalPosition(line - 1, col - 1)
            editor.caretModel.moveToLogicalPosition(pos)
            editor.scrollingModel.scrollToCaret(ScrollType.CENTER)
        }
    }

    private fun sendSchemas() {
        // Load from bundled schema file
        val schemaStream = javaClass.getResourceAsStream("/schemas/workflow-config.schema.json") ?: return
        val content = schemaStream.bufferedReader().readText()
            .replace("\\", "\\\\")
            .replace("`", "\\`")
            .replace("\$", "\\\$")
        browser.cefBrowser.executeJavaScript(
            "window.onSchemasLoaded && window.onSchemasLoaded(JSON.parse(`$content`));",
            "", 0
        )
    }

    fun dispose() {
        yamlUpdatedQuery.dispose()
        navigateQuery.dispose()
        schemaRequestQuery.dispose()
    }
}
```

**Step 3: Create WorkflowVisualEditorProvider**

```kotlin
package com.gocodalone.workflow.ide.editor

import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.actionSystem.CommonDataKeys
import com.intellij.openapi.fileEditor.FileEditorManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.wm.ToolWindow
import com.intellij.openapi.wm.ToolWindowFactory
import com.intellij.openapi.wm.ToolWindowManager
import com.intellij.ui.content.ContentFactory
import com.intellij.ui.jcef.JBCefBrowser
import javax.swing.JComponent

class WorkflowVisualEditorAction : AnAction("Open Visual Editor", "Open workflow visual editor", null) {
    override fun actionPerformed(e: AnActionEvent) {
        val project = e.project ?: return
        val file = e.getData(CommonDataKeys.VIRTUAL_FILE) ?: return
        if (!WorkflowFileDetector.isWorkflowFile(project, file)) return

        val toolWindow = ToolWindowManager.getInstance(project)
            .getToolWindow("Workflow Visual Editor") ?: return
        toolWindow.show {
            val browser = JBCefBrowser()
            val bridge = WorkflowBridge(project, file, browser)

            // Load the bundled editor HTML
            val htmlUrl = javaClass.getResource("/editor/index.html")?.toExternalForm() ?: return@show
            browser.loadURL(htmlUrl)
            bridge.initialize()

            val content = ContentFactory.getInstance()
                .createContent(browser.component, file.name, false)
            content.setDisposer { bridge.dispose() }
            toolWindow.contentManager.removeAllContents(true)
            toolWindow.contentManager.addContent(content)
        }
    }

    override fun update(e: AnActionEvent) {
        val project = e.project
        val file = e.getData(CommonDataKeys.VIRTUAL_FILE)
        e.presentation.isEnabledAndVisible = project != null && file != null &&
            (file.name.endsWith(".yaml") || file.name.endsWith(".yml"))
    }
}

class WorkflowVisualEditorToolWindowFactory : ToolWindowFactory {
    override fun createToolWindowContent(project: Project, toolWindow: ToolWindow) {
        // Content is created dynamically when action is triggered
    }

    override fun shouldBeAvailable(project: Project): Boolean = true
}
```

**Step 4: Register in plugin.xml**

Add to `plugin.xml`:

```xml
<!-- Visual Editor -->
<toolWindow id="Workflow Visual Editor"
            anchor="right"
            secondary="true"
            factoryClass="com.gocodalone.workflow.ide.editor.WorkflowVisualEditorToolWindowFactory"
            icon="/icons/workflow.svg"/>

<!-- Add action to editor context menu and toolbar -->
<action id="workflow.openVisualEditor"
        class="com.gocodalone.workflow.ide.editor.WorkflowVisualEditorAction"
        text="Open Visual Editor"
        description="Open workflow visual editor alongside YAML"
        icon="/icons/workflow.svg">
  <add-to-group group-id="WfctlGroup"/>
  <add-to-group group-id="EditorPopupMenu" anchor="last"/>
</action>
```

Add `configPaths` to `WorkflowSettings`:
```kotlin
var configPaths: List<String> = emptyList()
```

**Step 5: Commit**

```bash
git add src/main/kotlin/com/gocodalone/workflow/ide/editor/
git add src/main/resources/META-INF/plugin.xml
git commit -m "feat: add JCEF visual editor infrastructure"
```

---

### Task 18: Bundle the editor for JetBrains JCEF

**Files:**
- Create: `workflow-jetbrains/webview-src/` (same structure as VS Code webview, reusing bridge)
- Create: `workflow-jetbrains/webview-src/index.tsx`
- Create: `workflow-jetbrains/webview-src/bridge.ts`
- Create: `workflow-jetbrains/webview-src/vite.config.ts`
- Modify: `workflow-jetbrains/build.gradle.kts` (add webview build task)

**Step 1: Create JetBrains-specific bridge**

Create `webview-src/bridge.ts`:

```ts
// JetBrains JCEF ↔ editor bridge
// hostBridge is injected by WorkflowBridge.kt via JBCefJSQuery

export interface BridgeCallbacks {
  onYamlChanged: (content: string) => void;
  onCursorMoved: (line: number, col: number) => void;
  onSchemasLoaded: (schemas: unknown) => void;
}

let callbacks: BridgeCallbacks | null = null;

export function initBridge(cb: BridgeCallbacks) {
  callbacks = cb;

  // These are called by WorkflowBridge.kt via executeJavaScript
  (window as any).onYamlChanged = (content: string) => callbacks?.onYamlChanged(content);
  (window as any).onCursorMoved = (line: number, col: number) => callbacks?.onCursorMoved(line, col);
  (window as any).onSchemasLoaded = (schemas: unknown) => callbacks?.onSchemasLoaded(schemas);

  // Wait for hostBridge to be injected
  if ((window as any).hostBridge) {
    sendRequestSchemas();
  } else {
    window.addEventListener('hostBridgeReady', () => sendRequestSchemas());
  }
}

export function sendYamlUpdated(content: string) {
  (window as any).hostBridge?.sendYamlUpdated(content);
}

export function sendNavigateToLine(line: number, col: number) {
  (window as any).hostBridge?.sendNavigateToLine(line, col);
}

export function sendRequestSchemas() {
  (window as any).hostBridge?.sendRequestSchemas();
}
```

**Step 2: Create webview entry point**

Create `webview-src/index.tsx` (same structure as VS Code version but using JetBrains bridge):

```tsx
import React, { useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { WorkflowEditor } from '@gocodealone/workflow-editor';
import { initBridge, sendYamlUpdated, sendNavigateToLine } from './bridge';
import '@xyflow/react/dist/style.css';

function App() {
  const [yaml, setYaml] = useState<string>('');

  useEffect(() => {
    initBridge({
      onYamlChanged: (content) => setYaml(content),
      onCursorMoved: (_line, _col) => {
        // TODO: highlight corresponding node
      },
      onSchemasLoaded: (_schemas) => {
        // Inject into moduleSchemaStore
      },
    });
  }, []);

  return (
    <WorkflowEditor
      initialYaml={yaml}
      onChange={(newYaml) => sendYamlUpdated(newYaml)}
      onSave={async (newYaml) => sendYamlUpdated(newYaml)}
      onNavigateToSource={(line, col) => sendNavigateToLine(line, col)}
    />
  );
}

createRoot(document.getElementById('root')!).render(<App />);
```

**Step 3: Create Vite config**

Create `webview-src/vite.config.ts`:

```ts
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../src/main/resources/editor',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        entryFileNames: 'index.js',
        assetFileNames: 'index.[ext]',
      },
    },
  },
});
```

**Step 4: Add webview build to Gradle**

Add to `build.gradle.kts`:

```kotlin
tasks.register<Exec>("buildWebview") {
    workingDir = file("webview-src")
    commandLine("npx", "vite", "build", "--outDir", "../src/main/resources/editor")
}

tasks.named("processResources") {
    dependsOn("buildWebview")
}
```

Add `package.json` to `webview-src/`:
```json
{
  "private": true,
  "devDependencies": {
    "@gocodealone/workflow-editor": "^0.1.0",
    "@xyflow/react": "^12.10.1",
    "@vitejs/plugin-react": "^5.1.1",
    "react": "^19.2.0",
    "react-dom": "^19.2.0",
    "zustand": "^5.0.11",
    "vite": "^7.3.1"
  }
}
```

Add `webview-src/.npmrc`:
```
@gocodealone:registry=https://npm.pkg.github.com
```

**Step 5: Create index.html for JCEF**

Create `webview-src/index.html`:
```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <style>html, body, #root { height: 100%; margin: 0; overflow: hidden; }</style>
</head>
<body>
  <div id="root"></div>
  <script type="module" src="./index.tsx"></script>
</body>
</html>
```

**Step 6: Build and verify**

```bash
cd workflow-jetbrains
cd webview-src && npm install && npx vite build --outDir ../src/main/resources/editor
cd ..
./gradlew buildPlugin
```

**Step 7: Commit**

```bash
git add webview-src/ src/main/resources/editor/ build.gradle.kts
git commit -m "feat: bundle workflow-editor for JetBrains JCEF"
```

---

### Task 19: Add sync-editor CI workflow to JetBrains plugin

**Files:**
- Create: `workflow-jetbrains/.github/workflows/sync-editor.yml`

**Step 1: Create the workflow**

```yaml
name: Sync Editor on Editor Release

on:
  repository_dispatch:
    types: [editor-release]

permissions:
  contents: write

jobs:
  sync:
    name: Update editor dependency
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-node@v4
        with:
          node-version: '22'
          registry-url: 'https://npm.pkg.github.com'
          scope: '@gocodealone'

      - name: Update editor package
        working-directory: webview-src
        run: |
          EDITOR_VERSION="${{ github.event.client_payload.version }}"
          npm install "@gocodealone/workflow-editor@${EDITOR_VERSION#v}"
        env:
          NODE_AUTH_TOKEN: ${{ secrets.GITHUB_TOKEN }}

      - name: Build webview
        working-directory: webview-src
        run: |
          npm ci
          npx vite build --outDir ../src/main/resources/editor
        env:
          NODE_AUTH_TOKEN: ${{ secrets.GITHUB_TOKEN }}

      - uses: actions/setup-java@v4
        with:
          distribution: temurin
          java-version: '17'

      - name: Setup Gradle
        uses: gradle/actions/setup-gradle@v4

      - name: Build and test plugin
        run: ./gradlew buildPlugin test

      - name: Commit and tag
        run: |
          EDITOR_VERSION="${{ github.event.client_payload.version }}"
          git config user.name "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"
          git add -A
          git diff --cached --quiet && echo "No changes" && exit 0
          git commit -m "chore: sync editor to ${EDITOR_VERSION}"
          git push
          git tag "${EDITOR_VERSION}"
          git push origin "${EDITOR_VERSION}"
```

**Step 2: Commit**

```bash
git add .github/workflows/sync-editor.yml
git commit -m "ci: add sync-editor workflow for editor-release dispatch"
```

---

## Phase 5: Upstream CI Updates

### Task 20: Update workflow release.yml to dispatch to workflow-editor

**Files:**
- Modify: `workflow/.github/workflows/release.yml`

**Step 1: Add workflow-editor to the notify-ide-plugins matrix**

In `/Users/jon/workspace/workflow/.github/workflows/release.yml`, find the `notify-ide-plugins` job (around line 269) and add `workflow-editor` to the matrix:

```yaml
notify-ide-plugins:
  name: Notify IDE Plugins
  runs-on: ubuntu-latest
  needs: release
  if: ${{ !contains(inputs.tag_name || github.ref_name, '-') }}
  strategy:
    matrix:
      repo:
        - 'GoCodeAlone/workflow-vscode'
        - 'GoCodeAlone/workflow-jetbrains'
        - 'GoCodeAlone/workflow-editor'    # <-- ADD THIS
  steps:
  - name: Trigger update for ${{ matrix.repo }}
    uses: peter-evans/repository-dispatch@v4
    with:
      token: ${{ secrets.repo_dispatch_token }}
      repository: ${{ matrix.repo }}
      event-type: workflow-release
      client-payload: '{"version": "${{ env.TAG_NAME }}"}'
```

**Step 2: Commit**

```bash
cd /Users/jon/workspace/workflow
git add .github/workflows/release.yml
git commit -m "ci: dispatch workflow-release to workflow-editor"
```

---

### Task 21: Update workflow-plugin-admin to consume the editor package

**Files:**
- Modify: `workflow-plugin-admin/Makefile`
- Modify: `workflow-plugin-admin/.github/workflows/release.yml`
- Create: `workflow-plugin-admin/.github/workflows/sync-editor.yml`

**Step 1: Update Makefile**

The `make ui` target currently clones the workflow repo and builds UI from source. After `workflow/ui` consumes `@gocodealone/workflow-editor`, this target continues to work unchanged — it just builds the app that now imports the package.

No Makefile changes needed unless you want to add an explicit editor version pin.

**Step 2: Add sync-editor.yml**

```yaml
name: Sync Editor on Editor Release

on:
  repository_dispatch:
    types: [editor-release]

jobs:
  rebuild-ui:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-node@v4
        with:
          node-version: '22'
          registry-url: 'https://npm.pkg.github.com'
          scope: '@gocodealone'

      - name: Clone and build workflow UI with new editor
        run: |
          git clone --depth 1 https://github.com/GoCodeAlone/workflow.git /tmp/workflow-ui-build
          cd /tmp/workflow-ui-build/ui
          npm ci
          npx vite build
          cd -
          rm -rf internal/ui_dist
          cp -r /tmp/workflow-ui-build/ui/dist internal/ui_dist
          rm -rf /tmp/workflow-ui-build
        env:
          NODE_AUTH_TOKEN: ${{ secrets.GITHUB_TOKEN }}

      - name: Commit
        run: |
          EDITOR_VERSION="${{ github.event.client_payload.version }}"
          git config user.name "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"
          git add internal/ui_dist/
          git diff --cached --quiet && echo "No changes" && exit 0
          git commit -m "chore: rebuild UI with editor ${EDITOR_VERSION}"
          git push
```

**Step 3: Commit**

```bash
cd /Users/jon/workspace/workflow-plugin-admin
git add .github/workflows/sync-editor.yml
git commit -m "ci: add sync-editor workflow for editor-release dispatch"
```

---

## Phase 6: Schema-Aware Features & Polish

### Task 22: Content detection notification prompt (VS Code + JetBrains)

**Files:**
- Modify: `workflow-vscode/src/visual-editor.ts`
- Modify: `workflow-jetbrains/src/main/kotlin/com/gocodalone/workflow/ide/editor/WorkflowFileDetector.kt`

The design requires a non-intrusive notification when content detection matches (Layer 2). Currently the plan only has a boolean `isWorkflowFile()` — the prompt UX is missing.

**Step 1: VS Code — add notification prompt on content detection**

In `visual-editor.ts`, when a YAML file is opened that matches content detection (but NOT explicit `configPaths`), show an information message:

```ts
export function promptWorkflowDetection(document: vscode.TextDocument) {
  const configPaths: string[] = vscode.workspace.getConfiguration('workflow').get('configPaths', []);
  // Skip if already in explicit config paths
  if (isExplicitMatch(document, configPaths)) return;
  // Skip if user chose "Don't ask again"
  if (vscode.workspace.getConfiguration('workflow').get('suppressDetectionPrompt', false)) return;

  if (!isContentMatch(document)) return;

  vscode.window.showInformationMessage(
    'This looks like a Workflow config. Open the visual editor?',
    'Open Visual Editor',
    'Always for this file',
    "Don't ask again"
  ).then((choice) => {
    if (choice === 'Open Visual Editor') {
      vscode.commands.executeCommand('workflow.openVisualEditor');
    } else if (choice === 'Always for this file') {
      // Add to configPaths setting
      const relative = vscode.workspace.asRelativePath(document.uri);
      configPaths.push(relative);
      vscode.workspace.getConfiguration('workflow').update('configPaths', configPaths, vscode.ConfigurationTarget.Workspace);
      vscode.commands.executeCommand('workflow.openVisualEditor');
    } else if (choice === "Don't ask again") {
      vscode.workspace.getConfiguration('workflow').update('suppressDetectionPrompt', true, vscode.ConfigurationTarget.Workspace);
    }
  });
}
```

Register in `extension.ts` `activate()`:

```ts
vscode.workspace.onDidOpenTextDocument((doc) => {
  if (doc.languageId === 'yaml') promptWorkflowDetection(doc);
});
```

**Step 2: JetBrains — add EditorNotifications provider**

Create `WorkflowDetectionNotificationProvider.kt`:

```kotlin
class WorkflowDetectionNotificationProvider : EditorNotifications.Provider<EditorNotificationPanel>() {
    override fun createNotificationPanel(file: VirtualFile, editor: FileEditor, project: Project): EditorNotificationPanel? {
        if (WorkflowSettings.getInstance().suppressDetectionPrompt) return null
        if (isExplicitMatch(file, project)) return null
        if (!isContentMatch(file)) return null

        return EditorNotificationPanel(editor, EditorNotificationPanel.Status.Info).apply {
            text = "This looks like a Workflow config"
            createActionLabel("Open Visual Editor") {
                // trigger visual editor action
            }
            createActionLabel("Always for this file") {
                // add to configPaths
            }
            createActionLabel("Don't ask again") {
                WorkflowSettings.getInstance().suppressDetectionPrompt = true
                EditorNotifications.getInstance(project).updateAllNotifications()
            }
        }
    }
}
```

Register in `plugin.xml`:
```xml
<editorNotificationProvider implementation="com.gocodalone.workflow.ide.editor.WorkflowDetectionNotificationProvider"/>
```

**Step 3: Add `suppressDetectionPrompt` setting to both IDE plugins**

VS Code: add to `contributes.configuration`:
```json
"workflow.suppressDetectionPrompt": {
  "type": "boolean",
  "default": false,
  "description": "Don't show detection prompt for workflow YAML files"
}
```

JetBrains: add to `WorkflowSettings`:
```kotlin
var suppressDetectionPrompt: Boolean = false
```

**Step 4: Commit in each repo**

---

### Task 23: External plugin schema discovery

**Files:**
- Modify: `workflow-vscode/src/visual-editor.ts` (or new `src/plugin-discovery.ts`)
- Modify: `workflow-jetbrains/src/main/kotlin/com/gocodalone/workflow/ide/editor/WorkflowBridge.kt`
- Modify: `workflow-editor/src/stores/moduleSchemaStore.ts` (if needed)

The design specifies three-tier schema loading. Tier 1 (built-in) is covered. Tier 2 (installed plugins) and tier 3 (registry lookup) are missing.

**Step 1: Create plugin discovery module (shared logic)**

In both IDE plugins, add logic to:
1. Parse `go.mod` in the workspace root for `github.com/GoCodeAlone/workflow-plugin-*` imports
2. For each found plugin, fetch its manifest from `workflow-registry` (GitHub raw URL)
3. Extract `stepTypes`/`moduleTypes` with their JSON Schema definitions
4. Inject via `loadPluginSchemas()` on the editor's `moduleSchemaStore`

**VS Code (`src/plugin-discovery.ts`):**

```ts
import * as vscode from 'vscode';
import * as fs from 'fs';
import * as path from 'path';

const REGISTRY_BASE = 'https://raw.githubusercontent.com/GoCodeAlone/workflow-registry/main/plugins';

export async function discoverPluginSchemas(workspaceRoot: string): Promise<PluginSchemaData[]> {
  const goModPath = path.join(workspaceRoot, 'go.mod');
  if (!fs.existsSync(goModPath)) return [];

  const goMod = fs.readFileSync(goModPath, 'utf-8');
  const pluginImports = goMod.match(/github\.com\/GoCodeAlone\/workflow-plugin-(\w+)/g) || [];

  const schemas: PluginSchemaData[] = [];
  for (const imp of pluginImports) {
    const name = imp.split('workflow-plugin-')[1];
    try {
      const resp = await fetch(`${REGISTRY_BASE}/${name}/manifest.json`);
      if (!resp.ok) continue;
      const manifest = await resp.json();
      schemas.push({
        pluginName: manifest.name || name,
        pluginIcon: manifest.icon,
        pluginColor: manifest.color,
        modules: manifest.schemas || {},
      });
    } catch {
      // Skip unavailable manifests
    }
  }
  return schemas;
}
```

**Step 2: Wire into the editor bridge**

In `visual-editor.ts`, after sending built-in schemas, also send plugin schemas:

```ts
private async sendPluginSchemas() {
  const workspaceRoot = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
  if (!workspaceRoot) return;
  const plugins = await discoverPluginSchemas(workspaceRoot);
  this.panel?.webview.postMessage({ type: 'pluginSchemasLoaded', plugins });
}
```

**Step 3: Cache manifests locally**

Store fetched manifests in `context.globalStorageUri/plugin-manifests/` with a timestamp. Refresh on `workflow-release` dispatch or when user runs "Workflow: Refresh Plugin Schemas" command. Cache TTL: 24 hours.

**Step 4: JetBrains equivalent**

Same logic in Kotlin, using `HttpClient` to fetch manifests, caching in `PathManager.getPluginsPath()`.

**Step 5: Commit in each repo**

---

### Task 24: Plugin-aware palette grouping

**Files:**
- Modify: `workflow-editor/src/components/sidebar/NodePalette.tsx`

The design specifies visual distinction in the palette: built-in types grouped by category, plugin types grouped under plugin name with icon/color, private plugins only shown if detected in project dependencies.

**Step 1: Update NodePalette to group plugin types separately**

The `moduleSchemaStore` already merges plugin types via `loadPluginSchemas()`. Add a `pluginSource` field to track which types came from plugins vs built-in:

```tsx
// In NodePalette.tsx, after the existing category-based grouping:
const builtinTypes = moduleTypes.filter(t => !t.pluginSource);
const pluginGroups = new Map<string, ModuleTypeInfo[]>();
for (const t of moduleTypes.filter(t => t.pluginSource)) {
  const group = pluginGroups.get(t.pluginSource!) || [];
  group.push(t);
  pluginGroups.set(t.pluginSource!, group);
}
```

Render plugin groups after built-in categories with distinct styling (plugin name header, optional icon/color).

**Step 2: Add `pluginSource` field to ModuleTypeInfo**

In `types/workflow.ts`, add optional `pluginSource?: string` to `ModuleTypeInfo`.

In `moduleSchemaStore.ts`, `loadPluginSchemas()` sets `pluginSource` to the plugin name for each type it adds.

**Step 3: Test palette renders plugin groups**

**Step 4: Commit**

---

### Task 25: Node validation indicators

**Files:**
- Modify: `workflow-editor/src/components/nodes/BaseNode.tsx`
- Modify: `workflow-editor/src/stores/workflowStore.ts`

The design specifies validation indicators on nodes with invalid config.

**Step 1: Add validation to workflowStore**

Add a `validationErrors` map to the store: `Record<string, string[]>` keyed by node ID. Add a `validateNodes()` action that checks each node's config against its `configFields` schema:

```ts
validateNodes: () => {
  const { nodes } = get();
  const moduleTypeMap = useModuleSchemaStore.getState().moduleTypeMap;
  const errors: Record<string, string[]> = {};
  for (const node of nodes) {
    const info = moduleTypeMap[node.data.moduleType];
    if (!info?.configFields) continue;
    const nodeErrors: string[] = [];
    for (const field of info.configFields) {
      if (field.required && !node.data.config?.[field.key]) {
        nodeErrors.push(`Missing required field: ${field.label || field.key}`);
      }
    }
    if (nodeErrors.length > 0) errors[node.id] = nodeErrors;
  }
  set({ validationErrors: errors });
},
```

**Step 2: Display validation indicators on BaseNode**

In `BaseNode.tsx`, read validation errors from the store and render a warning badge:

```tsx
const errors = useWorkflowStore((s) => s.validationErrors[id]);
// ...
{errors && errors.length > 0 && (
  <div className="validation-badge" title={errors.join('\n')}>
    ⚠ {errors.length}
  </div>
)}
```

**Step 3: Trigger validation on config changes**

Call `validateNodes()` after `importFromConfig()`, `addNode()`, and config edits in `PropertyPanel`.

**Step 4: Test validation indicators**

**Step 5: Commit**

---

### Task 26: Cursor→node highlight (implement TODO)

**Files:**
- Modify: `workflow-editor/src/components/WorkflowEditor.tsx` (add `onCursorMoved` prop)
- Modify: `workflow-editor/src/stores/workflowStore.ts` (add `highlightedNodeId`)
- Modify: `workflow-editor/src/components/nodes/BaseNode.tsx` (render highlight)
- Modify: `workflow-vscode/webview-src/index.tsx` (wire `onCursorMoved`)
- Modify: `workflow-jetbrains/webview-src/index.tsx` (wire `onCursorMoved`)

**Step 1: Add `onCursorMoved` to WorkflowEditorProps**

```ts
onCursorMoved?: (line: number, col: number) => void;
```

**Step 2: Map YAML line → node ID**

When the host sends `cursorMoved(line, col)`, the editor needs to find which node corresponds to that YAML line. The `configToNodes` serialization already tracks `ui_position`, but we also need a `yamlLineMap: Record<string, { startLine: number; endLine: number }>` that maps node IDs to their YAML line ranges.

Add a `buildYamlLineMap(yaml: string)` utility that parses YAML with line tracking and returns the map.

**Step 3: Add `highlightedNodeId` to workflowStore**

When a cursor position maps to a node, set `highlightedNodeId`. BaseNode renders a highlight ring when its ID matches.

**Step 4: Wire in both IDE webview entry points**

Replace the TODO comments in `index.tsx` for both VS Code and JetBrains:

```ts
onCursorMoved: (line, col) => {
  // Find node at this line and highlight it
  const { yamlLineMap, setHighlightedNode } = useWorkflowStore.getState();
  for (const [nodeId, range] of Object.entries(yamlLineMap)) {
    if (line >= range.startLine && line <= range.endLine) {
      setHighlightedNode(nodeId);
      return;
    }
  }
  setHighlightedNode(null);
},
```

**Step 5: Commit in all three repos**

---

### Task 27: Testing — component tests and E2E

**Files:**
- Create: `workflow-editor/src/components/nodes/BaseNode.test.tsx`
- Create: `workflow-editor/src/components/sidebar/NodePalette.test.tsx`
- Create: `workflow-editor/e2e/editor.spec.ts` (Playwright)
- Modify: `workflow-editor/package.json` (add Playwright devDep)

**Step 1: Write component tests**

BaseNode test:
```tsx
import { render, screen } from '@testing-library/react';
import { ReactFlowProvider } from '@xyflow/react';
import { describe, it, expect } from 'vitest';

describe('BaseNode', () => {
  it('renders node label', () => { /* ... */ });
  it('shows validation badge when errors present', () => { /* ... */ });
  it('shows highlight ring when highlightedNodeId matches', () => { /* ... */ });
});
```

NodePalette test:
```tsx
describe('NodePalette', () => {
  it('renders built-in categories', () => { /* ... */ });
  it('renders plugin groups separately', () => { /* ... */ });
  it('filters by search text', () => { /* ... */ });
  it('sets drag data on drag start', () => { /* ... */ });
});
```

**Step 2: Write E2E Playwright test**

```ts
import { test, expect } from '@playwright/test';

test('editor loads YAML and renders nodes', async ({ page }) => {
  // Serve the workflow/ui app locally with the editor package
  await page.goto('http://localhost:5173');
  // Load a sample config
  // Verify nodes appear on the canvas
  // Add a node from palette
  // Verify YAML updates
});
```

**Step 3: Run tests**

```bash
npm test
npx playwright test
```

**Step 4: Commit**

---

## Task Summary

| # | Phase | Task | Repo |
|---|-------|------|------|
| 1 | 1 | Scaffold workflow-editor repo | workflow-editor |
| 2 | 1 | Extract TypeScript types | workflow-editor |
| 3 | 1 | Extract utility functions (fix serialization coupling) | workflow-editor |
| 4 | 1 | Extract Zustand stores (fix coupling points) | workflow-editor |
| 5 | 1 | Extract canvas components (fix saveWorkflowConfig) | workflow-editor |
| 6 | 1 | Extract node components | workflow-editor |
| 7 | 1 | Extract property panel and sub-editors | workflow-editor |
| 8 | 1 | Extract NodePalette and Toolbar (callback props) | workflow-editor |
| 9 | 1 | Create WorkflowEditor wrapper and public API | workflow-editor |
| 10 | 1 | Add CI workflows (publish, sync-schema, build) | workflow-editor |
| 11 | 1 | Write integration tests | workflow-editor |
| 12 | 1 | Initial publish v0.1.0 | workflow-editor |
| 13 | 2 | Replace workflow/ui inline code with package import | workflow |
| 14 | 3 | Add webview infrastructure to VS Code | workflow-vscode |
| 15 | 3 | Bundle editor for VS Code webview | workflow-vscode |
| 16 | 3 | Add sync-editor CI to VS Code | workflow-vscode |
| 17 | 4 | Add JCEF infrastructure to JetBrains | workflow-jetbrains |
| 18 | 4 | Bundle editor for JetBrains JCEF | workflow-jetbrains |
| 19 | 4 | Add sync-editor CI to JetBrains | workflow-jetbrains |
| 20 | 5 | Update workflow release.yml dispatch | workflow |
| 21 | 5 | Update workflow-plugin-admin CI | workflow-plugin-admin |
| 22 | 6 | Content detection notification prompt | workflow-vscode, workflow-jetbrains |
| 23 | 6 | External plugin schema discovery | workflow-vscode, workflow-jetbrains |
| 24 | 6 | Plugin-aware palette grouping | workflow-editor |
| 25 | 6 | Node validation indicators | workflow-editor |
| 26 | 6 | Cursor→node highlight | workflow-editor, workflow-vscode, workflow-jetbrains |
| 27 | 6 | Component tests and E2E | workflow-editor |
