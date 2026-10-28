import { describe, expect, it } from 'vitest';
import type { IAsk } from '@/next/features/asks/api';
import { askOfCall, parseAskCall, shellWords } from '@/next/features/asks/parse';

const ask = (over: Partial<IAsk>): IAsk => ({
  id: 'ask_000000000001',
  kind: 'question',
  status: 'waiting',
  question: 'Which port?',
  details: '',
  link: '',
  options: [],
  to: { name: '', username: 'anna_dev' },
  bot: 'Team',
  chat: 'Team chat',
  silent: false,
  conversation: 'tab-1',
  agent: 'Fix login',
  createdAt: 1,
  deadline: 2,
  closedAt: 0,
  decision: '',
  answer: null,
  replies: [],
  seenAt: 0,
  seenReplies: 0,
  error: '',
  ...over,
});

describe('shellWords', () => {
  it('reads quotes and escapes like a shell, and stops at a pipe', () => {
    expect(shellWords(`ask --to "Anna K" 'one two' a\\ b | tee x`)).toEqual([
      'ask',
      '--to',
      'Anna K',
      'one two',
      'a b',
    ]);
    expect(shellWords(`"say \\"hi\\""`)).toEqual(['say "hi"']);
  });
});

describe('parseAskCall', () => {
  it('reads a question with options', () => {
    const call = parseAskCall('Bash', '$ harnessly ask --to Anna -o Yes --option=No "Ship it now?"', null);
    expect(call).toEqual({ kind: 'question', to: 'Anna', question: 'Ship it now?', id: null });
  });

  it('reads an approval and the id the command printed', () => {
    const call = parseAskCall(
      'Bash',
      '$ harnessly ask approve --to=@lead --link https://example.com/rfc "RFC: new store"',
      'asked ask_0123456789ab → @lead in «Team», waiting up to 1h0m0s\napproved by @lead\n',
    );
    expect(call).toEqual({
      kind: 'approval',
      to: '@lead',
      question: 'RFC: new store',
      id: 'ask_0123456789ab',
    });
  });

  it('leaves other commands and subcommands alone', () => {
    expect(parseAskCall('Bash', '$ harnessly ask wait ask_0123456789ab', null)).toBeNull();
    expect(parseAskCall('Bash', '$ harnessly notify send --bot x hi', null)).toBeNull();
    expect(parseAskCall('Read', '$ harnessly ask --to a b', null)).toBeNull();
  });
});

describe('askOfCall', () => {
  const asks = [
    ask({ id: 'ask_000000000003', question: 'Which  port?', conversation: 'tab-2' }),
    ask({ id: 'ask_000000000002', question: 'Which port?', conversation: 'tab-1' }),
  ];
  it('finds by id first, else by the question in the same conversation', () => {
    const byText = parseAskCall('Bash', '$ harnessly ask --to Anna "Which port?"', null)!;
    expect(askOfCall(asks, byText, 'tab-1')?.id).toBe('ask_000000000002');
    expect(askOfCall(asks, { ...byText, id: 'ask_000000000003' }, 'tab-1')?.id).toBe('ask_000000000003');
    expect(askOfCall(asks, { ...byText, kind: 'approval' }, 'tab-1')).toBeNull();
  });
});
