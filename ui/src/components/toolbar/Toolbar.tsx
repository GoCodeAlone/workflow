import useWorkflowStore from '../../store/workflowStore.ts';
import { configToYaml, parseYaml } from '../../utils/serialization.ts';
import {
  saveWorkflowConfig,
  getWorkflowConfig,
  validateWorkflow,
} from '../../utils/api.ts';

export default function Toolbar() {
  const exportToConfig = useWorkflowStore((s) => s.exportToConfig);
  const importFromConfig = useWorkflowStore((s) => s.importFromConfig);
  const clearCanvas = useWorkflowStore((s) => s.clearCanvas);
  const nodes = useWorkflowStore((s) => s.nodes);
  const addToast = useWorkflowStore((s) => s.addToast);
  const undo = useWorkflowStore((s) => s.undo);
  const redo = useWorkflowStore((s) => s.redo);
  const undoStack = useWorkflowStore((s) => s.undoStack);
  const redoStack = useWorkflowStore((s) => s.redoStack);
  const toggleAIPanel = useWorkflowStore((s) => s.toggleAIPanel);
  const showAIPanel = useWorkflowStore((s) => s.showAIPanel);
  const toggleComponentBrowser = useWorkflowStore((s) => s.toggleComponentBrowser);
  const showComponentBrowser = useWorkflowStore((s) => s.showComponentBrowser);

  const handleExport = () => {
    const config = exportToConfig();
    const yaml = configToYaml(config);
    const blob = new Blob([yaml], { type: 'text/yaml' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'workflow.yaml';
    a.click();
    URL.revokeObjectURL(url);
  };

  const handleImport = () => {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.yaml,.yml,.json';
    input.onchange = async () => {
      const file = input.files?.[0];
      if (!file) return;
      const text = await file.text();
      try {
        if (file.name.endsWith('.json')) {
          const config = JSON.parse(text);
          importFromConfig(config);
        } else {
          const config = parseYaml(text);
          importFromConfig(config);
        }
        addToast('Workflow imported from file', 'success');
      } catch (e) {
        console.error('Failed to import:', e);
        addToast('Failed to parse workflow file', 'error');
      }
    };
    input.click();
  };

  const handleLoadFromServer = async () => {
    try {
      const config = await getWorkflowConfig();
      importFromConfig(config);
      addToast('Workflow loaded from server', 'success');
    } catch (e) {
      addToast(`Failed to load: ${(e as Error).message}`, 'error');
    }
  };

  const handleSave = async () => {
    const config = exportToConfig();
    try {
      await saveWorkflowConfig(config);
      addToast('Workflow saved to server', 'success');
    } catch (e) {
      addToast(`Save failed: ${(e as Error).message}`, 'error');
    }
  };

  const handleValidate = async () => {
    const config = exportToConfig();

    // Client-side validation first
    const localErrors: string[] = [];
    if (config.modules.length === 0) {
      localErrors.push('Workflow has no modules');
    }
    const names = config.modules.map((m) => m.name);
    const dupes = names.filter((n, i) => names.indexOf(n) !== i);
    if (dupes.length > 0) {
      localErrors.push(`Duplicate module names: ${dupes.join(', ')}`);
    }
    for (const mod of config.modules) {
      if (!mod.name.trim()) localErrors.push(`Module of type ${mod.type} has no name`);
      if (mod.dependsOn) {
        for (const dep of mod.dependsOn) {
          if (!names.includes(dep)) {
            localErrors.push(`${mod.name} depends on unknown module: ${dep}`);
          }
        }
      }
    }

    if (localErrors.length > 0) {
      for (const err of localErrors) {
        addToast(err, 'error');
      }
      return;
    }

    // Try server validation
    try {
      const result = await validateWorkflow(config);
      if (result.valid) {
        addToast('Workflow is valid', 'success');
      } else {
        for (const err of result.errors) {
          addToast(err, 'error');
        }
        for (const warn of result.warnings) {
          addToast(warn, 'warning');
        }
      }
    } catch {
      // Server not available, use local result
      addToast('Workflow is valid (local check only)', 'info');
    }
  };

  return (
    <div
      style={{
        height: 44,
        background: '#181825',
        borderBottom: '1px solid #313244',
        display: 'flex',
        alignItems: 'center',
        padding: '0 16px',
        gap: 8,
      }}
    >
      <span style={{ fontWeight: 700, fontSize: 14, color: '#cdd6f4', marginRight: 16 }}>
        Workflow Editor
      </span>

      <ToolbarButton label="Import" onClick={handleImport} />
      <ToolbarButton label="Load Server" onClick={handleLoadFromServer} />
      <ToolbarButton label="Export YAML" onClick={handleExport} disabled={nodes.length === 0} />
      <ToolbarButton label="Save" onClick={handleSave} disabled={nodes.length === 0} />
      <ToolbarButton label="Validate" onClick={handleValidate} disabled={nodes.length === 0} />

      <Separator />

      <ToolbarButton label="Undo" onClick={undo} disabled={undoStack.length === 0} />
      <ToolbarButton label="Redo" onClick={redo} disabled={redoStack.length === 0} />

      <Separator />

      <ToolbarButton
        label="AI Copilot"
        onClick={toggleAIPanel}
        variant={showAIPanel ? 'active' : undefined}
      />
      <ToolbarButton
        label="Components"
        onClick={toggleComponentBrowser}
        variant={showComponentBrowser ? 'active' : undefined}
      />

      <div style={{ flex: 1 }} />

      <span style={{ color: '#585b70', fontSize: 11, marginRight: 8 }}>{nodes.length} modules</span>
      <ToolbarButton label="Clear" onClick={clearCanvas} disabled={nodes.length === 0} variant="danger" />
    </div>
  );
}

function Separator() {
  return <div style={{ width: 1, height: 20, background: '#313244', margin: '0 4px' }} />;
}

function ToolbarButton({
  label,
  onClick,
  disabled,
  variant,
}: {
  label: string;
  onClick: () => void;
  disabled?: boolean;
  variant?: 'danger' | 'active';
}) {
  const color = disabled
    ? '#585b70'
    : variant === 'danger'
    ? '#f38ba8'
    : variant === 'active'
    ? '#89b4fa'
    : '#cdd6f4';

  return (
    <button
      onClick={onClick}
      disabled={disabled}
      style={{
        padding: '5px 12px',
        background: variant === 'active' ? '#313244' : variant === 'danger' ? '#45475a' : '#313244',
        border: `1px solid ${variant === 'active' ? '#89b4fa' : '#45475a'}`,
        borderRadius: 4,
        color,
        fontSize: 12,
        cursor: disabled ? 'default' : 'pointer',
        fontWeight: 500,
        opacity: disabled ? 0.5 : 1,
      }}
    >
      {label}
    </button>
  );
}
