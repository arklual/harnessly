import { beforeEach, describe, expect, it } from 'vitest';
import { designWorksOf, lastAsk } from './design-full';
import { applyStep, ensureFeed } from '@/next/stores/conversation';
import type { ConvId, Entry, MachineId } from '@/next/protocol/v2';

describe('designWorksOf', () => {
  it('takes the design opens, not reloads or other tabs', () => {
    const at = { createdAt: 5, appliedAt: null, appliedOn: null };
    expect(
      designWorksOf([
        { id: '1', command: { kind: 'open', tab: 'design', target: 'd1', auto: true }, ...at },
        { id: '2', command: { kind: 'open', tab: 'design', target: 'd1', reload: true }, ...at },
        { id: '3', command: { kind: 'open', tab: 'files', target: '/x' }, ...at },
        { id: '4', command: { kind: 'show' }, ...at },
      ]),
    ).toEqual([{ design: 'd1', at: 5 }]);
  });
});

describe('lastAsk', () => {
  const conv = 'c-ask' as ConvId;
  beforeEach(() => {
    ensureFeed(conv, 'm' as MachineId);
  });
  it('is the newest message of the person', () => {
    const user = (id: string, text: string, at: number) =>
      ({
        id,
        turnId: null,
        at,
        rev: 1,
        t: 'user',
        text,
        attachments: [],
        clientId: null,
      }) as unknown as Entry;
    applyStep(conv, user('e1', 'привет', 1), null);
    applyStep(conv, user('e2', 'правим макет', 2), null);
    applyStep(conv, { id: 'e3', turnId: null, at: 3, rev: 1, t: 'say', md: 'ок' } as unknown as Entry, null);
    expect(lastAsk(conv)).toEqual({ id: 'e2', text: 'правим макет', at: 2 });
  });
});
