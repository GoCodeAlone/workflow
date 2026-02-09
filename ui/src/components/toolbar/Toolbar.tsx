import useWorkflowStore from '../../store/workflowStore.ts';
import { configToYaml, parseYaml } from '../../utils/serialization.ts';

export default function Toolbar() {
  const exportToConfig = useWorkflowStore((s) => s.exportToConfig);
  const importFromConfig = useWorkflowStore((s) => s.importFromConfig);
  const clearCanvas = useWorkflowStore((s) => s.clearCanvas);
  const nodes = useWorkflowStore((s) => s.nodes);

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
      } catch (e) {
        console.error('Failed to import:', e);
        alert('Failed to parse workflow file');
      }
    };
    input.click();
  };

  const handleValidate = () => {
    const config = exportToConfig();
    const errors: string[] = [];

    if (config.modules.length === 0) {
      errors.push('Workflow has no modules');
    }

    const names = config.modules.map((m) => m.name);
    const dupes = names.filter((n, i) => names.indexOf(n) !== i);
    if (dupes.length > 0) {
      errors.push(`Duplicate module names: ${dupes.join(', ')}`);
    }

    for (const mod of config.modules) {
      if (!mod.name.trim()) errors.push(`Module of type ${mod.type} has no name`);
      if (mod.dependsOn) {
        for (const dep of mod.dependsOn) {
          if (!names.includes(dep)) {
            errors.push(`${mod.name} depends on unknown module: ${dep}`);
          }
        }
      }
    }

    if (errors.length === 0) {
      alert('Workflow is valid!');
    } else {
      alert('Validation errors:\n\n' + errors.join('\n'));
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
      <ToolbarButton label="Export YAML" onClick={handleExport} disabled={nodes.length === 0} />
      <ToolbarButton label="Validate" onClick={handleValidate} disabled={nodes.length === 0} />

      <div style={{ flex: 1 }} />

      <span style={{ color: '#585b70', fontSize: 11, marginRight: 8 }}>{nodes.length} modules</span>
      <ToolbarButton label="Clear" onClick={clearCanvas} disabled={nodes.length === 0} variant="danger" />
    </div>
  );
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
  variant?: 'danger';
}) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      style={{
        padding: '5px 12px',
        background: variant === 'danger' ? '#45475a' : '#313244',
        border: '1px solid #45475a',
        borderRadius: 4,
        color: disabled ? '#585b70' : variant === 'danger' ? '#f38ba8' : '#cdd6f4',
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
