# UI/UX Improvement Plan

## Feature 1: Enhanced Delegate Blocks

**Problem:** Delegate step blocks on the canvas show nothing about what they delegate to — no service name, no route info. Users can't tell at a glance what a delegate does.

**Solution:** Show delegate target info directly on the canvas node and in the route step list.

### Changes:

**A. BaseNode.tsx — Show delegate target in preview**
- When `moduleType === 'step.delegate'`, read `node.data.config.service` (the delegate target name)
- Display it as the preview text: e.g. `→ admin-queries`
- If the config also has a `handler` or `route` field, show that too

**B. RoutePipelineEditor.tsx — Show delegate target inline**
- In the step list items (line 180-246), when `step.type === 'delegate'`, show the delegate service name from `step.config?.service` next to the step name
- Style: dim color, monospace, e.g. `Delegate → admin-queries`

**C. DelegateServicePicker.tsx — Add service descriptions**
- Extend `ServiceInfo` in `moduleSchemaStore.ts` to include `description` and `moduleType` fields
- Update `fetchServices()` to request these from the API (if available)
- In the picker dropdown, show `svc.type` and `svc.description` (if available) below the service name in smaller text
- Group services by category or type so users can find relevant ones

### Files to modify:
- `ui/src/components/nodes/BaseNode.tsx` — add delegate preview
- `ui/src/components/properties/RoutePipelineEditor.tsx` — show delegate target
- `ui/src/components/properties/DelegateServicePicker.tsx` — add descriptions
- `ui/src/store/moduleSchemaStore.ts` — extend ServiceInfo type

---

## Feature 2: SQL Textarea with Syntax Highlighting

**Problem:** The `step.db_exec` "query" field renders as a single-line `<input type="text">`, making SQL unreadable. No syntax highlighting.

**Solution:** Create a new `sql` field type that renders as a styled textarea with basic SQL keyword highlighting.

### Changes:

**A. New component: `SqlEditor.tsx`**
Create `ui/src/components/properties/SqlEditor.tsx`:
- A `<textarea>` with monospace font, adequate rows (6-8 default), resizable
- An overlay or preview div that shows SQL keywords highlighted (SELECT, INSERT, UPDATE, DELETE, FROM, WHERE, SET, VALUES, INTO, JOIN, ON, AND, OR, NOT, ORDER BY, GROUP BY, LIMIT, AS, CREATE, ALTER, DROP, TABLE, INDEX)
- Implementation approach: Use a layered approach — transparent textarea on top, highlighted div behind (same technique as CodeMirror-lite / Prism.js inline). Keep it simple — just keyword coloring, no full parser.
- SQL keywords: bold + blue (#89b4fa)
- String literals ('...'): green (#a6e3a1)
- Numbers: peach (#fab387)
- Template expressions ({{ ... }}): purple (#cba6f7) with distinct background
- Comments (--): dim gray (#585b70)

**B. PropertyPanel.tsx — Add `sql` field type**
- In the field rendering chain (line 242-336), add a new branch for `field.type === 'sql'`
- Render `<SqlEditor value={...} onChange={...} />`

**C. moduleSchemaStore.ts — Map server type**
- In `mapFieldType()`, add `case 'sql': return 'sql';`

**D. workflow.ts types — Add `sql` to ConfigFieldDef type**
- Add `'sql'` to the union type

**E. Backend: schema/module_schema.go — Mark query fields as `sql` type**
- For `step.db_exec` and `step.db_query`, change the `query` field type from `"string"` to `"sql"`

### Files to modify:
- `ui/src/components/properties/SqlEditor.tsx` — NEW
- `ui/src/components/properties/PropertyPanel.tsx` — add sql branch
- `ui/src/store/moduleSchemaStore.ts` — map sql type
- `ui/src/types/workflow.ts` — add sql to type union
- `module/schema/module_schema.go` (Go backend) — change query field type to sql

---

## Feature 3: Pipeline Step Scope Context & Field Picker

**Problem:** Template expressions like `{{index .steps "parse-request" "path_params" "id"}}` are opaque. Users don't know:
- What objects/entities are available in the current scope
- What fields those objects have
- What types those fields are

**Solution:** Build a scope-aware field picker that shows available data at each point in the pipeline.

### Changes:

**A. Define step output schemas**
Each step type produces known output shapes. Create a mapping:

```typescript
// StepOutputSchema: what each step type makes available to subsequent steps
const STEP_OUTPUT_SCHEMAS: Record<string, StepOutputField[]> = {
  'step.request_parse': [
    { path: 'path_params', type: 'map', children: 'dynamic' },  // from route path params
    { path: 'query_params', type: 'map', children: 'dynamic' },
    { path: 'body', type: 'object', children: 'dynamic' },
    { path: 'headers', type: 'map' },
    { path: 'method', type: 'string' },
  ],
  'step.set': [
    // children come from config.values keys
  ],
  'step.db_query': [
    { path: 'row', type: 'object', description: 'Single result (mode: single)' },
    { path: 'rows', type: 'array', description: 'All results (mode: list)' },
    { path: 'found', type: 'boolean', description: 'Whether any rows matched' },
  ],
  'step.db_exec': [
    { path: 'rows_affected', type: 'number' },
    { path: 'last_insert_id', type: 'number' },
  ],
  // ... etc for each step type
};
```

**B. New component: `FieldPicker.tsx`**
Create `ui/src/components/properties/FieldPicker.tsx`:
- Takes: `pipelineSteps` (array of preceding steps), `currentStepIndex`, `onSelect` callback
- Shows a small button (📋 or field icon) next to template-capable input fields
- On click, opens a dropdown/popover showing:
  - **Steps scope**: Each preceding step listed by name, expandable to show its output fields
  - **Request scope**: `path_params`, `query_params`, `body`, `headers`
  - Each field shows: name, type (string/number/object/array), and a "copy" action
- When user clicks a field, it inserts the correct template expression at cursor position
- For simple fields: `{{ .steps.STEP_NAME.FIELD }}`
- For nested fields: `{{index .steps "STEP_NAME" "FIELD" "SUBFIELD"}}`

**C. Integrate FieldPicker into PropertyPanel**
- For string/sql fields in pipeline step nodes, show the FieldPicker button next to the input
- Detect if the current node is a pipeline step by checking `moduleType.startsWith('step.')`
- Find preceding steps from the pipeline chain (using edges with `edgeType === 'pipeline-flow'`)

**D. RoutePipelineEditor.tsx — Add field picker to inline step config**
- In the inline step config textarea, add a FieldPicker that knows about preceding steps in the same pipeline
- When a field is selected, insert the template expression at cursor position in the textarea

### Files to modify:
- `ui/src/components/properties/FieldPicker.tsx` — NEW
- `ui/src/components/properties/PropertyPanel.tsx` — integrate picker
- `ui/src/components/properties/RoutePipelineEditor.tsx` — integrate picker for inline steps

---

## Feature 4: Improved Route Step Visualization

**Problem:** Steps within a route are shown as generic blocks with connecting lines. Hard to see the full pipeline at a glance. No visual distinction between middleware, starter blocks, and ending blocks.

**Solution:** Redesign the route step list in RoutePipelineEditor to use an interlocking/stacked visual with role-based styling.

### Changes:

**A. Step role classification**
Classify each step type into a visual role:
```typescript
const STEP_ROLES: Record<string, 'start' | 'middleware' | 'transform' | 'action' | 'end'> = {
  'step.request_parse': 'start',
  'step.validate': 'middleware',
  'step.conditional': 'middleware',
  'step.set': 'transform',
  'step.transform': 'transform',
  'step.log': 'middleware',
  'step.db_query': 'action',
  'step.db_exec': 'action',
  'step.http_call': 'action',
  'step.delegate': 'action',
  'step.publish': 'action',
  'step.json_response': 'end',
};
```

**B. Redesign RoutePipelineEditor step items**
Replace the current flat list with interlocking stepped blocks:
- Each step is a card with a notch at the top (receiving) and a tab at the bottom (connecting to next)
- Steps overlap slightly to create the "puzzle piece" interlocking effect
- Color coding by role:
  - **Start** (request_parse): green left border (#a6e3a1)
  - **Middleware** (validate, conditional, log): blue left border (#89b4fa)
  - **Transform** (set, transform): orange left border (#fab387)
  - **Action** (db_query, db_exec, delegate, http_call): purple left border (#cba6f7)
  - **End** (json_response): red left border (#f38ba8)
- Each card shows:
  - Role icon/badge on left (▶ start, ◆ middleware, ⟳ transform, ⚡ action, ■ end)
  - Step name (bold)
  - Step type (dimmed)
  - Key config preview (e.g. for db_exec: first 40 chars of query; for delegate: service name)
  - Drag handle on right for reordering (replace up/down arrows with drag)
- A vertical connector line runs down the left side, with dots at each step, reinforcing the flow

**C. Visual connector between steps**
Between each step card, show a small connector:
- A downward arrow or notch graphic
- Slightly overlapping the cards above and below
- Color matches the flow (gradient from preceding step's role color to next step's role color)

### Files to modify:
- `ui/src/components/properties/RoutePipelineEditor.tsx` — full redesign of step rendering

---

## Implementation Order

These features are largely independent and can be implemented in parallel:
1. **Feature 2 (SQL Editor)** — standalone new component + minor wiring
2. **Feature 1 (Delegate Enhancement)** — touches BaseNode, RoutePipelineEditor, DelegateServicePicker
3. **Feature 4 (Route Step Visualization)** — full redesign of RoutePipelineEditor
4. **Feature 3 (Field Picker)** — new component + integration into PropertyPanel and RoutePipelineEditor

Dependencies:
- Feature 4 should be done before Feature 3's RoutePipelineEditor integration (since Feature 4 redesigns that component)
- Features 1 and 2 are fully independent
