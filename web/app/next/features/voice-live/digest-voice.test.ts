import { describe, expect, it } from 'vitest';
import { DIGEST_VOICE } from '@/next/features/voice-live/digest-listen';
import {
  DIGEST_SAID,
  DIGEST_STEP,
  digestSaid,
  digestScreen,
  digestState,
  readNote,
  saidOut,
  splitSaid,
} from '@/next/features/voice-live/digest-voice';
import { FLOOR } from '@/next/features/voice-live/use-voice-call';

describe('digest by voice', () => {
  it('tells going on, ending and a question apart', () => {
    expect(digestSaid('Да')).toBe(DIGEST_SAID.on);
    expect(digestSaid('продолжай')).toBe(DIGEST_SAID.on);
    expect(digestSaid('давай дальше')).toBe(DIGEST_SAID.on);
    expect(digestSaid('Хватит, спасибо')).toBe(DIGEST_SAID.stop);
    expect(digestSaid('нет')).toBe(DIGEST_SAID.stop);
    expect(digestSaid('а кто ревьюит этот MR?')).toBe(DIGEST_SAID.ask);
    expect(digestSaid('да, а что там с оплатой у Иры?')).toBe(DIGEST_SAID.ask);
    expect(digestSaid('  ')).toBeNull();
  });
  it('lights the said words of a phrase', () => {
    expect(splitSaid('Три задачи ждут ревью сегодня', 'Три задачи')).toEqual({
      said: 'Три задачи',
      rest: 'ждут ревью сегодня',
    });
    expect(splitSaid('Коротко', 'Коротко и ещё')).toEqual({ said: 'Коротко', rest: '' });
  });
  it('knows a phrase cut short', () => {
    expect(saidOut('Три задачи ждут ревью сегодня', 'Три задачи ждут ревью сегодня.')).toBe(true);
    expect(saidOut('Три задачи ждут ревью сегодня', 'Три задачи ждут ревью')).toBe(true);
    expect(saidOut('Три задачи ждут ревью сегодня', 'Три')).toBe(false);
  });
  it('gives the voice the digest with its anchors and a note to read word for word', () => {
    const ph = [{ text: 'Первое.', anchor: 'p1' }];
    expect(digestScreen(ph)).toContain('[p1] Первое.');
    expect(readNote(ph[0])).toBe('READ: Первое.');
  });
  it('maps where the reading is to the controls', () => {
    expect(digestState(false, DIGEST_STEP.reading, FLOOR.speaking, false)).toBe(DIGEST_VOICE.off);
    expect(digestState(true, DIGEST_STEP.reading, FLOOR.speaking, false)).toBe(DIGEST_VOICE.reading);
    expect(digestState(true, DIGEST_STEP.paused, FLOOR.speaking, false)).toBe(DIGEST_VOICE.paused);
    expect(digestState(true, DIGEST_STEP.reading, FLOOR.speaking, true)).toBe(DIGEST_VOICE.barged);
    expect(digestState(true, DIGEST_STEP.answering, FLOOR.thinking, false)).toBe(DIGEST_VOICE.thinking);
    expect(digestState(true, DIGEST_STEP.asking, FLOOR.listening, false)).toBe(DIGEST_VOICE.listening);
  });
});
