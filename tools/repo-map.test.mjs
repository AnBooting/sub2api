import { after, before, test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { copyFileSync, mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { randomUUID } from 'node:crypto';
import { tmpdir } from 'node:os';
import { resolve, join } from 'node:path';

const repoRoot = resolve(import.meta.dirname, '..');
let fixtureRoot;

function impact(root, query) {
  const result = spawnSync(process.execPath, [join(root, 'tools', 'repo-map.mjs'), 'impact', query], {
    cwd: root,
    encoding: 'utf8',
    windowsHide: true,
    timeout: 60_000,
    maxBuffer: 8 * 1024 * 1024,
  });
  assert.ifError(result.error);
  assert.equal(result.status, 0, result.stderr || result.stdout);
  const data = JSON.parse(result.stdout);
  assert.equal(data.command, 'impact');
  assert.equal(data.query, query);
  return data;
}

before(() => {
  fixtureRoot = mkdtempSync(join(tmpdir(), 'sub2-repo-map-'));
  mkdirSync(join(fixtureRoot, 'tools'));
  // Run the actual production CLI against a deterministic, isolated Git repository.
  copyFileSync(join(repoRoot, 'tools', 'repo-map.mjs'), join(fixtureRoot, 'tools', 'repo-map.mjs'));
  writeFileSync(join(fixtureRoot, 'needle.txt'), 'ordinary fixture content\n');
  writeFileSync(join(fixtureRoot, 'references.txt'), 'needle.txt is referenced here\n--safe-search is a literal flag\n');
  for (const args of [
    ['init', '-q'],
    ['add', 'needle.txt', 'references.txt'],
    ['-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', '-c', 'commit.gpgsign=false',
      '-c', 'core.hooksPath=' + join(fixtureRoot, 'no-hooks'), 'commit', '-qm', 'fixture'],
  ]) {
    const result = spawnSync('git', args, { cwd: fixtureRoot, encoding: 'utf8', windowsHide: true });
    assert.ifError(result.error);
    assert.equal(result.status, 0, result.stderr);
  }
});

after(() => {
  if (fixtureRoot) rmSync(fixtureRoot, { recursive: true, force: true });
});

test('impact searches a known production symbol through the actual CLI', () => {
  const data = impact(repoRoot, 'pricing_snapshot');
  assert.ok(data.matchCount > 0);
  assert.ok(data.matches.some((match) => match.text?.includes('pricing_snapshot')));
});

test('impact returns empty results when git grep has no matches', () => {
  const data = impact(fixtureRoot, 'absent-' + randomUUID());
  assert.equal(data.matchCount, 0);
  assert.deepEqual(data.matches, []);
  assert.deepEqual(data.files, []);
  assert.deepEqual(data.tests, []);
});

test('impact treats a leading hyphen as part of the literal search pattern', () => {
  const data = impact(fixtureRoot, '--safe-search');
  assert.equal(data.matchCount, 1);
  assert.equal(data.matches[0].text, '--safe-search is a literal flag');
});

test('impact searches a path-shaped pattern as text across files', () => {
  const data = impact(fixtureRoot, 'needle.txt');
  assert.equal(data.matchCount, 1);
  assert.deepEqual(data.files, ['references.txt']);
  assert.equal(data.matches[0].text, 'needle.txt is referenced here');
});
