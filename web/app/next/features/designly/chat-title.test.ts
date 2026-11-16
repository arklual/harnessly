import { describe, expect, it } from 'vitest';
import { t } from '@/next/i18n';
import { chatTitleOf } from '@/next/features/designly/chat-title';

const base = {
  convId: 'tab-k1yksasiw',
  convTitle: null,
  rowTitle: null,
  space: false,
  designTitle: 'harnessly',
};

describe('the design chat’s heading (HRN-1467)', () => {
  it('names the chat, not the design', () => {
    expect(chatTitleOf({ ...base, convTitle: 'Аналоги Dayflow' })).toBe('Аналоги Dayflow');
  });

  it('takes the chat list’s title before the chat itself has come', () => {
    expect(chatTitleOf({ ...base, rowTitle: 'Аналоги Dayflow' })).toBe('Аналоги Dayflow');
  });

  it('never falls back to the design’s title for a chat that is there', () => {
    expect(chatTitleOf(base)).toBe(t('next.site.chatUntitled'));
    expect(chatTitleOf({ ...base, convTitle: '  ' })).not.toBe('harnessly');
  });

  it('names a chat still to be made by what it is about', () => {
    expect(chatTitleOf({ ...base, convId: null })).toBe(t('next.site.newDesignChat'));
    expect(chatTitleOf({ ...base, convId: null, space: true })).toBe(
      t('next.site.newSpaceChat', { name: 'harnessly' }),
    );
  });
});
