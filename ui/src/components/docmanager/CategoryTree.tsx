import { useState } from 'react';
import type { CategoryTreeNode } from '../../store/docManagerStore.ts';
import type { Doc } from '../../store/docManagerStore.ts';

function countDocsInCategory(docs: Doc[], fullPath: string): number {
  return docs.filter(
    (d) => d.category === fullPath || d.category.startsWith(fullPath + '/'),
  ).length;
}

function TreeNode({
  node,
  docs,
  selectedCategory,
  onSelect,
  depth,
}: {
  node: CategoryTreeNode;
  docs: Doc[];
  selectedCategory: string;
  onSelect: (fullPath: string) => void;
  depth: number;
}) {
  const [expanded, setExpanded] = useState(true);
  const hasChildren = node.children.length > 0;
  const isActive = selectedCategory === node.fullPath;
  const count = countDocsInCategory(docs, node.fullPath);

  return (
    <div>
      <div
        onClick={() => onSelect(node.fullPath)}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 4,
          padding: '4px 8px',
          paddingLeft: 8 + depth * 14,
          cursor: 'pointer',
          background: isActive ? '#313244' : 'transparent',
          borderLeft: isActive ? '2px solid #89b4fa' : '2px solid transparent',
          fontSize: 12,
          color: isActive ? '#cdd6f4' : '#a6adc8',
          fontWeight: isActive ? 600 : 400,
          transition: 'background 0.1s',
        }}
        onMouseEnter={(e) => {
          if (!isActive)
            (e.currentTarget as HTMLDivElement).style.background = '#1e1e2e';
        }}
        onMouseLeave={(e) => {
          if (!isActive)
            (e.currentTarget as HTMLDivElement).style.background = 'transparent';
        }}
      >
        {hasChildren && (
          <span
            onClick={(e) => {
              e.stopPropagation();
              setExpanded(!expanded);
            }}
            style={{
              display: 'inline-flex',
              width: 14,
              justifyContent: 'center',
              color: '#6c7086',
              fontSize: 10,
              flexShrink: 0,
              cursor: 'pointer',
            }}
          >
            {expanded ? '\u25BC' : '\u25B6'}
          </span>
        )}
        {!hasChildren && <span style={{ width: 14, flexShrink: 0 }} />}
        <span
          style={{
            flex: 1,
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {node.name}
        </span>
        {count > 0 && (
          <span
            style={{
              color: '#6c7086',
              fontSize: 10,
              flexShrink: 0,
            }}
          >
            {count}
          </span>
        )}
      </div>
      {hasChildren && expanded && (
        <div>
          {node.children.map((child) => (
            <TreeNode
              key={child.fullPath}
              node={child}
              docs={docs}
              selectedCategory={selectedCategory}
              onSelect={onSelect}
              depth={depth + 1}
            />
          ))}
        </div>
      )}
    </div>
  );
}

export default function CategoryTree({
  tree,
  docs,
  selectedCategory,
  onSelect,
}: {
  tree: CategoryTreeNode[];
  docs: Doc[];
  selectedCategory: string;
  onSelect: (fullPath: string) => void;
}) {
  return (
    <div style={{ fontSize: 12 }}>
      {/* All categories option */}
      <div
        onClick={() => onSelect('')}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 4,
          padding: '4px 8px',
          cursor: 'pointer',
          background: selectedCategory === '' ? '#313244' : 'transparent',
          borderLeft:
            selectedCategory === ''
              ? '2px solid #89b4fa'
              : '2px solid transparent',
          color: selectedCategory === '' ? '#cdd6f4' : '#a6adc8',
          fontWeight: selectedCategory === '' ? 600 : 400,
          transition: 'background 0.1s',
        }}
        onMouseEnter={(e) => {
          if (selectedCategory !== '')
            (e.currentTarget as HTMLDivElement).style.background = '#1e1e2e';
        }}
        onMouseLeave={(e) => {
          if (selectedCategory !== '')
            (e.currentTarget as HTMLDivElement).style.background = 'transparent';
        }}
      >
        <span style={{ width: 14, flexShrink: 0 }} />
        <span style={{ flex: 1 }}>All categories</span>
        <span style={{ color: '#6c7086', fontSize: 10 }}>{docs.length}</span>
      </div>

      {tree.map((node) => (
        <TreeNode
          key={node.fullPath}
          node={node}
          docs={docs}
          selectedCategory={selectedCategory}
          onSelect={onSelect}
          depth={0}
        />
      ))}
    </div>
  );
}
