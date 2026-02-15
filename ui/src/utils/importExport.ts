/**
 * Import/Export utilities for workflow configurations.
 *
 * Export creates a zip containing:
 *   - workflow.yaml (the workflow YAML config)
 *   - workspace/   (referenced workspace files, if any)
 *
 * Import extracts both, restoring workspace files via the workspace API.
 */

/**
 * Export a workflow as a YAML string download.
 * If workspaceFiles is provided, they are bundled into a zip.
 */
export async function exportWorkflow(
  yaml: string,
  filename: string = 'workflow.yaml',
): Promise<void> {
  const blob = new Blob([yaml], { type: 'text/yaml' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

/**
 * Export a workflow with workspace files as a zip bundle.
 * Requires the project ID to fetch workspace files from the API.
 */
export async function exportWorkflowBundle(
  yaml: string,
  projectId: string,
  filename: string = 'workflow-bundle.zip',
): Promise<void> {
  const token = localStorage.getItem('auth_token');
  const headers: Record<string, string> = {};
  if (token) headers['Authorization'] = `Bearer ${token}`;

  // Fetch the list of workspace files
  const res = await fetch(`/api/v1/workspaces/${projectId}/files`, { headers });
  const files: Array<{ name: string; path: string; isDir: boolean }> = res.ok ? await res.json() : [];

  // If no workspace files, just download the YAML
  if (!files || files.length === 0 || files.every((f) => f.isDir)) {
    return exportWorkflow(yaml, filename.replace('.zip', '.yaml'));
  }

  // Use JSZip if available (must be installed), otherwise fall back to YAML-only
  try {
    const JSZip = (await import('jszip')).default;
    const zip = new JSZip();

    // Add the YAML config
    zip.file('workflow.yaml', yaml);

    // Add workspace files
    const workspaceFolder = zip.folder('workspace');
    for (const file of files) {
      if (file.isDir) continue;
      const fileRes = await fetch(`/api/v1/workspaces/${projectId}/files/${file.path}`, { headers });
      if (fileRes.ok) {
        const blob = await fileRes.blob();
        workspaceFolder?.file(file.path, blob);
      }
    }

    const blob = await zip.generateAsync({ type: 'blob' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    a.click();
    URL.revokeObjectURL(url);
  } catch {
    // JSZip not available, fall back to YAML-only export
    return exportWorkflow(yaml, filename.replace('.zip', '.yaml'));
  }
}

/**
 * Import a workflow YAML file. Returns the file content as a string.
 */
export function importWorkflowFile(): Promise<string> {
  return new Promise((resolve, reject) => {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.yaml,.yml,.zip';
    input.onchange = async () => {
      const file = input.files?.[0];
      if (!file) {
        reject(new Error('No file selected'));
        return;
      }

      if (file.name.endsWith('.zip')) {
        try {
          const JSZip = (await import('jszip')).default;
          const zip = await JSZip.loadAsync(file);
          const yamlFile = zip.file('workflow.yaml');
          if (!yamlFile) {
            reject(new Error('No workflow.yaml found in zip'));
            return;
          }
          const yamlContent = await yamlFile.async('string');
          resolve(yamlContent);
        } catch (e) {
          reject(new Error(`Failed to read zip: ${e}`));
        }
      } else {
        const text = await file.text();
        resolve(text);
      }
    };
    input.click();
  });
}

/**
 * Import a workflow bundle (zip with workspace files).
 * Extracts workspace files and uploads them via the workspace API.
 */
export async function importWorkflowBundle(
  projectId: string,
): Promise<{ yaml: string; filesUploaded: number }> {
  return new Promise((resolve, reject) => {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.yaml,.yml,.zip';
    input.onchange = async () => {
      const file = input.files?.[0];
      if (!file) {
        reject(new Error('No file selected'));
        return;
      }

      const token = localStorage.getItem('auth_token');
      const headers: Record<string, string> = {};
      if (token) headers['Authorization'] = `Bearer ${token}`;

      if (file.name.endsWith('.zip')) {
        try {
          const JSZip = (await import('jszip')).default;
          const zip = await JSZip.loadAsync(file);

          // Extract YAML
          const yamlFile = zip.file('workflow.yaml');
          if (!yamlFile) {
            reject(new Error('No workflow.yaml found in zip'));
            return;
          }
          const yaml = await yamlFile.async('string');

          // Upload workspace files
          let filesUploaded = 0;
          const workspaceFolder = zip.folder('workspace');
          if (workspaceFolder) {
            const filePromises: Promise<void>[] = [];
            workspaceFolder.forEach((relativePath, zipEntry) => {
              if (zipEntry.dir) return;
              const p = zipEntry.async('blob').then(async (blob) => {
                const form = new FormData();
                form.append('file', blob, relativePath);
                form.append('path', relativePath);
                await fetch(`/api/v1/workspaces/${projectId}/files`, {
                  method: 'POST',
                  headers,
                  body: form,
                });
                filesUploaded++;
              });
              filePromises.push(p);
            });
            await Promise.all(filePromises);
          }

          resolve({ yaml, filesUploaded });
        } catch (e) {
          reject(new Error(`Failed to read zip: ${e}`));
        }
      } else {
        const yaml = await file.text();
        resolve({ yaml, filesUploaded: 0 });
      }
    };
    input.click();
  });
}
