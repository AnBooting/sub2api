import test from 'node:test';
import assert from 'node:assert/strict';
import { buildManifest, cacheRootFor, ensureExternalCache, extractJsonEnvelope, fingerprintFromParts, isReadOnlyQuery, manifestStatus, parseCli, projectKey } from './repo-intelligence.mjs';

test('cache root is stable and external to source', () => {
  const root = 'F:/sub2';
  const cache = cacheRootFor(root, { LOCALAPPDATA: 'C:/Users/test/AppData/Local' });
  assert.match(projectKey(root), /^[0-9a-f]{16}$/);
  assert.equal(cache.replaceAll('\\', '/'), 'C:/Users/test/AppData/Local/dsh/repo-intelligence/sub2-' + projectKey(root));
});

// Regression: on Windows, path.relative between two different drives returns an
// ABSOLUTE path (e.g. root F:\sub2, cache C:\Users\...), which is neither '' nor does
// it start with '..'. The old containment test therefore concluded the cache was
// inside the source tree and rejected every run on such a machine, disabling the
// whole tool. This test exercises the guard directly, which the previous suite never
// did (it only checked cacheRootFor, so the bug shipped with 9/9 green).
test('cache on another Windows drive is accepted as external', () => {
  assert.doesNotThrow(() => ensureExternalCache('F:/sub2', 'C:/Users/test/AppData/Local/dsh/repo-intelligence/sub2-abc'));
  // Same drive, cache beside the source: still external.
  assert.doesNotThrow(() => ensureExternalCache('F:/sub2', 'F:/cache/sub2-abc'));
});

test('cache inside the source tree is rejected', () => {
  // The guard must keep failing closed for a genuinely internal cache.
  assert.throws(() => ensureExternalCache('F:/sub2', 'F:/sub2/.cache'), (error) => error.code === 'CACHE_INSIDE_SOURCE');
  assert.throws(() => ensureExternalCache('F:/sub2', 'F:/sub2'), (error) => error.code === 'CACHE_INSIDE_SOURCE');
  assert.throws(() => ensureExternalCache('F:/sub2', 'F:/sub2/tools/.cache'), (error) => error.code === 'CACHE_INSIDE_SOURCE');
});

test('CLI parses dirty and passthrough query arguments', () => {
  assert.deepEqual(parseCli(['query', '--allow-dirty', '--', 'context', 'Foo']), { command: 'query', options: { allowDirty: true, force: false }, rest: ['context', 'Foo'] });
});

test('freshness distinguishes clean, stale and dirty states', () => {
  const clean = { sourceRoot: 'F:/sub2', head: 'abc', dirty: false, dirtyFingerprint: '0' };
  const manifest = { sourceRoot: 'F:/sub2', head: 'abc', worktreeDirty: false, dirtyFingerprint: '0' };
  assert.equal(manifestStatus(manifest, clean).ready, true);
  assert.equal(manifestStatus(manifest, { ...clean, head: 'def' }).status, 'stale-commit');
  assert.equal(manifestStatus(manifest, { ...clean, dirty: true, dirtyFingerprint: '1' }).status, 'dirty-worktree');
});

test('dirty index requires the same worktree fingerprint', () => {
  const manifest = { sourceRoot: 'F:/sub2', head: 'abc', worktreeDirty: true, dirtyFingerprint: '1' };
  const state = { sourceRoot: 'F:/sub2', head: 'abc', dirty: true, dirtyFingerprint: '1' };
  assert.equal(manifestStatus(manifest, state).status, 'commit-current-dirty-index');
  assert.equal(manifestStatus(manifest, { ...state, dirtyFingerprint: '2' }).status, 'dirty-state-changed');
});

test('index manifest records the source and Roam summary', () => {
  const state = { sourceRoot: 'F:/sub2', branch: 'main', head: 'abc', dirty: false, dirtyFingerprint: '0', fingerprintBasis: 'test' };
  const manifest = buildManifest(state, 'C:/cache', { files: 10, symbols: 20, edges: 30, parse_coverage_pct: 94 }, 1234, '14.1.0');
  assert.equal(manifest.database.replaceAll('\\', '/'), 'C:/cache/roam-db/index.db');
  assert.deepEqual(manifest.summary, { files: 10, symbols: 20, edges: 30, parseCoveragePct: 94, elapsedSeconds: null });
});


test('fingerprint changes when tracked diff content changes', () => {
  const first = fingerprintFromParts(' M file.go', 'patch-a', '');
  const second = fingerprintFromParts(' M file.go', 'patch-b', '');
  assert.notEqual(first, second);
});

test('query allowlist admits read-only commands and rejects mutators', () => {
  assert.equal(isReadOnlyQuery(['context', 'Foo']), true);
  assert.equal(isReadOnlyQuery(['grep', 'pricing_snapshot']), true);
  assert.equal(isReadOnlyQuery(['index']), false);
  assert.equal(isReadOnlyQuery(['mcp']), false);
});

test('freshness rejects a missing database referenced by the manifest', () => {
  const manifest = { sourceRoot: 'F:/sub2', database: 'C:/__missing_repo_intelligence_db__/index.db', head: 'abc', worktreeDirty: false, dirtyFingerprint: '0' };
  const state = { sourceRoot: 'F:/sub2', head: 'abc', dirty: false, dirtyFingerprint: '0' };
  assert.equal(manifestStatus(manifest, state).status, 'database-missing');
});

test('JSON envelope extraction ignores log prefixes', () => {
  assert.deepEqual(extractJsonEnvelope('warning\n{"summary":{"files":3}}\n'), { summary: { files: 3 } });
  assert.equal(extractJsonEnvelope('not json'), null);
});
