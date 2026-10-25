// The pictures a message names by their path (ConvSnapshot.pictures): the agent's «Скриншот: /tmp/…/home.png», the
// path of a picture pasted into the old view in the person's words. The runtime lists only the ones it may show, by an
// id; a message shows each of them as a card under its text (the person's message instead of the path), and a press
// opens it full size, where the arrows and a swipe step through the pictures of the same message. A picture that does
// not load leaves no card.
import { createContext, useContext, useMemo, useState } from 'react';
import type { Picture } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import ImageView, { type IImageViewItem } from '@/next/ui/image-view';
import '@/next/styles/pictures.css';

export interface IMessagePictures {
  pictures: readonly Picture[];
  /** Where a picture's bytes are. */
  srcOf: (picture: Picture) => string;
}

/** The conversation's listed pictures; null where there is none (a gallery, a side chat). */
export const MessagePictures = createContext<IMessagePictures | null>(null);

const BEFORE = /[\p{L}\p{N}_./~:-]/u;
const AFTER = /[\p{L}\p{N}_/]/u;

/** Where a path stands in a text as a path of its own (not inside a longer one or a link), -1 when it does not. */
const indexOfPath = (text: string, path: string, from = 0): number => {
  for (let at = text.indexOf(path, from); at >= 0; at = text.indexOf(path, at + 1)) {
    const before = at > 0 ? text[at - 1] : '';
    const end = at + path.length;
    const after = text[end] ?? '';
    const another = after === '.' && AFTER.test(text[end + 1] ?? '');
    if (!BEFORE.test(before) && !AFTER.test(after) && !another) return at;
  }
  return -1;
};

/** The listed pictures a text names, in the order it names them. */
export const namedIn = (text: string, pictures: readonly Picture[]): Picture[] =>
  pictures
    .map((picture) => ({ picture, at: indexOfPath(text, picture.text) }))
    .filter((one) => one.at >= 0)
    .sort((a, b) => a.at - b.at)
    .map((one) => one.picture);

/** A line left with only its label once the path went: «Скриншот:», «- Screenshot:». */
const BARE_LABEL = /^[ \t]*(?:[-*][ \t]+)?[\p{L}\p{N} ]{1,40}:[ \t]*$/u;

/**
 * The text without the paths of its pictures, and without what they leave: an empty `code`, a line with only its
 * label, the blank lines and the spaces.
 */
export const withoutPaths = (text: string, pictures: readonly Picture[]): string => {
  let out = text;
  for (const picture of pictures) {
    for (let at = indexOfPath(out, picture.text); at >= 0; at = indexOfPath(out, picture.text, at)) {
      out = out.slice(0, at) + '\u0000' + out.slice(at + picture.text.length);
    }
  }
  out = out
    .split('\n')
    .map((line) => {
      if (!line.includes('\u0000')) return line;
      const left = line.replaceAll('`\u0000`', '').replaceAll('\u0000', '');
      return BARE_LABEL.test(left) ? '' : left;
    })
    .join('\n');
  return out
    .replace(/[ \t]+\n/g, '\n')
    .replace(/\n{3,}/g, '\n\n')
    .replace(/[ \t]{2,}/g, ' ')
    .trim();
};

const NONE: readonly Picture[] = [];

/** The listed pictures a message's text names, and where their bytes are. */
export const useNamedPictures = (
  text: string,
): { named: readonly Picture[]; srcOf: IMessagePictures['srcOf'] } => {
  const listed = useContext(MessagePictures);
  const pictures = listed?.pictures ?? NONE;
  const named = useMemo(() => (pictures.length > 0 ? namedIn(text, pictures) : NONE), [text, pictures]);
  return { named, srcOf: listed?.srcOf ?? (() => '') };
};

const nameOf = (picture: Picture): string => picture.text.slice(picture.text.lastIndexOf('/') + 1);

const LOAD = { loading: 'loading', ready: 'ready', broken: 'broken' } as const;
type TLoad = (typeof LOAD)[keyof typeof LOAD];

const Card = ({ picture, src, onOpen }: { picture: Picture; src: string; onOpen: () => void }) => {
  const [state, setState] = useState<TLoad>(LOAD.loading);
  if (state === LOAD.broken) return null;
  const name = nameOf(picture);
  return (
    <button
      type="button"
      className="hx-pic"
      data-state={state}
      title={picture.text}
      aria-label={t('next.pictures.open', { name })}
      onClick={onOpen}
    >
      <img
        className="hx-pic__img"
        src={src}
        alt=""
        loading="lazy"
        decoding="async"
        draggable={false}
        onLoad={() => setState(LOAD.ready)}
        onError={() => setState(LOAD.broken)}
      />
    </button>
  );
};

interface IPictureCardsProps {
  pictures: readonly Picture[];
  srcOf: IMessagePictures['srcOf'];
  /** The person's message lines its cards up on its side. */
  side?: 'user' | 'agent';
}

export const PictureCards = ({ pictures, srcOf, side = 'agent' }: IPictureCardsProps) => {
  const [viewing, setViewing] = useState<string | null>(null);
  if (pictures.length === 0) return null;
  const at = viewing === null ? -1 : pictures.findIndex((one) => one.id === viewing);
  const current = at >= 0 ? pictures[at] : null;
  const item: IImageViewItem | null = current ? { src: srcOf(current), name: nameOf(current) } : null;
  const step = (delta: 1 | -1) => {
    if (at < 0) return;
    setViewing(pictures[(at + delta + pictures.length) % pictures.length].id);
  };
  return (
    <div className="hx-pics" data-side={side}>
      {pictures.map((picture) => (
        <Card key={picture.id} picture={picture} src={srcOf(picture)} onOpen={() => setViewing(picture.id)} />
      ))}
      <ImageView
        item={item}
        onClose={() => setViewing(null)}
        position={at >= 0 ? { at: at + 1, of: pictures.length } : undefined}
        onStep={step}
      />
    </div>
  );
};

/** The cards of the pictures an agent's text names, under it. */
export const NamedPictures = ({ text }: { text: string }) => {
  const { named, srcOf } = useNamedPictures(text);
  return <PictureCards pictures={named} srcOf={srcOf} />;
};
