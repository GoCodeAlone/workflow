// Bounded regression for GHSA-mrhx-c7xc-m3w5. Run: node --test scripts/test-zod-size-bounds.cjs
// An explicit module path permits testing the identical assertions against a historical package.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { z } = require(process.env.ZOD_TEST_MODULE || 'zod/v4');

for (const kind of ['array', 'map', 'set']) {
  const input = (values) => kind === 'array' ? values : kind === 'set' ? new Set(values) : new Map(values.map((v) => [v, v]));
  const schema = (element) => kind === 'array' ? z.array(element).max(2) : kind === 'set' ? z.set(element).check(z.maxSize(2)) : z.map(z.number(), element).check(z.maxSize(2));
  test(`${kind}: oversize rejects before element traversal`, () => {
    let visited = 0;
    const element = z.preprocess((v) => { visited++; return v; }, z.number());
    const result = schema(element).safeParse(input([1, 2, 3]));
    assert.equal(result.success, false);
    assert.equal(result.error.issues[0].code, 'too_big');
    assert.equal(visited, 0);
  });
  test(`${kind}: exact boundary is parsed and nested path retained`, () => {
    let visited = 0;
    const element = z.preprocess((v) => { visited++; return v; }, z.number());
    assert.equal(schema(element).safeParse(input([1, 2])).success, true);
    assert.equal(visited, 2);
    const invalid = z.object({ items: schema(z.number()) }).safeParse({ items: input([1, 2, 3]) });
    assert.equal(invalid.success, false);
    assert.deepEqual(invalid.error.issues[0].path, ['items']);
  });
}
test('leading rewrite may reduce size before later maximum', () => {
  const schema = z.array(z.number()).overwrite((v) => v.slice(0, 1)).max(1);
  assert.deepEqual(schema.parse([1, 2]), [1]);
});
test('maximum before rewrite rejects and does not invoke rewrite', () => {
  let rewritten = false;
  const schema = z.array(z.number()).max(1).overwrite((v) => { rewritten = true; return v.slice(0, 1); });
  assert.equal(schema.safeParse([1, 2]).success, false);
  assert.equal(rewritten, false);
});
test('valid-sized invalid element still receives its nested issue path', () => {
  const result = z.object({ items: z.array(z.number()).max(2) }).safeParse({ items: [1, 'bad'] });
  assert.equal(result.success, false);
  assert.deepEqual(result.error.issues[0].path, ['items', 1]);
});
