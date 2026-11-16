// a step's line says what is done in words, how long it ran and how it ended; three runs of one script
// with a long path in front are told apart.
import { describe, expect, it } from 'vitest';
import {
  compactCommand,
  groupLabel,
  mcpServerName,
  mcpToolName,
  shortenPath,
  stepLine,
  stepMeta,
  stepPreview,
} from '@/next/features/chat/step-line';
import type { ToolEntry } from '@/next/features/chat/types';

const entry = (over: Partial<ToolEntry>): ToolEntry => ({
  id: 'c:1#1',
  turnId: null,
  at: 1_000_000,
  rev: 1,
  t: 'tool',
  toolUseId: 'tu',
  tool: 'Bash',
  summary: '',
  ok: true,
  output: null,
  ...over,
});

const LONG = 'S=/tmp/claude-995/-srv-acme-design-analytics-page/56e4efc7-2442-4d3d-b3f0-1c2a9e0f7d11';

describe('a command in its line', () => {
  it('drops the set-up in front and keeps what tells commands apart', () => {
    expect(compactCommand(`${LONG}; cd $S && node run.mjs --mode code`)).toBe('node run.mjs --mode code');
    expect(compactCommand(`${LONG}; tail -5 $S/out.log`)).toBe('tail -5 $S/out.log');
    expect(compactCommand('FOO=1 BAR="a b" go test ./internal/feed')).toBe('go test ./internal/feed');
  });

  it('keeps a lone assignment and a plain command as they are', () => {
    expect(compactCommand('git status -sb')).toBe('git status -sb');
    expect(compactCommand('X=1')).toBe('X=1');
  });

  it('unwraps Codex shells and cuts long paths in the middle', () => {
    expect(compactCommand(`bash -lc 'cd /w && ls'`)).toBe('ls');
    const short = shortenPath('/home/dev/projects/harbor/services/web/app/next/x.tsx');
    expect(short.startsWith('/home/…/')).toBe(true);
    expect(short.endsWith('/x.tsx')).toBe(true);
  });

  it('leads with the agent description, the command second', () => {
    const line = stepLine(
      entry({ call: { description: 'Запускает обычный режим', command: `${LONG}; node run.mjs` } }),
      `$ ${LONG}; node run.mjs`,
    );
    expect(line.title).toBe('Запускает обычный режим');
    expect(line.detail).toBe('node run.mjs');
    expect(line.command).toBe(`${LONG}; node run.mjs`);
  });
});

describe('how a step ended', () => {
  it('shows the time from a second, and the exit code of a failure', () => {
    expect(stepMeta(entry({ call: { durationMs: 400 } }), 0)).toBeNull();
    expect(stepMeta(entry({ call: { durationMs: 12_000 } }), 0)?.text).toMatch(/12/);
    const failed = stepMeta(entry({ ok: false, call: { durationMs: 3_000, exit: 2 } }), 0);
    expect(failed?.failed).toBe(true);
    expect(failed?.text).toMatch(/2/);
  });

  it('counts how long a running step has been going', () => {
    expect(stepMeta(entry({ ok: null }), 1_000_000 + 65_000)?.text).toMatch(/1/);
  });

  it('previews the last lines, and the first line of an error', () => {
    const ok = stepPreview(
      entry({ output: { text: 'a\nb\nc\nd', truncated: null }, call: { lines: 19, tail: 'b\nc\nd' } }),
      'run',
    );
    expect(ok).toEqual({ lines: ['b', 'c', 'd'], more: 16, failed: false });
    const bad = stepPreview(
      entry({ ok: false, output: { text: 'Exit code 1\nboom\nat x', truncated: null } }),
      'run',
    );
    expect(bad?.lines).toEqual(['boom']);
    expect(bad?.failed).toBe(true);
    expect(stepPreview(entry({ output: { text: '19 lines', truncated: null } }), 'run')).toBeNull();
  });
});

describe('other tools', () => {
  it('a Read says the lines it asked for', () => {
    const line = stepLine(entry({ tool: 'Read', call: { offset: 120, limit: 61 } }), 'Read /w/a.ts');
    expect(line.kind).toBe('read');
    expect(line.fact).toMatch(/120.180/);
  });

  it('an edit says its lines added and removed', () => {
    const line = stepLine(entry({ tool: 'Edit' }), 'Update /w/a.ts (+12, -3)');
    expect(line.fact).toBe('+12 −3');
    expect(line.detail).toBe('/w/a.ts');
  });

  it('a search says how much it found', () => {
    const line = stepLine(
      entry({ tool: 'Grep', output: { text: 'Found 4 files\na\nb', truncated: null } }),
      'Grep "orderId" → 5 results',
    );
    expect(line.detail).toBe('«orderId»');
    expect(line.fact).toMatch(/4/);
  });

  it('an MCP tool reads by its server and name', () => {
    expect(mcpServerName('plugin_acme-tracker_youtrack')).toBe('youtrack');
    expect(mcpToolName('search_issues')).toBe('search issues');
    const line = stepLine(
      entry({ tool: 'mcp__plugin_acme-tracker_youtrack__search_issues' }),
      'mcp__plugin_acme-tracker_youtrack__search_issues project: HRN',
    );
    expect(line.title).toBe('youtrack · search issues');
    expect(line.detail).toBe('project: HRN');
  });
});

describe('the head of a run of tools', () => {
  it('names commands and the one running now', () => {
    const tools = [
      entry({ id: 'a', ok: null, call: { description: 'Гоняет Code Mode', command: 'node a.mjs' } }),
      entry({ id: 'b', call: { command: 'node b.mjs' } }),
      entry({ id: 'c', ok: false, call: { command: 'node c.mjs' } }),
    ];
    const label = groupLabel(tools, (e) => stepLine(e, ''));
    expect(label).toMatch(/3/);
    expect(label).toContain('Гоняет Code Mode');
  });
});
