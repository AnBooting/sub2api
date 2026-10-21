import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { resolve, join } from 'node:path';

const repoRoot = resolve(process.env.SUB2API_REVIEW_ROOT || process.cwd());

test('PR #5: actual repo-map impact CLI searches a known production symbol', () => {
  const result = spawnSync(process.execPath, [
    join(repoRoot, 'tools', 'repo-map.mjs'), 'impact', 'pricing_snapshot',
  ], {
    cwd: repoRoot,
    encoding: 'utf8',
    windowsHide: true,
    timeout: 60_000,
    maxBuffer: 8 * 1024 * 1024,
  });

  assert.ifError(result.error);
  assert.equal(result.status, 0, result.stderr || result.stdout);
  const data = JSON.parse(result.stdout);
  assert.equal(data.command, 'impact');
  assert.equal(data.query, 'pricing_snapshot');
  assert.ok(data.matchCount > 0, 'Known production symbol must yield matches');
  assert.ok(data.matches.some((match) => match.text?.includes('pricing_snapshot')),
    'At least one result must contain the requested symbol');
});
