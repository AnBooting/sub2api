#!/usr/bin/env node

import { createHash } from 'node:crypto';
import { existsSync, mkdirSync, readFileSync, renameSync, statSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { join, parse, relative, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { spawnSync } from 'node:child_process';

export const REPO_ROOT = resolve(import.meta.dirname, '..');
const NL = String.fromCharCode(10);
const NUL = String.fromCharCode(0);
const DEFAULT_CACHE_PARENT_NAME = 'dsh/repo-intelligence';

export function projectKey(root) {
  return createHash('sha256').update(resolve(root).toLowerCase()).digest('hex').slice(0, 16);
}

export function cacheRootFor(root, env = process.env) {
  const configured = env.SUB2_REPO_INTELLIGENCE_HOME;
  if (configured && configured.trim()) return resolve(configured);
  const localAppData = env.LOCALAPPDATA || join(homedir(), 'AppData', 'Local');
  return join(localAppData, DEFAULT_CACHE_PARENT_NAME, 'sub2-' + projectKey(root));
}

export function ensureExternalCache(root, cacheRoot) {
  const resolvedRoot = resolve(root);
  const resolvedCache = resolve(cacheRoot);
  // path.relative returns an ABSOLUTE path when the two paths are on different
  // Windows drives (e.g. root F:\sub2, cache C:\Users\...). That absolute path is
  // neither '' nor does it start with '..', so the old containment test wrongly
  // reported "cache is inside the source tree" and disabled the whole tool on any
  // machine whose cache root sits on another drive. Compare parsed paths instead:
  // the cache is inside the source only when root === cache or root is an ancestor.
  const parsedRoot = parse(resolvedRoot);
  const parsedCache = parse(resolvedCache);
  const sameDrive = parsedRoot.root.toLowerCase() === parsedCache.root.toLowerCase();
  const cacheInsideRoot = sameDrive && (() => {
    const rel = relative(resolvedRoot, resolvedCache);
    return rel === '' || (!rel.startsWith('..') && rel !== '..');
  })();
  if (cacheInsideRoot) {
    const error = new Error('Repository intelligence cache must be outside the source tree: ' + resolvedCache);
    error.code = 'CACHE_INSIDE_SOURCE';
    throw error;
  }
}

function run(command, args, cwd = REPO_ROOT, env = process.env) {
  const result = spawnSync(command, args, { cwd, env, encoding: 'utf8', windowsHide: true, maxBuffer: 64 * 1024 * 1024 });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    const error = new Error((result.stderr || result.stdout || ('Command failed: ' + command)).trim());
    error.code = 'COMMAND_FAILED';
    error.status = result.status;
    throw error;
  }
  return (result.stdout || '').trimEnd();
}

function git(root, args) {
  return run('git', args, root);
}

export function fingerprintFromParts(status, diff, untrackedMetadata = '') {
  return createHash('sha256').update([status, diff, untrackedMetadata].join(NL)).digest('hex');
}

export function readRepoState(root = REPO_ROOT) {
  const status = git(root, ['status', '--porcelain=v1', '-z']);
  const diff = git(root, ['diff', '--binary', 'HEAD', '--']);
  const untrackedFiles = git(root, ['ls-files', '--others', '--exclude-standard', '-z']).split(NUL).filter(Boolean);
  const untrackedMetadata = untrackedFiles.map((file) => {
    try {
      const stat = statSync(resolve(root, file));
      return file + NUL + stat.size + NUL + stat.mtimeMs;
    } catch {
      return file + NUL + 'missing';
    }
  }).join(NL);
  const entries = status ? status.split(NUL).filter(Boolean) : [];
  const dirtyFingerprint = fingerprintFromParts(status, diff, untrackedMetadata);
  return {
    sourceRoot: resolve(root),
    branch: git(root, ['branch', '--show-current']),
    head: git(root, ['rev-parse', 'HEAD']),
    dirty: entries.length > 0,
    dirtyFingerprint,
    statusEntries: entries,
    fingerprintBasis: 'git status, binary diff from HEAD, and untracked file size/mtime; untracked contents are not read',
  };
}

export function readManifest(cacheRoot) {
  const path = join(cacheRoot, 'repo-intelligence.manifest.json');
  if (!existsSync(path)) return null;
  try {
    return JSON.parse(readFileSync(path, 'utf8'));
  } catch (error) {
    const wrapped = new Error('Index manifest is unreadable or corrupt: ' + path);
    wrapped.code = 'MANIFEST_CORRUPT';
    wrapped.next = ['Run node tools/repo-intelligence.mjs index --force.'];
    wrapped.cause = error;
    throw wrapped;
  }
}

export function manifestStatus(manifest, state) {
  if (!manifest) return { status: 'missing', ready: false, reason: 'No index manifest exists.' };
  if (!manifest.sourceRoot) return { status: 'invalid-manifest', ready: false, reason: 'Index manifest has no sourceRoot.' };
  if (manifest.database && !existsSync(manifest.database)) return { status: 'database-missing', ready: false, reason: 'Index database referenced by manifest does not exist.' };
  if (resolve(manifest.sourceRoot) !== resolve(state.sourceRoot)) return { status: 'source-mismatch', ready: false, reason: 'Index belongs to another source root.' };
  if (manifest.head !== state.head) return { status: 'stale-commit', ready: false, reason: 'Index commit differs from current HEAD.' };
  if (state.dirty && !manifest.worktreeDirty) return { status: 'dirty-worktree', ready: false, reason: 'Current worktree has changes that are absent from the clean index.' };
  if (state.dirty && manifest.dirtyFingerprint !== state.dirtyFingerprint) return { status: 'dirty-state-changed', ready: false, reason: 'Worktree changes differ from the indexed dirty state.' };
  if (!state.dirty && manifest.worktreeDirty) return { status: 'worktree-cleaned', ready: false, reason: 'Index was built with uncommitted changes that are no longer present.' };
  return { status: state.dirty ? 'commit-current-dirty-index' : 'commit-current-clean-index', ready: true, reason: null };
}

const READ_ONLY_QUERY_COMMANDS = new Set(['alerts', 'ask', 'batch-search', 'coupling', 'dead-code', 'deps', 'diagnose', 'diagnose-issue', 'file-info', 'grep', 'health', 'impact', 'index-stats', 'metrics', 'preflight', 'search-symbol', 'taint', 'understand', 'uses', 'context', 'freshness', 'affected-tests', 'complete', 'disambiguate', 'syntax-check', 'timeline', 'test-impact']);

export function isReadOnlyQuery(args) {
  return args.length > 0 && READ_ONLY_QUERY_COMMANDS.has(args[0]);
}

export function extractJsonEnvelope(output) {
  const text = String(output || '').trim();
  for (let index = text.indexOf('{'); index >= 0; index = text.indexOf('{', index + 1)) {
    try {
      const value = JSON.parse(text.slice(index));
      if (value && typeof value === 'object') return value;
    } catch {}
  }
  return null;
}

function roamCommand(env = process.env) {
  const configured = env.ROAM_BIN;
  if (configured && configured.trim()) {
    const path = resolve(configured);
    if (!existsSync(path)) {
      const error = new Error('ROAM_BIN does not exist: ' + path);
      error.code = 'ROAM_NOT_FOUND';
      throw error;
    }
    return path;
  }
  return process.platform === 'win32' ? 'roam.exe' : 'roam';
}

function roamEnv(cacheRoot, env = process.env) {
  return {
    ...env,
    ROAM_DB_DIR: join(cacheRoot, 'roam-db'),
    ROAM_NO_AUTO_INDEX: '1',
    ROAM_MCP_PRESET: 'core',
    ROAM_AI_ENABLED: '0',
  };
}

export function buildManifest(state, cacheRoot, index, elapsedMs, roamVersion = null) {
  return {
    schemaVersion: 1,
    sourceRoot: state.sourceRoot,
    projectKey: projectKey(state.sourceRoot),
    branch: state.branch,
    head: state.head,
    worktreeDirty: state.dirty,
    dirtyFingerprint: state.dirtyFingerprint,
    fingerprintBasis: state.fingerprintBasis,
    generatedAt: new Date().toISOString(),
    indexer: { name: 'roam-code', version: roamVersion, command: 'roam index' },
    cacheRoot: resolve(cacheRoot),
    database: join(resolve(cacheRoot), 'roam-db', 'index.db'),
    elapsedMs,
    summary: {
      files: index?.files ?? index?.corpus?.files ?? null,
      symbols: index?.symbols ?? index?.corpus?.symbols ?? null,
      edges: index?.edges ?? index?.corpus?.edges ?? null,
      parseCoveragePct: index?.parse_coverage_pct ?? null,
      elapsedSeconds: index?.elapsed_s ?? null,
    },
  };
}

function writeManifest(cacheRoot, manifest) {
  mkdirSync(cacheRoot, { recursive: true });
  const path = join(cacheRoot, 'repo-intelligence.manifest.json');
  const temp = path + '.tmp-' + process.pid;
  writeFileSync(temp, JSON.stringify(manifest, null, 2) + NL, 'utf8');
  renameSync(temp, path);
}

function parseCli(argv) {
  const args = [...argv];
  const command = args.shift() || 'help';
  const options = { allowDirty: false, force: false };
  const rest = [];
  for (const arg of args) {
    if (arg === '--allow-dirty') options.allowDirty = true;
    else if (arg === '--force') options.force = true;
    else if (arg === '--') continue;
    else rest.push(arg);
  }
  return { command, options, rest };
}

export { parseCli };

function currentContext() {
  const state = readRepoState(REPO_ROOT);
  const cacheRoot = cacheRootFor(REPO_ROOT);
  ensureExternalCache(REPO_ROOT, cacheRoot);
  const manifest = readManifest(cacheRoot);
  return { state, cacheRoot, manifest, freshness: manifestStatus(manifest, state) };
}

function requireReady(context, allowDirty) {
  const { freshness, state } = context;
  if (freshness.ready && (!state.dirty || allowDirty)) return;
  const error = new Error(freshness.reason || 'Repository intelligence index is not ready.');
  error.code = freshness.status.toUpperCase().replaceAll('-', '_');
  error.freshness = freshness;
  error.next = [
    'Run node tools/repo-intelligence.mjs status.',
    'Run node tools/repo-intelligence.mjs index --allow-dirty when indexing current uncommitted work is intentional.',
    'Use node tools/repo-map.mjs impact <term> as the no-index fallback.',
  ];
  throw error;
}

function roamVersion(binary, env) {
  const result = spawnSync(binary, ['--version'], { env, encoding: 'utf8', windowsHide: true });
  return result.status === 0 ? (result.stdout || '').trim() : null;
}

function indexCommand(context, options) {
  if (context.state.dirty && !options.allowDirty) {
    const error = new Error('Refusing to index a dirty worktree without --allow-dirty.');
    error.code = 'DIRTY_WORKTREE';
    throw error;
  }
  const binary = roamCommand(process.env);
  const env = roamEnv(context.cacheRoot, process.env);
  mkdirSync(join(context.cacheRoot, 'roam-db'), { recursive: true });
  const args = ['index', '--json', '--quiet'];
  if (options.force) args.push('--force');
  const started = Date.now();
  const result = spawnSync(binary, args, { cwd: REPO_ROOT, env, encoding: 'utf8', windowsHide: true, maxBuffer: 64 * 1024 * 1024 });
  const elapsedMs = Date.now() - started;
  if (result.error) throw result.error;
  if (result.status !== 0) {
    const error = new Error((result.stderr || result.stdout || 'Roam index failed').trim());
    error.code = 'ROAM_INDEX_FAILED';
    error.status = result.status;
    throw error;
  }
  const index = extractJsonEnvelope(result.stdout);
  const state = readRepoState(REPO_ROOT);
  if (state.head !== context.state.head || state.dirtyFingerprint !== context.state.dirtyFingerprint) {
    const error = new Error('Source changed while the index was being built; the result was not recorded as fresh.');
    error.code = 'SOURCE_CHANGED_DURING_INDEX';
    throw error;
  }
  const manifest = buildManifest(state, context.cacheRoot, index, elapsedMs, roamVersion(binary, env));
  writeManifest(context.cacheRoot, manifest);
  return { command: 'index', freshness: manifestStatus(manifest, state), manifest, indexOutput: index, stderr: result.stderr || '' };
}

function queryCommand(context, options, rest) {
  requireReady(context, options.allowDirty);
  if (rest.length === 0) throw new Error('Usage: query [--allow-dirty] <roam-command> [args...]');
  if (!isReadOnlyQuery(rest)) {
    const error = new Error('Query command is not in the read-only allowlist: ' + rest[0]);
    error.code = 'QUERY_NOT_READ_ONLY';
    throw error;
  }
  const binary = roamCommand(process.env);
  const env = roamEnv(context.cacheRoot, process.env);
  const result = spawnSync(binary, rest, { cwd: REPO_ROOT, env, encoding: 'utf8', windowsHide: true, maxBuffer: 64 * 1024 * 1024 });
  if (result.error) throw result.error;
  return { command: 'query', query: rest, freshness: context.freshness, exitCode: result.status ?? 1, stdout: result.stdout || '', stderr: result.stderr || '' };
}

function mcpCommand(context, options, rest) {
  requireReady(context, options.allowDirty);
  const binary = roamCommand(process.env);
  const env = roamEnv(context.cacheRoot, process.env);
  const args = ['mcp', '--no-auto-index', ...rest];
  const result = spawnSync(binary, args, { cwd: REPO_ROOT, env, stdio: 'inherit', windowsHide: true });
  if (result.error) throw result.error;
  return { command: 'mcp', exitCode: result.status ?? 1 };
}

function statusCommand(context) {
  return { command: 'status', source: context.state, cacheRoot: context.cacheRoot, manifest: context.manifest, freshness: context.freshness, fallback: 'node tools/repo-map.mjs' };
}

function help() {
  return {
    commands: {
      status: 'Show source commit, dirty state, cache path, manifest and freshness.',
      index: 'Build the external Roam index; requires --allow-dirty for uncommitted work.',
      query: 'Run a read-only Roam query after freshness validation.',
      mcp: 'Start Roam core MCP with automatic indexing disabled.',
      fallback: 'Use the lexical/structural repo-map fallback without Roam.',
    },
    examples: [
      'node tools/repo-intelligence.mjs status',
      'node tools/repo-intelligence.mjs index',
      'node tools/repo-intelligence.mjs index --allow-dirty',
      'node tools/repo-intelligence.mjs query --allow-dirty context ValidateNativePricingRequest',
      'node tools/repo-intelligence.mjs mcp --allow-dirty',
      'node tools/repo-map.mjs impact pricing_snapshot',
    ],
    environment: {
      SUB2_REPO_INTELLIGENCE_HOME: 'External cache root; defaults to LOCALAPPDATA/dsh/repo-intelligence/sub2-<project-key>.',
      ROAM_BIN: 'Absolute path to the approved roam executable; required when roam is not on PATH.',
    },
  };
}

export function main(argv = process.argv.slice(2)) {
  const parsed = parseCli(argv);
  try {
    const context = currentContext();
    let result;
    if (parsed.command === 'status') result = statusCommand(context);
    else if (parsed.command === 'index') result = indexCommand(context, parsed.options);
    else if (parsed.command === 'query') result = queryCommand(context, parsed.options, parsed.rest);
    else if (parsed.command === 'mcp') result = mcpCommand(context, parsed.options, parsed.rest);
    else result = help();
    process.stdout.write(JSON.stringify(result, null, 2) + NL);
    return typeof result?.exitCode === 'number' ? result.exitCode : 0;
  } catch (error) {
    const output = { error: error.code || 'REPOSITORY_INTELLIGENCE_FAILED', message: error.message, freshness: error.freshness, next: error.next };
    process.stderr.write(JSON.stringify(output, null, 2) + NL);
    return 2;
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) process.exitCode = main();
