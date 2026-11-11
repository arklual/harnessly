// Annotations of the agent's answers, in the language of the pane's browser (ui/notes/notes.tsx). Picked words get
// one compact button over them, «Annotate»; a press puts a numbered mark at the end of the words, keeps them marked
// (the CSS highlight hx-feed-note) and opens the row of the comment beside the mark: Enter puts the annotation into the
// field as its own chip, Esc or a press elsewhere takes a fresh mark back. The marks stand while their annotations are
// in the field; a press on one opens its row again, a press on its chip in the field brings its words into view. The
// words are kept by their turn and their place in its text (feed-anchor.ts): the feed draws only the turns near the
// screen and redraws a turn as it grows. A finger has no mouseup, so on touch the button waits until the handles stop.
// Where the chat has a side chat, «Ask aside» stands beside «Annotate»: the words go to the side chat's field as a quote
// (stores/side-ask.ts), the agent of the chat is not disturbed.
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createStore, useStore } from '@/next/core/store';
import { anchorOf, rangeOf, stillHolds, type IFeedAnchor } from '@/next/features/chat/feed-anchor';
import { t } from '@/next/i18n';
import type { ConvId } from '@/next/protocol/v2';
import {
  addFeedNote,
  clearFeedNotes,
  editQuote,
  feedNotesOf,
  nextFeedNoteId,
  removeQuote,
  useQuotes,
} from '@/next/stores/quotes';
import { revealMessage } from '@/next/stores/reveal';
import { askBeside } from '@/next/stores/side-ask';
import AnnotatePill from '@/next/ui/notes/pill';
import type { INoteChip } from '@/next/ui/notes/note-chips';
import {
  clipQuote,
  NOTE_OUT_MS,
  NOTE_ROW_H,
  NOTE_ROW_W,
  NoteBar,
  NoteBarButton,
  NoteMark,
} from '@/next/ui/notes/notes';
import type { IAnnotateSpot } from '@/next/ui/notes/place';
import { useLeavingList } from '@/next/ui/presence';
import '@/next/styles/feed-notes.css';

const TOUCH_SETTLE_MS = 380;
const HIGHLIGHT = 'hx-feed-note';
const FLASH_MS = 900;
/** The row is this far left of its dot; it never leaves the screen by more than the margin. */
const ROW_LEAD = 14;
const EDGE = 8;
/** Tries to bring the words of a turn the feed has not drawn yet: the feed moves its window to the turn first. */
const SHOW_TRIES = 6;
const SHOW_RETRY_MS = 160;

interface IPicked extends IAnnotateSpot {
  quote: string;
  anchor: IFeedAnchor;
}

interface IFeedMark {
  id: string;
  quote: string;
  comment: string;
  anchor: IFeedAnchor;
  /** Just put by a press: leaving it without Enter takes it back. */
  fresh: boolean;
}

const coarse = () => window.matchMedia('(pointer: coarse)').matches;

const spotOf = (range: Range): IAnnotateSpot => {
  const r = range.getBoundingClientRect();
  return { x: Math.round(r.left + r.width / 2), top: Math.round(r.top), bottom: Math.round(r.bottom) };
};

const pickedNow = (): IPicked | null => {
  const sel = window.getSelection();
  if (!sel || sel.isCollapsed || sel.rangeCount === 0) return null;
  const text = sel.toString().replace(/\s+/g, ' ').trim();
  const range = sel.getRangeAt(0).cloneRange();
  const node = range.commonAncestorContainer;
  const el = node.nodeType === 1 ? (node as Element) : node.parentElement;
  if (
    text.length < 2 ||
    !el?.closest('.hx-thread') ||
    el.closest('.hx-msg-user, textarea, input, .hx-note__pill, .hx-fnotes')
  )
    return null;
  const anchor = anchorOf(range);
  if (!anchor) return null;
  return { quote: text, anchor, ...spotOf(range) };
};

/** A press on a chip in the field: its words come into view and their mark calls itself out. */
const showAsk = createStore<{ conv: ConvId; id: string; nonce: number } | null>(null);
let asked = 0;
export const showFeedNote = (conv: ConvId, id: string): void => {
  asked += 1;
  showAsk.set({ conv, id, nonce: asked });
};

/** The highlight of the marked words, one for all the marks. */
const highlight = (): Highlight | null => {
  if (typeof CSS === 'undefined' || !('highlights' in CSS) || typeof Highlight === 'undefined') return null;
  const have = CSS.highlights.get(HIGHLIGHT);
  if (have) return have;
  const made = new Highlight();
  CSS.highlights.set(HIGHLIGHT, made);
  return made;
};

/** The part of the window the feed shows: under the chat's head, over its field. */
const shownBox = (row: Element): { top: number; bottom: number } => {
  const chat = row.closest('.hx-chat');
  const feed = chat?.querySelector('.hx-feed')?.getBoundingClientRect();
  const dock = chat?.querySelector('.hx-dock')?.getBoundingClientRect();
  return { top: feed?.top ?? 0, bottom: Math.min(feed?.bottom ?? window.innerHeight, dock?.top ?? Infinity) };
};

/** Where the mark of the words stands: at the end of their last line. */
const placeMark = (el: HTMLElement, range: Range | null) => {
  const rects = range?.getClientRects();
  const last = rects && rects.length > 0 ? rects[rects.length - 1] : null;
  const row = range?.startContainer.parentElement;
  if (!range || !last || !row) {
    el.dataset.place = 'hidden';
    return;
  }
  const box = shownBox(row);
  const x = Math.round(last.right + 6);
  const y = Math.round(last.top + 2);
  el.dataset.place = y < box.top || y > box.bottom ? 'hidden' : 'shown';
  el.dataset.up = y + NOTE_ROW_H > box.bottom ? 'true' : 'false';
  el.dataset.flip = 'right';
  // A row that would run past either edge of the window slides back in (a phone is narrower than the row).
  const width = Math.min(NOTE_ROW_W, window.innerWidth - 2 * EDGE);
  const left = x - ROW_LEAD;
  let dx = Math.min(0, window.innerWidth - EDGE - width - left);
  if (left + dx < EDGE) dx = EDGE - left;
  el.style.setProperty('--x', `${x}px`);
  el.style.setProperty('--y', `${y}px`);
  el.style.setProperty('--note-row-dx', `${Math.round(dx)}px`);
};

const markId = (mark: IFeedMark): string => mark.id;

/** The feed's annotations as the field's chips: the words in guillemets, the card holds them whole. */
export const useFeedNoteChips = (conv: ConvId): INoteChip[] => {
  const quotes = useQuotes(conv);
  return useMemo(
    () =>
      feedNotesOf(quotes).map((q) => ({
        id: q.id,
        name: `«${clipQuote(q.text, 40)}»`,
        comment: q.note ?? '',
        facts: [{ label: t('next.annotate.words'), value: clipQuote(q.text, 240), title: q.text }],
      })),
    [quotes],
  );
};

/** «Аннотирование · N» over the field: «Очистить» and «Отправить все» (the field's own send). */
export const FeedNotesBar = ({
  conv,
  count,
  onSend,
}: {
  conv: ConvId;
  count: number;
  onSend: () => void;
}) => (
  <NoteBar
    className="hx-fnotes__bar"
    visible={count > 0}
    count={count}
    sendLabel={t('next.annotate.sendAll')}
    onSend={onSend}
    tools={
      <NoteBarButton icon="trash-2" label={t('next.annotate.clear')} onClick={() => clearFeedNotes(conv)} />
    }
  />
);

interface ISelBarProps {
  conv: ConvId;
  /** The chat has a side chat (a Claude chat): the picked words can be asked about there. */
  aside?: boolean;
}

const SelBar = ({ conv, aside = false }: ISelBarProps) => {
  const quotes = useQuotes(conv);
  const [picked, setPicked] = useState<IPicked | null>(null);
  const [fresh, setFresh] = useState<IFeedMark | null>(null);
  const [editing, setEditing] = useState<string | null>(null);

  const marks = useMemo<IFeedMark[]>(() => {
    const kept = feedNotesOf(quotes).flatMap((q) =>
      q.anchor ? [{ id: q.id, quote: q.text, comment: q.note ?? '', anchor: q.anchor, fresh: false }] : [],
    );
    return fresh ? [...kept, fresh] : kept;
  }, [quotes, fresh]);
  const shown = useLeavingList(marks, markId, NOTE_OUT_MS);

  // A mark that is leaving keeps the number it had.
  const current = marks.map((m, i) => `${m.id}:${i + 1}`).join(' ');
  const [numbers, setNumbers] = useState(new Map<string, number>());
  const [seen, setSeen] = useState('');
  if (seen !== current) {
    setSeen(current);
    setNumbers((was) => new Map([...was, ...marks.map((m, i) => [m.id, i + 1] as const)]));
  }

  // The words picked now: the button over them.
  useEffect(() => {
    let settle = 0;
    const look = () => setPicked(pickedNow());
    const up = (event: Event) => {
      if (event instanceof globalThis.KeyboardEvent && !event.shiftKey) return;
      if ((event.target as Element | null)?.closest?.('.hx-note__pill, .hx-fnotes')) return;
      window.setTimeout(look, 0);
    };
    const changed = () => {
      if (!coarse()) return;
      window.clearTimeout(settle);
      settle = window.setTimeout(look, TOUCH_SETTLE_MS);
    };
    // The feed scrolls under the button: it follows the picked words.
    const scrolled = (event: Event) => {
      if ((event.target as Element).closest?.('.hx-feed')) setPicked((have) => (have ? pickedNow() : have));
    };
    document.addEventListener('mouseup', up);
    document.addEventListener('keyup', up);
    document.addEventListener('selectionchange', changed);
    document.addEventListener('scroll', scrolled, true);
    return () => {
      window.clearTimeout(settle);
      document.removeEventListener('mouseup', up);
      document.removeEventListener('keyup', up);
      document.removeEventListener('selectionchange', changed);
      document.removeEventListener('scroll', scrolled, true);
    };
  }, []);

  // The marks follow their words: on every scroll, resize and change of the feed, at most once a frame.
  const els = useRef(new Map<string, HTMLDivElement>());
  const ranges = useRef(new Map<string, Range>());
  const drawn = useRef(shown);
  useLayoutEffect(() => {
    drawn.current = shown;
  });
  const draw = useCallback(() => {
    const marked: Range[] = [];
    for (const { item, leaving } of drawn.current) {
      const el = els.current.get(item.id);
      if (!el) continue;
      const kept = ranges.current.get(item.id);
      const range = kept && stillHolds(kept, item.anchor) ? kept : rangeOf(item.anchor);
      if (range) ranges.current.set(item.id, range);
      else ranges.current.delete(item.id);
      placeMark(el, range);
      if (range && !leaving) marked.push(range);
    }
    const mark = highlight();
    if (mark) {
      mark.clear();
      for (const range of marked) mark.add(range);
    }
  }, []);
  const ids = shown.map(({ item, leaving }) => `${item.id}${leaving ? '-' : ''}`).join(' ');
  useLayoutEffect(() => {
    draw();
    if (ids === '') return;
    let frame = 0;
    const later = () => {
      if (frame === 0)
        frame = requestAnimationFrame(() => {
          frame = 0;
          draw();
        });
    };
    const feed = document.querySelector('.hx-feed');
    const changes = new MutationObserver(later);
    if (feed) changes.observe(feed, { subtree: true, childList: true, characterData: true });
    document.addEventListener('scroll', later, true);
    window.addEventListener('resize', later);
    return () => {
      cancelAnimationFrame(frame);
      changes.disconnect();
      document.removeEventListener('scroll', later, true);
      window.removeEventListener('resize', later);
    };
  }, [ids, draw]);
  useEffect(() => () => highlight()?.clear(), []);

  // A press on a chip: the words into view (the feed first draws their turn when it is far), the mark flashes.
  const ask = useStore(showAsk);
  const known = useRef(marks);
  const showTimer = useRef(0);
  useLayoutEffect(() => {
    known.current = marks;
  });
  useEffect(() => {
    if (ask === null || ask.conv !== conv) return;
    showAsk.set(null);
    const mark = known.current.find((m) => m.id === ask.id);
    if (!mark) return;
    window.clearTimeout(showTimer.current);
    let tries = 0;
    const show = () => {
      const range = rangeOf(mark.anchor);
      if (!range) {
        if (tries === 0) revealMessage({ conv, entryId: mark.anchor.key, turnId: mark.anchor.key });
        tries += 1;
        if (tries < SHOW_TRIES) showTimer.current = window.setTimeout(show, SHOW_RETRY_MS);
        return;
      }
      range.startContainer.parentElement?.scrollIntoView({ block: 'center', behavior: 'smooth' });
      const el = els.current.get(mark.id);
      if (!el) return;
      el.removeAttribute('data-flash');
      // A new frame between, so a second press plays the flash again.
      requestAnimationFrame(() => el.setAttribute('data-flash', ''));
      showTimer.current = window.setTimeout(() => el.removeAttribute('data-flash'), FLASH_MS);
    };
    show();
  }, [ask, conv]);
  useEffect(() => {
    const timer = showTimer;
    return () => window.clearTimeout(timer.current);
  }, []);

  // A press outside the open row (or Esc in it) closes it; a fresh mark goes with it.
  const close = useCallback(() => {
    setEditing(null);
    setFresh(null);
  }, []);
  useEffect(() => {
    if (editing === null) return;
    const away = (event: PointerEvent) => {
      if (!(event.target as Element | null)?.closest?.('.hx-fnotes .hx-note__mark[data-editing]')) close();
    };
    document.addEventListener('pointerdown', away, true);
    return () => document.removeEventListener('pointerdown', away, true);
  }, [editing, close]);

  const press = useCallback(() => {
    if (!picked) return;
    const id = nextFeedNoteId();
    setFresh({ id, quote: picked.quote, comment: '', anchor: picked.anchor, fresh: true });
    setEditing(id);
    setPicked(null);
    window.getSelection()?.removeAllRanges();
  }, [picked]);
  const pressAside = useCallback(() => {
    if (!picked) return;
    askBeside(conv, picked.quote);
    setPicked(null);
    window.getSelection()?.removeAllRanges();
  }, [picked, conv]);
  const dismiss = useCallback(() => setPicked(null), []);

  const commit = (mark: IFeedMark, comment: string) => {
    if (mark.fresh)
      addFeedNote(conv, {
        id: mark.id,
        text: mark.quote,
        note: comment === '' ? null : comment,
        anchor: mark.anchor,
      });
    else editQuote(conv, mark.id, comment);
    setFresh(null);
    setEditing(null);
  };
  const remove = (mark: IFeedMark) => {
    if (!mark.fresh) removeQuote(conv, mark.id);
    close();
  };

  return (
    <>
      <AnnotatePill
        target={picked}
        preferBelow={coarse()}
        onPress={press}
        onAside={aside ? pressAside : undefined}
        keys
        onDismiss={dismiss}
      />
      {shown.length > 0 && (
        <div className="hx-fnotes">
          {shown.map(({ item, leaving }) => {
            const n = leaving ? (numbers.get(item.id) ?? 0) : marks.findIndex((m) => m.id === item.id) + 1;
            return (
              <NoteMark
                key={item.id}
                markRef={(el) => {
                  if (el) els.current.set(item.id, el);
                  else els.current.delete(item.id);
                }}
                n={n}
                label={t('next.pane.annot.mark', { n, name: `«${clipQuote(item.quote, 60)}»` })}
                comment={item.comment}
                editing={!leaving && editing === item.id}
                leaving={leaving}
                rowLabel={t('next.annotate.label')}
                placeholder={t('next.annotate.placeholder')}
                onOpen={() => {
                  if (fresh && fresh.id !== item.id) setFresh(null);
                  setEditing(editing === item.id ? null : item.id);
                }}
                onCommit={(comment) => commit(item, comment)}
                onRemove={() => remove(item)}
                onCancel={close}
              />
            );
          })}
        </div>
      )}
    </>
  );
};

export default SelBar;
