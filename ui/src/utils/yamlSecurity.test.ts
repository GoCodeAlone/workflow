import { describe, it, expect } from 'vitest';
import { load } from 'js-yaml';
import { parseYaml } from '@gocodealone/workflow-editor/utils';

const defaultMergeBudget = 10_000;

function mergeInput(mergeSources: number): string {
  // Empty maps exercise merge-work accounting without expanding output keys.
  const mappings = Array.from({ length: mergeSources }, (_, index) =>
    `  m${index}: {<<: *empty}`,
  ).join('\n');
  return `base: &empty {}
modules: []
workflows: {}
triggers: {}
payload:
${mappings}
`;
}

describe.each([
  ['js-yaml.load', load],
  ['workflow-editor.parseYaml', parseYaml],
] as const)('%s YAML security', (_name, parse) => {
  it.each([
    ['tenant-alpha', 'resource-first'],
    ['tenant-beta', 'resource-second'],
  ])('parses a valid config for %s/%s', (tenant, resource) => {
    const moduleName = `${tenant}-${resource}`;
    const input = `modules:
  - name: ${moduleName}
    type: http.server
    config:
      address: '127.0.0.1:0'
workflows: {}
triggers: {}
`;

    expect(parse(input)).toEqual({
      modules: [{
        name: moduleName,
        type: 'http.server',
        config: { address: '127.0.0.1:0' },
      }],
      workflows: {},
      triggers: {},
    });
  });

  it('rejects malformed syntax', () => {
    expect(() => parse('modules: [\n')).toThrow(/unexpected end|end of the stream/i);
  });

  it('accepts empty-map merges at the real default budget', () => {
    expect(parse(mergeInput(defaultMergeBudget))).toMatchObject({
      modules: [],
      workflows: {},
      triggers: {},
    });
  });

  it('rejects hostile merge work above the real default budget', () => {
    const hostile = mergeInput(defaultMergeBudget + 1);
    expect(hostile.length).toBeLessThan(256_000);
    expect(() => parse(hostile)).toThrow(/merge keys exceeded maxTotalMergeKeys \(10000\)/);
  });
});
