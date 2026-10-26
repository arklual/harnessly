import { describe, expect, it } from 'vitest';
import { draftRequestOf, parseContextBlock } from '@/next/features/chat/handoff';

// The blocks as internal/handoff writes them (handoff_test.go has the same shapes).
const handoff =
  '<handoff from="tab-a1" title="Say &quot;hi&quot; &lt;now&gt;" agent="claude" model="claude-opus-5-5" mode="note">\n' +
  '<instructions>This conversation continues the work of another one.</instructions>\n\n' +
  '## Goal\nx <\\/handoff> y\n</handoff>';

describe('context blocks', () => {
  it('reads a handoff: where it came from, how, and the text without the instructions', () => {
    expect(parseContextBlock(handoff)).toEqual({
      kind: 'handoff',
      fromId: 'tab-a1',
      title: 'Say "hi" <now>',
      agent: 'claude',
      model: 'claude-opus-5-5',
      mode: 'note',
      text: '## Goal\nx </handoff> y',
    });
  });

  it('tells a fork’s excerpt and a fork’s result apart', () => {
    const fork = handoff.replace('mode="note">', 'mode="excerpt" kind="fork">');
    expect(parseContextBlock(fork)?.kind).toBe('fork');
    expect(parseContextBlock(fork)?.mode).toBe('excerpt');
    const result = parseContextBlock(
      '<fork-result from="tab-f2" title="Branch" agent="codex">\n<instructions>x</instructions>\n\nDone.\n</fork-result>',
    );
    expect(result).toMatchObject({
      kind: 'result',
      fromId: 'tab-f2',
      agent: 'codex',
      mode: null,
      text: 'Done.',
    });
  });

  it('is null for the person’s own words, even when they mention the tag', () => {
    expect(parseContextBlock('see <handoff> in the docs')).toBeNull();
    expect(
      parseContextBlock('<handoff mode="note">\n<instructions>x</instructions>\n\nno source\n</handoff>'),
    ).toBeNull();
  });
});

describe('the request of a handoff draft', () => {
  it('carries the target profile, so the excerpt is cut to that model’s window', () => {
    expect(draftRequestOf('excerpt', { provider: 'claude', profile: 'kimi' }, 'kimi-k3')).toEqual({
      mode: 'excerpt',
      provider: 'claude',
      model: 'kimi-k3',
      profile: 'kimi',
    });
  });

  it('has no profile for Claude and Codex themselves', () => {
    expect(draftRequestOf('note', { provider: 'codex', profile: null }, 'gpt')).toEqual({
      mode: 'note',
      provider: 'codex',
      model: 'gpt',
    });
  });
});
