import { type ReactNode, useCallback, useRef, useEffect, useState } from 'react';

interface CollapsiblePanelProps {
  collapsed: boolean;
  onToggle: () => void;
  side: 'left' | 'right';
  panelName: string;
  width: number;
  onResize?: (width: number) => void;
  minWidth?: number;
  maxWidth?: number;
  children: ReactNode;
}

export default function CollapsiblePanel({
  collapsed,
  onToggle,
  side,
  panelName,
  width,
  onResize,
  minWidth = 100,
  maxWidth = 600,
  children,
}: CollapsiblePanelProps) {
  const borderSide = side === 'left' ? 'borderRight' : 'borderLeft';
  const animatedWidth = collapsed ? 0 : width;

  const [isResizing, setIsResizing] = useState(false);
  const [hoverToggle, setHoverToggle] = useState(false);
  const [hoverResize, setHoverResize] = useState(false);
  const startXRef = useRef(0);
  const startWidthRef = useRef(0);

  const handleResizeStart = useCallback(
    (e: React.MouseEvent) => {
      if (collapsed || !onResize) return;
      e.preventDefault();
      setIsResizing(true);
      startXRef.current = e.clientX;
      startWidthRef.current = width;
    },
    [collapsed, onResize, width],
  );

  useEffect(() => {
    if (!isResizing) return;

    const handleMouseMove = (e: MouseEvent) => {
      const delta = side === 'left'
        ? e.clientX - startXRef.current
        : startXRef.current - e.clientX;
      const newWidth = Math.max(minWidth, Math.min(maxWidth, startWidthRef.current + delta));
      onResize?.(newWidth);
    };

    const handleMouseUp = () => {
      setIsResizing(false);
    };

    document.addEventListener('mousemove', handleMouseMove);
    document.addEventListener('mouseup', handleMouseUp);
    // Prevent text selection while resizing
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';

    return () => {
      document.removeEventListener('mousemove', handleMouseMove);
      document.removeEventListener('mouseup', handleMouseUp);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };
  }, [isResizing, onResize, side, minWidth, maxWidth]);

  const tooltipText = collapsed ? `Expand ${panelName}` : `Collapse ${panelName}`;

  // Grip dots indicator (3 vertical dots)
  const gripDots = (
    <span
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 3,
        alignItems: 'center',
      }}
    >
      {[0, 1, 2].map((i) => (
        <span
          key={i}
          style={{
            width: 4,
            height: 4,
            borderRadius: '50%',
            background: hoverToggle ? '#cdd6f4' : '#585b70',
            transition: 'background 0.15s',
          }}
        />
      ))}
    </span>
  );

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: side === 'left' ? 'row' : 'row-reverse',
        height: '100%',
        position: 'relative',
      }}
    >
      {/* Panel content */}
      <div
        style={{
          width: animatedWidth,
          minWidth: 0,
          overflow: 'hidden',
          transition: isResizing ? 'none' : 'width 0.2s ease',
          [borderSide]: collapsed ? 'none' : '1px solid #313244',
        }}
      >
        <div style={{ width, minWidth: width, height: '100%' }}>
          {children}
        </div>
      </div>

      {/* Resize handle - between panel content and toggle button */}
      {!collapsed && onResize && (
        <div
          onMouseDown={handleResizeStart}
          onMouseEnter={() => setHoverResize(true)}
          onMouseLeave={() => setHoverResize(false)}
          style={{
            width: 5,
            minWidth: 5,
            cursor: 'col-resize',
            background: hoverResize || isResizing ? '#45475a' : 'transparent',
            transition: 'background 0.15s',
            position: 'relative',
            zIndex: 2,
          }}
          title="Drag to resize"
        />
      )}

      {/* Collapse/expand toggle button */}
      <button
        onClick={onToggle}
        title={tooltipText}
        onMouseEnter={() => setHoverToggle(true)}
        onMouseLeave={() => setHoverToggle(false)}
        style={{
          width: 16,
          minWidth: 16,
          background: hoverToggle ? '#313244' : '#1e1e2e',
          border: 'none',
          [borderSide]: '1px solid #313244',
          color: hoverToggle ? '#cdd6f4' : '#585b70',
          cursor: 'pointer',
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          gap: 8,
          fontSize: 12,
          padding: 0,
          transition: 'background 0.15s, color 0.15s',
        }}
      >
        {gripDots}
        <span
          style={{
            display: 'inline-block',
            transition: 'transform 0.2s ease',
            transform: getChevronRotation(side, collapsed),
            fontSize: 8,
          }}
        >
          &#9654;
        </span>
      </button>
    </div>
  );
}

function getChevronRotation(side: 'left' | 'right', collapsed: boolean): string {
  if (side === 'left') {
    return collapsed ? 'rotate(0deg)' : 'rotate(180deg)';
  }
  return collapsed ? 'rotate(180deg)' : 'rotate(0deg)';
}
