import { describe, expect, it } from 'vitest';
import { nothingToSend } from './nothing-to-send';

describe('nothingToSend', () => {
  it('holds an empty line with only the context around it', () => {
    expect(nothingToSend({ text: '', attached: 0 })).toBe(true);
    expect(nothingToSend({ text: '  \n ', attached: 0 })).toBe(true);
  });
  it('sends words, or an attachment without words', () => {
    expect(nothingToSend({ text: 'добавь комментарий', attached: 0 })).toBe(false);
    expect(nothingToSend({ text: '', attached: 1 })).toBe(false);
  });
});
