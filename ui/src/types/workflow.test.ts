import { describe, it, expect } from 'vitest';
import {
  MODULE_TYPES,
  MODULE_TYPE_MAP,
  CATEGORIES,
  CATEGORY_COLORS,
} from './workflow.ts';
import type { ModuleCategory } from './workflow.ts';

describe('workflow types', () => {
  describe('MODULE_TYPES', () => {
    it('has the expected number of module types', () => {
      expect(MODULE_TYPES.length).toBe(30);
    });

    it('each module type has required fields', () => {
      for (const mod of MODULE_TYPES) {
        expect(mod.type).toBeTruthy();
        expect(typeof mod.type).toBe('string');

        expect(mod.label).toBeTruthy();
        expect(typeof mod.label).toBe('string');

        expect(mod.category).toBeTruthy();
        expect(typeof mod.category).toBe('string');

        expect(mod.defaultConfig).toBeDefined();
        expect(typeof mod.defaultConfig).toBe('object');

        expect(Array.isArray(mod.configFields)).toBe(true);
      }
    });

    it('all types are unique', () => {
      const types = MODULE_TYPES.map((m) => m.type);
      expect(new Set(types).size).toBe(types.length);
    });

    it('all module types belong to a valid category', () => {
      const categoryKeys = CATEGORIES.map((c) => c.key);
      for (const mod of MODULE_TYPES) {
        expect(categoryKeys).toContain(mod.category);
      }
    });

    it('configFields have correct structure', () => {
      for (const mod of MODULE_TYPES) {
        for (const field of mod.configFields) {
          expect(field.key).toBeTruthy();
          expect(field.label).toBeTruthy();
          expect(['string', 'number', 'boolean', 'select', 'json']).toContain(field.type);

          if (field.type === 'select') {
            expect(Array.isArray(field.options)).toBe(true);
            expect(field.options!.length).toBeGreaterThan(0);
          }
        }
      }
    });
  });

  describe('MODULE_TYPE_MAP', () => {
    it('covers all types from MODULE_TYPES', () => {
      for (const mod of MODULE_TYPES) {
        expect(MODULE_TYPE_MAP[mod.type]).toBeDefined();
        expect(MODULE_TYPE_MAP[mod.type]).toBe(mod);
      }
    });

    it('has same number of entries as MODULE_TYPES', () => {
      expect(Object.keys(MODULE_TYPE_MAP).length).toBe(MODULE_TYPES.length);
    });

    it('returns correct info for a known type', () => {
      const server = MODULE_TYPE_MAP['http.server'];
      expect(server.label).toBe('HTTP Server');
      expect(server.category).toBe('http');
      expect(server.defaultConfig).toEqual({ address: ':8080' });
    });
  });

  describe('CATEGORIES', () => {
    it('has all 10 expected categories', () => {
      expect(CATEGORIES).toHaveLength(10);
    });

    it('contains all expected category keys', () => {
      const keys = CATEGORIES.map((c) => c.key);
      expect(keys).toContain('http');
      expect(keys).toContain('middleware');
      expect(keys).toContain('messaging');
      expect(keys).toContain('statemachine');
      expect(keys).toContain('events');
      expect(keys).toContain('integration');
      expect(keys).toContain('scheduling');
      expect(keys).toContain('infrastructure');
      expect(keys).toContain('database');
      expect(keys).toContain('observability');
    });

    it('each category has a key and label', () => {
      for (const cat of CATEGORIES) {
        expect(cat.key).toBeTruthy();
        expect(cat.label).toBeTruthy();
      }
    });

    it('each category key has a color', () => {
      for (const cat of CATEGORIES) {
        expect(CATEGORY_COLORS[cat.key as ModuleCategory]).toBeTruthy();
      }
    });
  });

  describe('CATEGORY_COLORS', () => {
    it('has all 10 category colors', () => {
      expect(Object.keys(CATEGORY_COLORS)).toHaveLength(10);
    });

    it('all values are hex color strings', () => {
      for (const color of Object.values(CATEGORY_COLORS)) {
        expect(color).toMatch(/^#[0-9a-f]{6}$/i);
      }
    });
  });
});
