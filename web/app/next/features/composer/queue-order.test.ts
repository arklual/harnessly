import { describe, expect, it } from 'vitest';
import {
  inQueueOrder,
  isMovable,
  movableCount,
  planMove,
  planStep,
  DIRECTION,
} from '@/next/features/composer/queue-order';
import type { QueueRow } from '@/next/features/composer/types';
import { EDGE } from '@/next/ui/use-drag';

const row = (clientId: string, over: Partial<QueueRow> = {}): QueueRow => ({
  clientId,
  text: clientId,
  attachments: [],
  state: { k: 'queued', position: 1, reason: 'behind' },
  ...over,
});
const typing: QueueRow['state'] = { k: 'typing' };

describe('the rows that move', () => {
  it('are the ones that wait in the machine’s line, not the ones on their way or failed', () => {
    expect(isMovable(row('a'))).toBe(true);
    expect(isMovable(row('a', { state: typing }))).toBe(false);
    expect(isMovable(row('a', { steer: true }))).toBe(false);
    expect(
      isMovable(
        row('a', { state: { k: 'failed', code: 'type-failed', retryable: true } as QueueRow['state'] }),
      ),
    ).toBe(false);
    expect(movableCount([row('a'), row('b', { state: typing })])).toBe(1);
  });
});

describe('inQueueOrder', () => {
  const rows = [row('a'), row('b'), row('c')];
  it('follows the machine’s line', () => {
    expect(inQueueOrder(rows, ['c', 'a', 'b']).map((r) => r.clientId)).toEqual(['c', 'a', 'b']);
  });
  it('leaves the rows that are not in the line where they stand', () => {
    const mixed = [row('a'), row('x', { state: typing }), row('b')];
    expect(inQueueOrder(mixed, ['b', 'a']).map((r) => r.clientId)).toEqual(['b', 'x', 'a']);
  });
  it('keeps the rows as they are while the line is not known', () => {
    expect(inQueueOrder(rows, null).map((r) => r.clientId)).toEqual(['a', 'b', 'c']);
    expect(inQueueOrder(rows, []).map((r) => r.clientId)).toEqual(['a', 'b', 'c']);
  });
  it('ignores an id of the line that has no row', () => {
    expect(inQueueOrder(rows, ['z', 'b', 'a']).map((r) => r.clientId)).toEqual(['b', 'a', 'c']);
  });
});

describe('planMove', () => {
  const rows = [row('a'), row('b'), row('c'), row('d')];
  it('puts a row before the one it is dropped above', () => {
    expect(planMove(rows, 'd', 'b', EDGE.before)).toEqual({ id: 'd', beforeId: 'b' });
  });
  it('puts a row after the one it is dropped below: before the next one', () => {
    expect(planMove(rows, 'a', 'b', EDGE.after)).toEqual({ id: 'a', beforeId: 'c' });
    expect(planMove(rows, 'a', 'c', EDGE.after)).toEqual({ id: 'a', beforeId: 'd' });
  });
  it('puts a row last after the last one', () => {
    expect(planMove(rows, 'a', 'd', EDGE.after)).toEqual({ id: 'a', beforeId: null });
  });
  it('asks for nothing when the row stays where it is', () => {
    expect(planMove(rows, 'b', 'b', EDGE.before)).toBeNull();
    expect(planMove(rows, 'b', 'a', EDGE.after)).toBeNull();
    expect(planMove(rows, 'b', 'c', EDGE.before)).toBeNull();
    expect(planMove(rows, 'd', 'c', EDGE.after)).toBeNull();
  });
  it('does not carry a row that is on its way, nor drop beside one', () => {
    const mixed = [row('a'), row('x', { state: typing }), row('b')];
    expect(planMove(mixed, 'x', 'a', EDGE.before)).toBeNull();
    expect(planMove(mixed, 'b', 'x', EDGE.before)).toBeNull();
  });
  it('keeps what was sent «now» at the head', () => {
    const urgent = [row('n', { ahead: true }), row('a'), row('b')];
    expect(planMove(urgent, 'b', 'n', EDGE.before)).toBeNull();
    expect(planMove(urgent, 'n', 'b', EDGE.after)).toBeNull();
    expect(planMove(urgent, 'b', 'a', EDGE.before)).toEqual({ id: 'b', beforeId: 'a' });
    expect(planMove(urgent, 'b', 'n', EDGE.after)).toEqual({ id: 'b', beforeId: 'a' });
  });
});

describe('planStep', () => {
  const rows = [row('a'), row('b'), row('c')];
  it('moves a row one place up or down', () => {
    expect(planStep(rows, 'c', DIRECTION.up)).toEqual({ id: 'c', beforeId: 'b' });
    expect(planStep(rows, 'a', DIRECTION.down)).toEqual({ id: 'a', beforeId: 'c' });
    expect(planStep(rows, 'b', DIRECTION.down)).toEqual({ id: 'b', beforeId: null });
  });
  it('stops at the ends of the line and at «now»', () => {
    expect(planStep(rows, 'a', DIRECTION.up)).toBeNull();
    expect(planStep(rows, 'c', DIRECTION.down)).toBeNull();
    expect(planStep([row('n', { ahead: true }), row('a')], 'a', DIRECTION.up)).toBeNull();
    expect(planStep(rows, 'nope', DIRECTION.up)).toBeNull();
  });
});
