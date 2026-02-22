import { describe, it, expect } from 'vitest';
import {
  MODULE_TYPE_MAP,
  MODULE_TYPES,
  CATEGORIES,
  CATEGORY_COLORS,
  layoutNodes,
  nodeTypes,
} from '../index.ts';

describe('@gocodealoneorg/workflow-ui exports', () => {
  it('exports MODULE_TYPE_MAP', () => {
    expect(MODULE_TYPE_MAP).toBeDefined();
    expect(typeof MODULE_TYPE_MAP).toBe('object');
  });

  it('exports MODULE_TYPES array', () => {
    expect(Array.isArray(MODULE_TYPES)).toBe(true);
    expect(MODULE_TYPES.length).toBeGreaterThan(0);
  });

  it('exports CATEGORIES', () => {
    expect(Array.isArray(CATEGORIES)).toBe(true);
    expect(CATEGORIES.length).toBeGreaterThan(0);
  });

  it('exports CATEGORY_COLORS', () => {
    expect(CATEGORY_COLORS).toBeDefined();
    expect(typeof CATEGORY_COLORS).toBe('object');
  });

  it('exports nodeTypes registry', () => {
    expect(nodeTypes).toBeDefined();
    expect(typeof nodeTypes).toBe('object');
    expect('httpNode' in nodeTypes).toBe(true);
    expect('schedulerNode' in nodeTypes).toBe(true);
    expect('stateMachineNode' in nodeTypes).toBe(true);
  });

  it('exports layoutNodes utility', () => {
    expect(typeof layoutNodes).toBe('function');
  });
});
