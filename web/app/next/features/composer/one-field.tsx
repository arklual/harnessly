// One field for the whole window: a chat, a new chat, Settings, Sinopia's start and its island do not each
// mount a field of their own — they lend the one the window keeps. A screen still decides everything about it (the
// words, the files, the model, what sending does) and hands that to the field as props; the field itself stays mounted
// and its node moves into the screen's place, so it keeps its focus, its caret, an open menu and its size. It glides
// from the old place to the new one only from a start page into the screen its sending opens (the same tag in
// glideOut and glideIn: home → chat, Sinopia's start → its design); any other move (Sinopia, Settings or another chat
// into a chat) puts it in its place from the first frame. Fields outside the main area — the right pane's chat, a card
// in the feed — are their own, as before.
import {
  createContext,
  memo,
  useCallback,
  useContext,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from 'react';
import { createPortal } from 'react-dom';
import Composer, { type IComposerProps } from '@/next/features/composer/composer';
import { SteadyProps } from '@/next/features/composer/steady-props';
import { AttachmentSrc } from '@/next/ui/attachment-thumb';

type TSrcOf = React.ContextType<typeof AttachmentSrc>;

interface ILent {
  props: IComposerProps;
  srcOf: TSrcOf;
  /** The screen that lends the field. */
  of: object;
}

/** Who holds the field: the screens that lend it, the last mounted on top. */
const lenders: {
  token: object;
  glide: IGlide;
  lent: ILent;
  steady: SteadyProps<IComposerProps>;
  slot: HTMLElement | null;
}[] = [];
const listeners = new Set<() => void>();
let snapshot: ILent | null = null;
let host: HTMLDivElement | null = null;

const emit = () => {
  snapshot = lenders.at(-1)?.lent ?? null;
  for (const listener of listeners) listener();
};

const theHost = (): HTMLDivElement => {
  if (!host) {
    host = document.createElement('div');
    host.className = 'hx-onefield';
  }
  return host;
};

/** The field goes where the top screen keeps its place; focus inside it survives the move. */
const place = () => {
  const top = lenders.at(-1);
  if (!top?.slot) return;
  const node = theHost();
  if (node.parentElement === top.slot) return;
  const had = focusIn(node) ?? refocus;
  refocus = null;
  const from = leftAt && top.glide.in !== null && top.glide.in === leftAt.tag ? leftAt.rect : null;
  leftAt = null;
  top.slot.appendChild(node);
  had?.focus({ preventScroll: true });
  glide(node, from);
};

/** Where the field was when a start page left (glideOut): the next place glides from there if it takes it (glideIn). */
let leftAt: { rect: DOMRect | null; tag: string } | null = null;

const fieldRect = (node: HTMLElement): DOMRect | null => {
  const field = node.querySelector<HTMLElement>('.hx-composer');
  const rect = field?.getBoundingClientRect();
  return rect && rect.width > 0 && rect.height > 0 ? rect : null;
};

const GLIDE_MS = 320;

/**
 * The field moves from where it was to its new place (FLIP): only the field's own box moves, on the compositor, and the
 * screen around it changes at once — no picture of the page is taken and nothing waits for it (the view
 * transition this replaces held every route change for a frame and swallowed clicks while it ran).
 */
const glide = (node: HTMLElement, from: DOMRect | null) => {
  const field = node.querySelector<HTMLElement>('.hx-composer');
  const to = fieldRect(node);
  if (!field || !from || !to || typeof field.animate !== 'function') return;
  if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return;
  const dx = from.left - to.left;
  const dy = from.top - to.top;
  const sx = from.width / to.width;
  const sy = from.height / to.height;
  if (Math.abs(dx) < 2 && Math.abs(dy) < 2 && Math.abs(sx - 1) < 0.02 && Math.abs(sy - 1) < 0.02) return;
  const ease =
    getComputedStyle(document.documentElement).getPropertyValue('--hx-ease-out').trim() || 'ease-out';
  field.animate(
    [
      { transformOrigin: '0 0', transform: `translate(${dx}px, ${dy}px) scale(${sx}, ${sy})` },
      { transformOrigin: '0 0', transform: 'none' },
    ],
    { duration: GLIDE_MS, easing: ease },
  );
};

/** What had focus in the field when its screen left: the screen's node goes away with it, the next one gets it back. */
let refocus: HTMLElement | null = null;

const focusIn = (node: HTMLElement): HTMLElement | null => {
  const focused = document.activeElement;
  return focused instanceof HTMLElement && node.contains(focused) ? focused : null;
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => listeners.delete(listener);
};
const read = () => snapshot;
/** The screen holds the window's field: it is the top one, or about to be — mounted and not registered yet. */
const holding = (token: object) => {
  const at = lenders.findIndex((l) => l.token === token);
  return at < 0 || at === lenders.length - 1;
};

const lend = (token: object, props: IComposerProps, srcOf: TSrcOf, slot: HTMLElement | null, glide: IGlide) => {
  const me = lenders.find((l) => l.token === token);
  if (me) {
    // A screen that drew again with nothing new for the field (its clock, its handlers made anew) leaves it be.
    const moved = me.steady.update(props);
    if (!moved && me.lent.srcOf === srcOf) return;
    me.lent = { props: me.steady.value, srcOf, of: token };
    if (lenders.at(-1) === me) emit();
    return;
  }
  const steady = new SteadyProps(props);
  lenders.push({ token, glide, lent: { props: steady.value, srcOf, of: token }, steady, slot });
  emit();
  place();
};

const giveBack = (token: object) => {
  const at = lenders.findIndex((l) => l.token === token);
  if (at < 0) return;
  if (at === lenders.length - 1 && host) {
    refocus = focusIn(host);
    const out = lenders[at].glide.out;
    leftAt = out === null ? null : { rect: fieldRect(host), tag: out };
    // Only the screen that comes in the same commit takes it; later the focus is the person's again.
    queueMicrotask(() => {
      refocus = null;
      leftAt = null;
    });
  }
  lenders.splice(at, 1);
  emit();
  place();
};

/** The window's field; mounted once, by the frame, for as long as the window lives. */
export const OneFieldRoot = () => {
  const lent = useSyncExternalStore(subscribe, read, () => null);
  // What the person just typed, shown at once: the screen that owns the words hears of it in the same event, but its
  // new words reach this field only after the screen has drawn. Without it the field draws the old words in between
  // (the browser's text is set back and then forward, and the page is measured twice for every key — HRN-1362).
  const [echo, setEcho] = useState<string | null>(null);
  const [seen, setSeen] = useState<{ value: string; of: object | null }>({ value: '', of: null });
  const value = lent?.props.value ?? '';
  const of = lent?.of ?? null;
  if (seen.value !== value || seen.of !== of) {
    setSeen({ value, of });
    setEcho(null);
  }
  const onChange = lent?.props.onChange;
  const typed = useCallback(
    (text: string) => {
      setEcho(text);
      onChange?.(text);
      // A screen that kept its words as they were (a field it holds still) has drawn by now: they are shown again.
      queueMicrotask(() => setEcho(null));
    },
    [onChange],
  );
  if (typeof document === 'undefined') return null;
  return createPortal(
    <AttachmentSrc.Provider value={lent?.srcOf ?? noSrc}>
      {lent && <FieldComposer {...lent.props} value={echo ?? value} onChange={typed} />}
    </AttachmentSrc.Provider>,
    theHost(),
  );
};

const noSrc: TSrcOf = () => null;

/** The window's field draws only when what it shows changed: a screen catching up with a key leaves it be. */
const FieldComposer = memo(Composer);

/** The main area: a field in it lends the window's one. */
export const MainField = createContext(false);

export const MainFieldArea = ({ children }: { children: ReactNode }) => (
  <MainField.Provider value>{children}</MainField.Provider>
);

/** Which moves of the window's field glide: from this screen (a start page whose sending opens the next screen) and
 *  into it (the screen a start page's sending opens). */
interface IGlide {
  out: string | null;
  in: string | null;
}

interface IScreenFieldProps extends IComposerProps {
  /** A start page: the field glides from here into the screen with the same glideIn tag. */
  glideOut?: string;
  /** The screen a start page's sending opens: the field glides in from the start page with the same glideOut tag; from
   *  anywhere else it stands. */
  glideIn?: string;
}

/** A screen's place for the window's field, with what the field is for on this screen. */
const LentField = ({ glideOut, glideIn, ...props }: IScreenFieldProps) => {
  const srcOf = useContext(AttachmentSrc);
  const slot = useRef<HTMLDivElement>(null);
  const [token] = useState(() => ({}));
  // Before it is registered a screen is about to be the top one: no field of its own for a moment.
  const holds = useSyncExternalStore(
    subscribe,
    () => holding(token),
    () => true,
  );

  // Each render hands the field what this screen wants of it; the first one registers the screen as the top one.
  useLayoutEffect(() => lend(token, props, srcOf, slot.current, { out: glideOut ?? null, in: glideIn ?? null }));
  useLayoutEffect(() => () => giveBack(token), [token]);

  return (
    <div ref={slot} className="hx-onefield__slot">
      {/* Another screen of the main area holds the window's field: this one keeps a field of its own meanwhile. */}
      {!holds && <Composer {...props} />}
    </div>
  );
};

/** The field of a screen: the window's one in the main area, a field of its own anywhere else. */
const ScreenField = ({ glideOut, glideIn, ...props }: IScreenFieldProps) =>
  useContext(MainField) ? <LentField {...props} glideOut={glideOut} glideIn={glideIn} /> : <Composer {...props} />;

export default ScreenField;
