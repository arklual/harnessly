import { describe, expect, it } from 'vitest';
import { ANSWER_EVENT, readAnswerFrames, withoutMarks } from '@/next/features/search/sources-api';

describe('readAnswerFrames', () => {
  it('reads whole events and keeps the unfinished one for the next piece', () => {
    const first = readAnswerFrames('event: delta\ndata: {"text":"В сейфе"}\n\nevent: delta\ndata: {"te');
    expect(first.events).toEqual([{ type: ANSWER_EVENT.delta, text: 'В сейфе' }]);
    const second = readAnswerFrames(`${first.rest}xt":" машины [1]"}\n\n`);
    expect(second.events).toEqual([{ type: ANSWER_EVENT.delta, text: ' машины [1]' }]);
    expect(second.rest).toBe('');
  });

  it('reads the end with the sources and a failure with its reason', () => {
    const done = readAnswerFrames('event: done\r\ndata: {"text":"В сейфе машины.","cited":[0,2]}\r\n\r\n');
    expect(done.events).toEqual([
      { type: ANSWER_EVENT.done, answer: { text: 'В сейфе машины.', cited: [0, 2] } },
    ]);
    const failed = readAnswerFrames(
      'event: error\ndata: {"error":"timeout","message":"Модель не ответила."}\n\n',
    );
    expect(failed.events).toEqual([
      { type: ANSWER_EVENT.error, code: 'timeout', message: 'Модель не ответила.' },
    ]);
  });

  it('leaves out what is not an event of the answer', () => {
    expect(readAnswerFrames(': ping\n\nevent: delta\ndata: not json\n\n').events).toEqual([]);
  });
});

describe('withoutMarks', () => {
  it('hides the marks of the sources, a half-written one too', () => {
    expect(withoutMarks('В сейфе [2]. Ключ не возвращается [1][3].')).toBe('В сейфе. Ключ не возвращается.');
    expect(withoutMarks('В сейфе [')).toBe('В сейфе');
    expect(withoutMarks('В сейфе [1')).toBe('В сейфе');
  });
});
