// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MessagePictures, namedIn, NamedPictures, withoutPaths } from '@/next/features/chat/pictures';
import UserMessage from '@/next/features/chat/user-message';
import type { Picture } from '@/next/protocol/v2';

afterEach(cleanup);

const SHOT = '/tmp/claude-1001/-home-x/s1/scratchpad/home.png';
const PASTE = '/home/coder/.harnessly/uploads/unknown/unknown/1792894270382-e2af4dde-image.png';
const LISTED: Picture[] = [
  { id: 'p1', text: PASTE },
  { id: 'p2', text: SHOT },
];
const srcOf = (picture: Picture) => `/shot?id=${picture.id}`;

describe('pictures a message names', () => {
  it('finds the listed paths standing on their own, in the order the text names them', () => {
    expect(namedIn(`Скриншот: ${SHOT}. И ещё ${PASTE}`, LISTED).map((p) => p.id)).toEqual(['p2', 'p1']);
    expect(namedIn(`${SHOT}.bak и /x${SHOT} и https://h${SHOT}`, LISTED)).toEqual([]);
  });

  it('takes the paths out of the words', () => {
    expect(withoutPaths(`Вот что видит покупатель\n${PASTE}`, LISTED)).toBe('Вот что видит покупатель');
    expect(withoutPaths(`до ${PASTE} после`, LISTED)).toBe('до после');
  });

  it('drops the label and the empty code a path leaves', () => {
    expect(withoutPaths(`Готово.\n\nСкриншот: ${SHOT}`, LISTED)).toBe('Готово.');
    expect(withoutPaths(`Готово.\n- Screenshot: \`${SHOT}\``, LISTED)).toBe('Готово.');
    expect(withoutPaths(`Сравни: было ${SHOT}, стало лучше`, LISTED)).toBe('Сравни: было , стало лучше');
  });

  it('shows a card under the agent text, and a picture that does not load leaves none', () => {
    render(
      <MessagePictures.Provider value={{ pictures: LISTED, srcOf }}>
        <NamedPictures text={`Готово.\n\nСкриншот: ${SHOT}`} />
      </MessagePictures.Provider>,
    );
    const img = document.querySelector('.hx-pic__img') as HTMLImageElement;
    expect(img.getAttribute('src')).toBe('/shot?id=p2');
    expect(img.getAttribute('loading')).toBe('lazy');
    fireEvent.error(img);
    expect(document.querySelector('.hx-pic')).toBeNull();
  });

  it('shows the person’s pasted picture instead of its path, and opens it full size', () => {
    render(
      <MessagePictures.Provider value={{ pictures: LISTED, srcOf }}>
        <UserMessage
          text={`Вот что видит покупатель\n${PASTE}`}
          attachments={[]}
          outgoing={null}
          offline={false}
          onRetry={vi.fn()}
          onDelete={vi.fn()}
        />
      </MessagePictures.Provider>,
    );
    expect(document.querySelector('.hx-bubble')?.textContent).toBe('Вот что видит покупатель');
    const card = document.querySelector('.hx-pic') as HTMLButtonElement;
    fireEvent.load(card.querySelector('img') as HTMLImageElement);
    expect(card.dataset.state).toBe('ready');
    fireEvent.click(card);
    expect(screen.getByRole('dialog')).toBeTruthy();
  });

  it('leaves a path nobody listed as text', () => {
    render(
      <UserMessage
        text={`глянь ${PASTE}`}
        attachments={[]}
        outgoing={null}
        offline={false}
        onRetry={vi.fn()}
        onDelete={vi.fn()}
      />,
    );
    expect(document.querySelector('.hx-bubble')?.textContent).toBe(`глянь ${PASTE}`);
    expect(document.querySelector('.hx-pic')).toBeNull();
  });
});
