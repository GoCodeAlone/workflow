import { useState } from 'react';
import useDocManagerStore from '../../store/docManagerStore.ts';
import DocList from './DocList.tsx';
import DocEditor from './DocEditor.tsx';
import DocViewer from './DocViewer.tsx';

type Mode = 'view' | 'edit' | 'new';

export default function DocsPage() {
  const selectedDoc = useDocManagerStore((s) => s.selectedDoc);
  const [mode, setMode] = useState<Mode>('view');

  const handleNew = () => {
    useDocManagerStore.getState().selectDoc(null);
    useDocManagerStore.getState().setEditorContent('');
    setMode('new');
  };

  const handleEdit = () => {
    setMode('edit');
  };

  const handleCancel = () => {
    setMode('view');
  };

  return (
    <div style={{ display: 'flex', height: '100%', background: '#1e1e2e', overflow: 'hidden' }}>
      {/* Left sidebar */}
      <DocList onNew={handleNew} />

      {/* Right content */}
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        {mode === 'new' ? (
          <DocEditor isNew onCancel={handleCancel} />
        ) : mode === 'edit' && selectedDoc ? (
          <DocEditor isNew={false} onCancel={handleCancel} />
        ) : selectedDoc ? (
          <DocViewer onEdit={handleEdit} />
        ) : (
          <div
            style={{
              flex: 1,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              flexDirection: 'column',
              gap: 12,
            }}
          >
            <div style={{ color: '#6c7086', fontSize: 16 }}>No document selected</div>
            <div style={{ color: '#585b70', fontSize: 13 }}>
              Select a document from the list or create a new one
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
