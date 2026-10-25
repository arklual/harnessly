import { describe, expect, it } from 'vitest';
import type { AttachmentRef } from '@/next/protocol/v2';
import { isPristine, judgeRemote, nextWrite, REMOTE } from '@/next/stores/draft-sync';

const shot: AttachmentRef = { id: 'up_1', name: 'CleanShot.png', kind: 'image', mime: 'image/png' };
const f = (text: string, attachments: AttachmentRef[] = []) => ({ text, attachments });
const base = (rev: number, text: string, attachments: AttachmentRef[] = []) => ({ rev, text, attachments });

describe('the rules between a field and a draft of another device', () => {
  it('a field is pristine while its text and files are its base’s; a field never synced, while it is empty', () => {
    expect(isPristine(f('привет'), base(3, 'привет'))).toBe(true);
    expect(isPristine(f('привет!'), base(3, 'привет'))).toBe(false);
    expect(isPristine(f('привет', [shot]), base(3, 'привет'))).toBe(false);
    expect(isPristine(f('привет', [shot]), base(3, 'привет', [shot]))).toBe(true);
    expect(isPristine(f(''), undefined)).toBe(true);
    expect(isPristine(f('', [shot]), undefined)).toBe(false);
    expect(isPristine(f('что-то своё'), undefined)).toBe(false);
  });

  it('an older or the same revision changes nothing', () => {
    expect(judgeRemote(f('a'), base(4, 'a'), { rev: 4 }, false)).toBe(REMOTE.ignore);
    expect(judgeRemote(f('a'), base(4, 'a'), { rev: 2 }, false)).toBe(REMOTE.ignore);
  });

  it('a pristine field takes the newer draft, a clear included — files the base had go with it', () => {
    expect(judgeRemote(f('Проверь'), base(3, 'Проверь'), { rev: 4 }, false)).toBe(REMOTE.take);
    expect(judgeRemote(f('смотри', [shot]), base(3, 'смотри', [shot]), { rev: 4 }, false)).toBe(REMOTE.take);
    expect(judgeRemote(f(''), undefined, { rev: 7 }, false)).toBe(REMOTE.take);
  });

  it('a field changed here keeps what it has; so does one whose write is on its way', () => {
    expect(judgeRemote(f('Проверь миграцию'), base(3, 'Проверь'), { rev: 4 }, false)).toBe(REMOTE.keep);
    expect(judgeRemote(f('Проверь', [shot]), base(3, 'Проверь'), { rev: 4 }, false)).toBe(REMOTE.keep);
    expect(judgeRemote(f('Проверь'), base(3, 'Проверь'), { rev: 4 }, true)).toBe(REMOTE.keep);
    expect(judgeRemote(f('своё'), undefined, { rev: 1 }, false)).toBe(REMOTE.keep);
  });

  it('the next write is the next revision of the base with the files, and none while the machine has the field', () => {
    expect(nextWrite(f('abc'), base(3, 'ab'))).toEqual({ text: 'abc', attachments: [], rev: 4 });
    expect(nextWrite(f('ab', [shot]), base(3, 'ab'))).toEqual({ text: 'ab', attachments: [shot], rev: 4 });
    expect(nextWrite(f(''), base(3, 'ab', [shot]))).toEqual({ text: '', attachments: [], rev: 4 });
    expect(nextWrite(f('первое'), undefined)).toEqual({ text: 'первое', attachments: [], rev: 1 });
    expect(nextWrite(f('ab'), base(3, 'ab'))).toBeNull();
    expect(nextWrite(f(''), undefined)).toBeNull();
  });
});
