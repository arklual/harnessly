import { describe, expect, it } from 'vitest';
import {
  AUTO_WINDOW_MS,
  INTENT_TTL_MS,
  parseIntents,
  planIntents,
  requestOf,
  requestsOf,
  type IPaneIntent,
  type TIntentCommand,
} from '@/next/features/pane/intents';

const NOW = 1_760_000_000_000;

const intent = (
  id: string,
  command: TIntentCommand,
  at = NOW,
  appliedAt: number | null = null,
): IPaneIntent => ({
  id,
  command,
  createdAt: at,
  appliedAt,
  appliedOn: appliedAt === null ? null : 'desktop',
});

const open = (
  tab: string,
  target?: string,
  extra: { auto?: boolean; reload?: boolean } = {},
): TIntentCommand => ({
  kind: 'open',
  tab,
  ...(target === undefined ? {} : { target }),
  ...extra,
});

describe('parseIntents', () => {
  it('keeps well-formed intents, the runtime’s flags with them', () => {
    const body = {
      intents: [
        intent('a', open('design', 'landing', { auto: true })),
        {
          id: 'b',
          command: { kind: 'open', tab: 'files', auto: 'yes' },
          createdAt: NOW,
          appliedAt: null,
          appliedOn: null,
        },
        { id: 'c', command: { kind: 'exec' }, createdAt: NOW, appliedAt: null, appliedOn: null },
        'junk',
      ],
    };
    expect(parseIntents(body).map((x) => x.id)).toEqual(['a']);
    expect(parseIntents(null)).toEqual([]);
  });
});

describe('planIntents', () => {
  it('acts once per browser: what was settled never acts again, a closed tab stays closed', () => {
    const list = [intent('a', open('task', 'PROJ-1'))];
    const first = planIntents(list, [], NOW, { auto: true });
    expect(first?.apply.map((x) => x.id)).toEqual(['a']);
    expect(planIntents(list, first?.settled ?? [], NOW, { auto: true })).toBeNull();
  });

  it('waits for the person: an intent of hours ago still acts on the first look at the conversation', () => {
    const list = [intent('a', open('design', 'landing'), NOW - 5 * 60 * 60_000)];
    expect(planIntents(list, null, NOW, { auto: true })?.apply.map((x) => x.id)).toEqual(['a']);
  });

  it('a browser’s first look does not replay what another page already showed', () => {
    const list = [intent('a', open('task', 'PROJ-1'), NOW, NOW), intent('b', open('task', 'PROJ-2'))];
    const plan = planIntents(list, null, NOW, { auto: true });
    expect(plan?.apply.map((x) => x.id)).toEqual(['b']);
    expect(plan?.settled).toEqual(['a', 'b']);
  });

  it('forgets a day-old intent and opens the newest three of a burst, oldest first', () => {
    const list = [
      intent('old', open('task', 'PROJ-0'), NOW - INTENT_TTL_MS - 1),
      ...[1, 2, 3, 4, 5].map((n) => intent(`n${n}`, open('task', `PROJ-${n}`), NOW + n)),
    ];
    const plan = planIntents(list, [], NOW + 10, { auto: true });
    expect(plan?.apply.map((x) => x.id)).toEqual(['n3', 'n4', 'n5']);
    expect(plan?.settled).toHaveLength(6);
  });

  it('with auto-open off, what the runtime asked by itself is settled without acting; the agent’s own still acts', () => {
    const list = [
      intent('auto', open('design', 'landing', { auto: true })),
      intent('own', open('task', 'PROJ-1')),
    ];
    const plan = planIntents(list, [], NOW, { auto: false });
    expect(plan?.apply.map((x) => x.id)).toEqual(['own']);
    expect(plan?.settled).toEqual(['auto', 'own']);
  });

  it('a kind switched off is settled without acting; its reloads still reach an open tab', () => {
    const list = [
      intent('d', open('design', 'landing', { auto: true })),
      intent('a', open('artifacts', '/agents/docs/x', { auto: true })),
      intent('r', open('artifacts', '/agents/docs/y', { auto: true, reload: true })),
    ];
    const kinds = { design: true, artifacts: false, browser: true, files: true };
    const plan = planIntents(list, [], NOW, { auto: true, kinds });
    expect(plan?.apply.map((x) => x.id)).toEqual(['d', 'r']);
    expect(plan?.settled).toEqual(['d', 'a', 'r']);
  });

  it('the runtime opens three tabs in ten minutes at most, one look or many; the agent’s own open is not counted', () => {
    const doc = (n: number) => open('artifacts', `/agents/docs/d${n}`, { auto: true });
    const earlier = [1, 2].map((n) => intent(`d${n}`, doc(n), NOW + n, NOW + n));
    const later = [3, 4, 5].map((n) => intent(`d${n}`, doc(n), NOW + n));
    const own = intent('own', open('task', 'PROJ-1'), NOW + 6);
    const plan = planIntents([...earlier, ...later, own], ['d1', 'd2'], NOW + 10, { auto: true });
    expect(plan?.apply.map((x) => x.id)).toEqual(['d5', 'own']);
    expect(plan?.settled).toEqual(['d3', 'd4', 'd5', 'own']);
    // Out of the window the budget is whole again.
    const after = intent('d9', doc(9), NOW + AUTO_WINDOW_MS + 100);
    const next = planIntents([...earlier, after], ['d1', 'd2'], NOW + AUTO_WINDOW_MS + 200, { auto: true });
    expect(next?.apply.map((x) => x.id)).toEqual(['d9']);
  });

  it('reloads do not take the place of opens in a burst', () => {
    const list = [
      intent('o1', open('task', 'PROJ-1'), NOW + 1),
      intent('o2', open('task', 'PROJ-2'), NOW + 2),
      intent('o3', open('task', 'PROJ-3'), NOW + 3),
      intent('r', open('artifacts', '/agents/docs/x', { auto: true, reload: true }), NOW + 4),
    ];
    expect(planIntents(list, [], NOW + 5, { auto: true })?.apply.map((x) => x.id)).toEqual([
      'o1',
      'o2',
      'o3',
      'r',
    ]);
  });
});

describe('requestOf', () => {
  it('maps every kind of the prompt to a place of the new pane', () => {
    expect(requestOf({ kind: 'show' })).toEqual({ k: 'show', reload: false });
    expect(requestOf(open('overview'))).toEqual({ k: 'tools', reload: false });
    expect(requestOf(open('tools'))).toEqual({ k: 'tools', reload: false });
    expect(requestOf(open('browser', 'localhost:4400'))).toEqual({
      k: 'browser',
      target: 'localhost:4400',
      reload: false,
    });
    expect(requestOf(open('design', 'landing'))).toEqual({ k: 'design', target: 'landing', reload: false });
    expect(requestOf(open('design'))).toEqual({ k: 'design', target: null, reload: false });
    expect(requestOf(open('files', '/w/a.md'))).toEqual({ k: 'files', target: '/w/a.md', reload: false });
    expect(requestOf(open('plan'))).toEqual({ k: 'plan', reload: false });
    expect(requestOf(open('agent-browser'))).toEqual({ k: 'agent-browser', reload: false });
    expect(requestOf(open('subagents'))).toEqual({ k: 'subagent', reload: false });
    expect(requestOf(open('terminal'))).toEqual({ k: 'terminal', reload: false });
    expect(requestOf(open('side-chat'))).toEqual({ k: 'side', reload: false });
    for (const kind of ['changes', 'artifacts', 'gitlab', 'task', 'economics', 'peers', 'gallery', 'disk'])
      expect(requestOf(open(kind, 'x'))).toEqual({ k: 'legacy', kind, target: 'x', reload: false });
    expect(requestOf(open('artifacts', '/agents/docs/x', { reload: true }))).toEqual({
      k: 'legacy',
      kind: 'artifacts',
      target: '/agents/docs/x',
      reload: true,
    });
    expect(requestOf(open('nonsense'))).toBeNull();
  });
});

// only the agent's own `open` brings the pane out; what the runtime asked by itself waits behind.
describe('requestsOf', () => {
  it('splits what the agent asked from what the runtime asked by itself', () => {
    const got = requestsOf([
      intent('a', open('artifacts', '/agents/docs/plan')),
      intent('b', open('agent-browser', undefined, { auto: true })),
      intent('c', open('design', 'landing', { auto: true })),
      intent('d', open('artifacts', '/agents/docs/plan', { auto: true, reload: true })),
      intent('e', { kind: 'show' }),
      intent('f', open('nowhere')),
    ]);
    expect(got.asked).toEqual([
      { k: 'legacy', kind: 'artifacts', target: '/agents/docs/plan', reload: false },
      { k: 'show', reload: false },
    ]);
    expect(got.quiet).toEqual([
      { k: 'agent-browser', reload: false },
      { k: 'design', target: 'landing', reload: false },
      { k: 'legacy', kind: 'artifacts', target: '/agents/docs/plan', reload: true },
    ]);
  });
});
