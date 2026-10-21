#!/usr/bin/env node

import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';

const root = resolve(import.meta.dirname, '..');
const cacheDir = resolve(root, '.codegraph');
const manifestPath = resolve(cacheDir, 'manifest.json');
const overviewPath = resolve(cacheDir, 'overview.json');
const NL = String.fromCharCode(10);
const NUL = String.fromCharCode(0);
const BACKSLASH = String.fromCharCode(92);

function run(command, args, cwd = root) {
  return execFileSync(command, args, { cwd, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] }).trimEnd();
}

function git(args) {
  return run('git', args);
}

function state() {
  const status = git(['status', '--short']);
  return {
    branch: git(['branch', '--show-current']),
    head: git(['rev-parse', 'HEAD']),
    dirty: status.length > 0,
    statusEntries: status ? status.split(NL) : [],
  };
}

function files() {
  return git(['ls-files', '-z']).split(NUL).filter(Boolean);
}

function endsWithAny(file, suffixes) {
  return suffixes.some((suffix) => file.endsWith(suffix));
}

function kind(file) {
  if (endsWithAny(file, ['_test.go', '.spec.ts', '.spec.js', '.test.ts', '.test.js'])) return 'test';
  if (file.startsWith('backend/') && file.endsWith('.go')) return 'backend-go';
  if (file.startsWith('frontend/') && file.endsWith('.vue')) return 'frontend-vue';
  if (file.startsWith('frontend/') && endsWithAny(file, ['.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs'])) return 'frontend-js-ts';
  if (file.startsWith('backend/migrations/') && file.endsWith('.sql')) return 'migration';
  if (file.startsWith('project-docs/')) return 'project-docs';
  if (file.endsWith('.md')) return 'documentation';
  return 'other';
}

function count(values, selector) {
  const result = {};
  for (const value of values) {
    const key = selector(value);
    result[key] = (result[key] || 0) + 1;
  }
  return Object.fromEntries(Object.entries(result).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0])));
}

function goPackages() {
  const candidates = process.env.GO_BIN ? [process.env.GO_BIN] : ['go'];
  for (const executable of candidates) {
    try {
      return run(executable, ['list', './...'], resolve(root, 'backend')).split(NL).filter(Boolean);
    } catch (error) {
      if (process.env.GO_BIN) throw error;
    }
  }
  return [];
}

function baseOverview() {
  const current = state();
  const tracked = files();
  const packages = goPackages();
  return {
    schemaVersion: 1,
    kind: 'sub2-repository-overview',
    generatedFor: current.head,
    branch: current.branch,
    generatedAt: new Date().toISOString(),
    freshness: current.dirty ? 'commit-current-working-tree-dirty' : 'commit-current',
    authority: 'source-tests-compiler-runtime',
    indexLimit: 'lexical-and-structural-navigation; not a complete semantic call graph',
    counts: { trackedFiles: tracked.length, byKind: count(tracked, kind), byTopLevel: count(tracked, (file) => file.split('/')[0]), goPackages: packages.length },
    goPackages: packages,
    entrypoints: {
      backend: ['backend/cmd/server', 'backend/internal/server/routes', 'backend/internal/handler', 'backend/internal/service', 'backend/internal/repository', 'backend/ent/schema', 'backend/migrations'],
      frontend: ['frontend/src', 'frontend/src/api', 'frontend/src/router', 'frontend/src/stores', 'frontend/src/views'],
      deployment: ['deploy/docker-compose.yml', 'deploy/Dockerfile'],
      documentation: ['AGENTS.md', 'project-docs/00-overview.md', 'project-docs/architecture/README.md'],
    },
    untracked: current.statusEntries.filter((entry) => entry.startsWith('?? ')).map((entry) => entry.slice(3)),
  };
}

function freshness() {
  const current = state();
  if (!existsSync(manifestPath)) return { status: 'missing', currentHead: current.head, dirty: current.dirty };
  const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'));
  return { status: manifest.generatedFor === current.head ? (current.dirty ? 'commit-current-working-tree-dirty' : 'current') : 'stale-commit', generatedFor: manifest.generatedFor, currentHead: current.head, dirty: current.dirty, statusEntries: current.statusEntries };
}

function refresh() {
  const result = baseOverview();
  mkdirSync(cacheDir, { recursive: true });
  writeFileSync(overviewPath, JSON.stringify(result, null, 2) + NL, 'utf8');
  writeFileSync(manifestPath, JSON.stringify({ schemaVersion: 1, generatedFor: result.generatedFor, generatedAt: result.generatedAt, files: ['overview.json'] }, null, 2) + NL, 'utf8');
  return { command: 'refresh', cache: '.codegraph', generatedFor: result.generatedFor, counts: result.counts, freshness: result.freshness };
}

function parseMatches(output) {
  if (!output) return [];
  return output.split(NL).map((line) => {
    const first = line.indexOf(':');
    const second = first < 0 ? -1 : line.indexOf(':', first + 1);
    if (first <= 0 || second <= first) return { raw: line };
    return { file: line.slice(0, first), line: Number(line.slice(first + 1, second)), text: line.slice(second + 1) };
  });
}

function impact(query) {
  const current = state();
  let output = '';
  try {
    output = git(['grep', '-n', '-I', '-F', '-e', query, '--']);
  } catch (error) {
    if (error.status !== 1) throw error;
  }
  const matches = parseMatches(output);
  const impactedFiles = [...new Set(matches.filter((match) => match.file).map((match) => match.file))];
  const tests = impactedFiles.filter((file) => kind(file) === 'test');
  return { command: 'impact', query, generatedFor: current.head, freshness: current.dirty ? 'working-tree-dirty' : 'commit-current', confidence: 'lexical-candidate-set', matchCount: matches.length, files: impactedFiles, tests, matches: matches.slice(0, 200), next: ['Read definitions and direct callers.', 'Check configuration, registration, generated code, and migration edges.', 'Run targeted tests and query again after edits.'] };
}

function moduleMap(prefix) {
  const normalized = prefix.split(BACKSLASH).join('/').replace(/\/$/, '');
  const selected = files().filter((file) => file === normalized || file.startsWith(normalized + '/'));
  return { command: 'module', module: normalized, freshness: freshness(), fileCount: selected.length, byKind: count(selected, kind), files: selected };
}

function help() {
  return { commands: { refresh: 'Write a commit-bound overview to ignored .codegraph/.', overview: 'Print current repository structure without writing.', freshness: 'Check generated index commit and worktree state.', impact: 'Find lexical candidates and likely tests for a symbol or contract term.', module: 'List tracked files for a path prefix.' }, examples: ['node tools/repo-map.mjs refresh', 'node tools/repo-map.mjs overview', 'node tools/repo-map.mjs freshness', 'node tools/repo-map.mjs impact pricingAt', 'node tools/repo-map.mjs module backend/internal/service'] };
}

const args = process.argv.slice(2);
const command = args.shift();
let result;
if (command === 'refresh') result = refresh();
else if (command === 'overview') result = { command: 'overview', ...baseOverview() };
else if (command === 'freshness') result = freshness();
else if (command === 'impact' && args.length > 0) result = impact(args.join(' '));
else if (command === 'module' && args.length > 0) result = moduleMap(args.join(' '));
else result = help();
process.stdout.write(JSON.stringify(result, null, 2) + NL);
