// Comments on the slides: the «Комментарии» tab (the list and «Решено»), the layer over the slide (pins, the capture while
// a place is being picked, the ring of a held finger), the bar «Нажмите на место» and the popover (a bottom sheet on a
// phone) where the comment is written. The state of picking is in ./comment-ui.ts; the list in ./use-deck-comments.ts.
import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
} from 'react';
import { createPortal } from 'react-dom';
import {
  closeDeckDraft,
  onDeckRing,
  openDeckDraft,
  stopDeckPick,
  useDeckPick,
  type IDeckDraft,
} from '@/next/features/designly/deck/comment-ui';
import { nextNumber, pinsOf, type IDeckCommentItem } from '@/next/features/designly/deck/comments-model';
import type { IPressPoint } from '@/next/features/designly/deck/press';
import type { IDeckComments } from '@/next/features/designly/deck/use-deck-comments';
import { useVars } from '@/next/features/designly/deck/use-vars';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { usePresence, PRESENCE } from '@/next/ui/presence';
import { toast } from '@/next/ui/toast';

/** The popover leaves in 160 ms on a wide window, the phone's sheet in 240 ms. */
const POP_OUT_MS = 160;
const SHEET_OUT_MS = 240;
/** The phone's sheet has risen by then: the field takes the focus without moving the sheet. */
const SHEET_FOCUS_MS = 380;
/** How long the board may take to say which block is under a point. */
const HIT_MS = 400;
const POP_W = 320;

// ---- the list

const Row = ({
  item,
  busy,
  onGo,
  onResolve,
}: {
  item: IDeckCommentItem;
  busy: boolean;
  onGo: (slide: number) => void;
  onResolve: (item: IDeckCommentItem) => void;
}) => (
  <div className="hx-deck-cm" data-done={item.resolved ? '1' : '0'} onClick={() => onGo(item.slide)}>
    <span className="hx-deck-cm__n">{item.n}</span>
    <div className="hx-deck-cm__b">
      <small>
        {t('next.deck.comments.who', { who: item.who ?? t('next.deck.comments.guest'), n: item.slide + 1 })}
      </small>
      {item.text}
    </div>
    <button
      className="hx-btn"
      data-variant="ghost"
      data-size="sm"
      data-shape="icon"
      type="button"
      aria-label={t('next.deck.comments.resolve')}
      aria-pressed={item.resolved}
      disabled={busy}
      onClick={(event) => {
        event.stopPropagation();
        onResolve(item);
      }}
    >
      <Icon name="check" size="sm" />
    </button>
  </div>
);

/** The list of the panel's third tab. `comments.items` null is the deck still being read. */
export const CommentsTab = ({
  comments,
  onGo,
}: {
  comments: IDeckComments;
  onGo: (slide: number) => void;
}) => {
  const [busy, setBusy] = useState<string | null>(null);
  const resolve = async (item: IDeckCommentItem) => {
    setBusy(item.key);
    await comments.resolve(item);
    setBusy(null);
  };
  const { items } = comments;
  return (
    <div className="hx-deck-pbody" role="tabpanel">
      {items?.map((item) => (
        <Row key={item.key} item={item} busy={busy === item.key} onGo={onGo} onResolve={resolve} />
      ))}
      {items?.length === 0 && (
        <div className="hx-deck-cm-empty">
          {t('next.deck.comments.empty')}
          <br />
          {t('next.deck.comments.emptyHint')}
        </div>
      )}
    </div>
  );
};

// ---- on the slide

const Pin = ({ x, y, n }: { x: number; y: number; n: number }) => {
  const ref = useVars<HTMLDivElement>({ '--x': `${x}%`, '--y': `${y}%` });
  return (
    <div ref={ref} className="hx-deck-pin">
      {n}
    </div>
  );
};

const clamp = (v: number, min: number, max: number) => Math.max(min, Math.min(max, v));

/** Which block the board has under a point (percent of the slide); '' when it did not say in time. */
const labelAt = async (
  hit: ((x: number, y: number) => Promise<string>) | undefined,
  x: number,
  y: number,
): Promise<string> => {
  if (!hit) return '';
  const late = new Promise<string>((done) => window.setTimeout(() => done(''), HIT_MS));
  return Promise.race([hit(x, y).catch(() => ''), late]);
};

/** The popover's place near the click, in the shell's pixels, kept inside it. */
const popAt = (root: HTMLElement, at: IDeckDraft['at']): { left: number; top: number } => {
  const rect = root.getBoundingClientRect();
  const k = rect.width / root.offsetWidth || 1;
  return {
    left: clamp((at.left - rect.left) / k + 18, 12, root.offsetWidth - POP_W - 12),
    top: clamp((at.top - rect.top) / k - 12, 64, root.offsetHeight - 210),
  };
};

interface IPopoverProps {
  root: HTMLElement;
  draft: IDeckDraft;
  closing: boolean;
  phone: boolean;
  /** 1-based number of the slide and of the comment being written. */
  slide: number;
  onSend: (text: string) => Promise<boolean>;
  onCancel: () => void;
}

const CommentPopover = ({ root, draft, closing, phone, slide, onSend, onCancel }: IPopoverProps) => {
  const [text, setText] = useState('');
  const [sending, setSending] = useState(false);
  const place = phone ? { left: 0, top: 0 } : popAt(root, draft.at);
  const ref = useVars<HTMLElement>({ '--l': `${place.left}px`, '--t': `${place.top}px` });
  const field = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    const timer = window.setTimeout(
      () => field.current?.focus({ preventScroll: true }),
      phone ? SHEET_FOCUS_MS : 0,
    );
    return () => window.clearTimeout(timer);
  }, [phone]);

  const send = async () => {
    if (sending || closing) return;
    setSending(true);
    const ok = await onSend(text.trim() === '' ? t('next.deck.comments.fallback') : text.trim());
    if (!ok) setSending(false);
  };
  const onKey = (event: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      onCancel();
    } else if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      void send();
    }
  };

  return createPortal(
    <section
      ref={ref}
      className="hx-deck-cpop"
      data-closing={closing ? '1' : '0'}
      role="dialog"
      aria-label={t('next.deck.comments.new')}
    >
      <div className="hx-deck-cpop__a">
        <Icon name="locate" size="sm" />
        {draft.label === ''
          ? t('next.deck.chat.context', { n: slide })
          : t('next.deck.comments.chip', { n: slide, label: draft.label })}
      </div>
      <textarea
        ref={field}
        value={text}
        placeholder={t('next.deck.comments.placeholder')}
        aria-label={t('next.deck.comments.text')}
        onChange={(event) => setText(event.target.value)}
        onKeyDown={onKey}
      />
      <div className="hx-deck-cpop__row">
        <button className="hx-btn" data-size="sm" type="button" onClick={onCancel}>
          {t('next.deck.comments.cancel')}
        </button>
        <button
          className="hx-btn"
          data-variant="primary"
          data-size="sm"
          type="button"
          disabled={sending}
          onClick={() => void send()}
        >
          {t('next.deck.comments.send')}
        </button>
      </div>
    </section>,
    root,
  );
};

const PickBar = ({
  root,
  closing,
  onCancel,
}: {
  root: HTMLElement;
  closing: boolean;
  onCancel: () => void;
}) =>
  createPortal(
    <div className="hx-deck-pickbar" data-closing={closing ? '1' : '0'} role="status">
      {t('next.deck.comments.pickBar')}
      <button type="button" onClick={onCancel}>
        {t('next.deck.comments.cancel')}
      </button>
    </div>,
    root,
  );

export interface IDeckCommentLayerProps {
  artifact: string;
  /** 0-based slide on screen. */
  slideIndex: number;
  comments: IDeckComments;
  /** Which block the board has under a point (percent of the slide): the popover's chip. Absent: no label. */
  hit?: (x: number, y: number) => Promise<string>;
  /** ≤560: a comment is a sheet, and a finger held on the slide leaves one. */
  phone: boolean;
  /** The comment went to the runtime: the shell shows the panel's «Комментарии». */
  onAdded: () => void;
  /** Where a comment is sent when it is not the team's own (a guest's, through the gateway); true when it went. */
  submit?: (slide: number, draft: { x: number; y: number; label: string; text: string }) => Promise<boolean>;
}

/**
 * Mount inside the slide's slot, over the board: it fills the slot. It draws the pins of the slide, takes the click that
 * picks a place while `useDeckPick(artifact).picking`, and rings a finger held on the board (`feedDeckRing`; the shell
 * opens the draft at the board's pick). The bar and the popover are drawn in the shell root (the closest `.hx-deck`).
 */
export const DeckCommentLayer = ({
  artifact,
  slideIndex,
  comments,
  hit,
  phone,
  onAdded,
  submit,
}: IDeckCommentLayerProps) => {
  const layer = useRef<HTMLDivElement>(null);
  const [root, setRoot] = useState<HTMLElement | null>(null);
  const [ring, setRing] = useState<IPressPoint | null>(null);
  const pick = useDeckPick(artifact);
  const draft = usePresence(pick.draft, phone ? SHEET_OUT_MS : POP_OUT_MS);
  const bar = usePresence(pick.picking ? true : null, POP_OUT_MS);

  useLayoutEffect(() => {
    setRoot(layer.current?.closest<HTMLElement>('.hx-deck') ?? null);
  }, []);

  // A finger held on the board is the board's gesture (phone, edit and link): its ring is drawn here where it says.
  useEffect(() => onDeckRing(artifact, setRing), [artifact]);

  const onPick = async (event: ReactMouseEvent<HTMLDivElement>) => {
    const el = layer.current;
    if (!el || !pick.picking) return;
    event.preventDefault();
    event.stopPropagation();
    const rect = el.getBoundingClientRect();
    const x = clamp(((event.clientX - rect.left) / rect.width) * 100, 0, 100);
    const y = clamp(((event.clientY - rect.top) / rect.height) * 100, 0, 100);
    const at = { left: event.clientX, top: event.clientY };
    const label = await labelAt(hit, x, y);
    openDeckDraft(artifact, { x, y, label, at });
  };

  const send = async (text: string): Promise<boolean> => {
    const d = draft.value;
    if (!d) return false;
    const made = { x: d.x, y: d.y, label: d.label, text };
    const ok = submit ? await submit(slideIndex, made) : await comments.add(slideIndex, made);
    if (!ok) return false;
    toast(t('next.deck.comments.added'), { tone: 'success' });
    closeDeckDraft(artifact);
    onAdded();
    return true;
  };

  const pins = comments.items ? pinsOf(comments.items, slideIndex) : [];
  const ringVars = useVars<HTMLDivElement>({ '--x': `${ring?.px ?? 0}%`, '--y': `${ring?.py ?? 0}%` });
  return (
    <div
      ref={layer}
      className="hx-deck-clayer"
      data-pick={pick.picking ? '1' : undefined}
      onClick={pick.picking ? (event) => void onPick(event) : undefined}
    >
      {pins.map((p) => (
        <Pin key={p.key} x={p.x ?? 0} y={p.y ?? 0} n={p.n} />
      ))}
      {draft.value && <Pin x={draft.value.x} y={draft.value.y} n={nextNumber(comments.items ?? [])} />}
      {ring && (
        <div ref={ringVars} className="hx-deck-press">
          <svg viewBox="0 0 64 64" aria-hidden="true">
            <circle className="track" cx="32" cy="32" r="28" />
            <circle className="fill" cx="32" cy="32" r="28" />
          </svg>
        </div>
      )}
      {root && bar.value && (
        <PickBar
          root={root}
          closing={bar.state === PRESENCE.closing}
          onCancel={() => stopDeckPick(artifact)}
        />
      )}
      {root && draft.value && (
        <CommentPopover
          key={`${draft.value.x},${draft.value.y}`}
          root={root}
          draft={draft.value}
          closing={draft.state === PRESENCE.closing}
          phone={phone}
          slide={slideIndex + 1}
          onSend={send}
          onCancel={() => closeDeckDraft(artifact)}
        />
      )}
    </div>
  );
};
