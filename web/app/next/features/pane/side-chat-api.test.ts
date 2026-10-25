import { describe, expect, it } from 'vitest';
import { readFrames } from '@/next/features/pane/side-chat-api';

describe('readFrames', () => {
  it('reads whole frames and keeps the unfinished one', () => {
    const { events, rest } = readFrames(
      ': ping\n\ndata: {"type":"delta","text":"Он "}\n\ndata: {"type":"done","text":"Он сделал"}\n\ndata: {"type":"del',
    );
    expect(events).toEqual([
      { type: 'delta', text: 'Он ' },
      { type: 'done', text: 'Он сделал' },
    ]);
    expect(rest).toBe('data: {"type":"del');
  });

  it('reads an error frame and skips what is not JSON', () => {
    const { events } = readFrames('data: nope\n\ndata: {"type":"error","code":"busy","message":"занят"}\n\n');
    expect(events).toEqual([{ type: 'error', code: 'busy', message: 'занят' }]);
  });
});
