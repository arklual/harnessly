import { describe, expect, it } from 'vitest';
import { readCardMeta, whenText } from '@/next/features/chat/link-card-meta';
import { cardSlugOf } from '@/next/features/chat/link-card';

const config = {
  artifactsUrl: 'https://portal.example.com',
  boardsUrl: 'https://portal.example.com',
  readerUrl: null,
};

describe('link card meta (HRN-1478)', () => {
  it('reads the hub’s card and nothing else', () => {
    expect(
      readCardMeta({
        kind: 'board',
        title: ' Стена ',
        author: 'Аня',
        editor: 'Боб',
        editedAt: '2026-11-14T10:00:00Z',
        preview: true,
        page: true,
      }),
    ).toEqual({
      kind: 'board',
      title: 'Стена',
      editor: 'Боб',
      editedAt: '2026-11-14T10:00:00Z',
      preview: true,
      page: false,
      excerpt: '',
    });
    expect(readCardMeta({ kind: 'artifact', title: 'План' })?.preview).toBe(false);
    expect(readCardMeta({ kind: 'artifact', title: 'Отчёт', page: true })?.page).toBe(true);
    expect(readCardMeta({ kind: 'artifact', title: 'План', excerpt: ' первый шаг\nвторой ' })?.excerpt).toBe(
      'первый шаг\nвторой',
    );
    expect(readCardMeta({ kind: 'page', title: 'x' })).toBeNull();
    expect(readCardMeta({ kind: 'board', title: '' })).toBeNull();
    expect(readCardMeta(null)).toBeNull();
  });

  it('names the slug of a board and of a document', () => {
    expect(cardSlugOf('https://portal.example.com/b/plan-abc?k=K', config as never)).toBe('plan-abc');
    expect(cardSlugOf('https://portal.example.com/a/note-1', config as never)).toBe('note-1');
    expect(cardSlugOf('https://other.example.com/a/note-1', config as never)).toBeNull();
  });

  it('says when in words', () => {
    const now = Date.parse('2026-11-14T12:00:00Z');
    expect(whenText('', now)).toBe('');
    expect(whenText('2026-11-14T11:55:00Z', now)).not.toBe('');
    expect(whenText('2026-11-14T09:00:00Z', now)).toMatch(/3/);
    expect(whenText('2026-10-01T09:00:00Z', now)).toMatch(/1/);
  });
});
