// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/platform/config', () => ({
  appConfig: () => ({
    taskKeyPattern: '[A-Z][A-Z0-9]{1,9}-\\d{1,6}',
    artifactsUrl: 'https://a.example.org',
    boardsUrl: 'https://a.example.org',
    readerUrl: null,
    standDomain: null,
    trackerHosts: [],
    connectionHosts: ['wiki.example.org'],
    gitlabHosts: [],
    paneHosts: [],
    defaultPins: [],
  }),
}));

const cards = vi.hoisted(() => ({ title: null as string | null }));
vi.mock('@/next/features/chat/link-card-meta', () => ({
  previewSrc: () => '',
  whenText: () => '',
  useLinkCardMeta: (slug: string | null) =>
    slug && cards.title
      ? {
          kind: 'artifact',
          title: cards.title,
          editor: '',
          editedAt: '',
          preview: false,
          page: false,
          excerpt: '',
        }
      : null,
}));

import Markdown from '@/next/features/chat/markdown';
import { RefContext } from '@/next/features/chat/ref-chip';

afterEach(() => {
  cleanup();
  cards.title = null;
});

const show = (md: string, onOpen = vi.fn()) => {
  render(
    <RefContext.Provider value={{ refs: {}, onOpen }}>
      <Markdown md={md} />
    </RefContext.Provider>,
  );
  return onOpen;
};

describe('a bare address of a document or a board (HRN-1652)', () => {
  const doc = 'https://a.example.org/a/eval-laboratoriya-proektnaya-doka-uuwhtz';

  it('says the slug as words until the hub names the document, and still opens it', () => {
    const onOpen = show(`Doc: <${doc}>`);
    fireEvent.click(screen.getByRole('link', { name: 'eval laboratoriya proektnaya doka' }));
    expect(onOpen).toHaveBeenCalledWith('link', doc, doc);
  });

  it('says the name of the document when the hub has it', () => {
    cards.title = 'Эвал-лаборатория: проектная дока';
    show(`Doc: <${doc}>`);
    expect(screen.getByRole('link', { name: 'Эвал-лаборатория: проектная дока' })).toBeTruthy();
    expect(screen.queryByText(doc)).toBeNull();
  });

  it('names a board too', () => {
    show('Board: <https://a.example.org/b/schema-sistemy-k3m9xq>');
    expect(screen.getByRole('link', { name: 'schema sistemy' })).toBeTruthy();
  });

  it('keeps the text the author gave the link', () => {
    cards.title = 'Другое имя';
    show(`See [Мой план](${doc}).`);
    expect(screen.getByRole('link', { name: 'Мой план' })).toBeTruthy();
  });
});

describe('a path in the text (HRN-1652)', () => {
  it('says the file’s name and keeps the path for the hint', () => {
    const onOpen = show('Look at web/app/next/feed.tsx now');
    const chip = screen.getByRole('button', { name: 'feed.tsx' });
    expect(chip.getAttribute('title')).toBe('web/app/next/feed.tsx');
    fireEvent.click(chip);
    expect(onOpen).toHaveBeenCalledWith('file', 'web/app/next/feed.tsx', null);
  });
});

it('hands a connected Wiki address in inline code to the pane resolver', () => {
  const url = 'https://wiki.example.org/wiki/spaces/ABC/pages/117';
  const open = show('Read `' + url + '`.');
  fireEvent.click(screen.getByRole('link', { name: url }));
  expect(open).toHaveBeenCalledWith('link', url, url);
});
