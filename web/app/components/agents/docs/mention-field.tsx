import { useEffect, useMemo, useRef, useState } from 'react';
import type { CSSProperties } from 'react';
import { ArrowUp } from 'lucide-react';
import { cx } from '@/components/agents/ui';
import { fetchDocPeople } from '@/lib/doc-threads-client';
import {
  activeMention,
  applyMention,
  encodeMentions,
  mentionRanges,
  splitMentions,
  stepIndex,
  stillMentioned,
} from '@/lib/doc-mentions';
import type { IMentionedPerson } from '@/lib/doc-mentions';
import type { IDocPerson } from '@/types/doc-threads';
import { t } from '@/lib/i18n';

const SEARCH_DEBOUNCE_MS = 120;
const LIST_ROOM = 220;

/** A comment's text with its mentions drawn as chips. */
export const MessageText = ({ body, className }: { body: string; className?: string }) => (
  <p className={className}>
    {splitMentions(body).map((segment, i) =>
      segment.kind === 'mention' ? (
        <span key={i} className="td-chip" title={segment.id}>
          @{segment.name}
        </span>
      ) : (
        <span key={i}>{segment.text}</span>
      ),
    )}
  </p>
);

interface IListPlace {
  left: number;
  width: number;
  top?: number;
  bottom?: number;
}

interface IPillProps {
  placeholder: string;
  label: string;
  autoFocus?: boolean;
  /** Receives the stored body: mentions already encoded as `@[Name](person:<email>)`. */
  onSubmit: (body: string) => Promise<void>;
  onCancel?: () => void;
}

/**
 * FigJam's input: one line that grows, the send arrow inside it, Enter sends, Esc cancels. Typing `@` at the
 * start of a word opens the list of people; a picked person is a chip behind its `@Name` in the field.
 */
export const Pill = ({ placeholder, label, autoFocus, onSubmit, onCancel }: IPillProps) => {
  const [text, setText] = useState('');
  const [picked, setPicked] = useState<IMentionedPerson[]>([]);
  const [caret, setCaret] = useState(0);
  const [sending, setSending] = useState(false);
  const [people, setPeople] = useState<{ query: string; list: IDocPerson[] } | null>(null);
  const [index, setIndex] = useState(0);
  const [dismissedAt, setDismissedAt] = useState<number | null>(null);
  const [place, setPlace] = useState<IListPlace | null>(null);
  const fieldRef = useRef<HTMLTextAreaElement | null>(null);
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const ready = text.trim().length > 0 && !sending;

  const match = useMemo(() => activeMention(text, caret), [text, caret]);
  const query = match?.query ?? null;

  useEffect(() => {
    if (query === null) return undefined;
    let cancelled = false;
    const timer = setTimeout(() => {
      fetchDocPeople(query).then(
        (result) => !cancelled && setPeople({ query, list: result.people }),
        () => !cancelled && setPeople({ query, list: [] }),
      );
    }, SEARCH_DEBOUNCE_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [query]);

  // The answer of an older query is not the list of this one.
  const list = match && people?.query === match.query && dismissedAt !== match.start ? people.list : [];
  const open = list.length > 0;
  const active = Math.min(index, Math.max(0, list.length - 1));

  const track = (el: HTMLTextAreaElement) => {
    setCaret(el.selectionStart);
    const box = wrapRef.current?.getBoundingClientRect();
    if (box)
      setPlace(
        box.top > LIST_ROOM
          ? { left: box.left, width: box.width, bottom: window.innerHeight - box.top + 6 }
          : { left: box.left, width: box.width, top: box.bottom + 6 },
      );
  };

  const choose = (person: IDocPerson) => {
    if (!match) return;
    const next = applyMention(text, caret, match, { id: person.id, name: person.name });
    setText(next.text);
    setCaret(next.caret);
    setPicked((current) =>
      current.some((p) => p.id === person.id) ? current : [...current, { id: person.id, name: person.name }],
    );
    setIndex(0);
    requestAnimationFrame(() => {
      fieldRef.current?.focus();
      fieldRef.current?.setSelectionRange(next.caret, next.caret);
    });
  };

  const submit = async () => {
    if (!ready) return;
    setSending(true);
    try {
      await onSubmit(encodeMentions(text.trim(), stillMentioned(text, picked)));
      setText('');
      setPicked([]);
    } finally {
      setSending(false);
    }
  };

  const chips = useMemo(() => {
    const out: { text: string; chip: boolean }[] = [];
    let at = 0;
    for (const range of mentionRanges(text, picked)) {
      if (range.start < at) continue;
      if (range.start > at) out.push({ text: text.slice(at, range.start), chip: false });
      out.push({ text: text.slice(range.start, range.end), chip: true });
      at = range.end;
    }
    out.push({ text: text.slice(at), chip: false });
    return out;
  }, [text, picked]);

  const listStyle: CSSProperties | undefined = place
    ? { left: place.left, width: place.width, top: place.top, bottom: place.bottom }
    : undefined;

  return (
    <div className="td-pill" ref={wrapRef}>
      <div className="td-pill-field">
        <div className="td-pill-mirror" aria-hidden="true">
          {chips.map((part, i) =>
            part.chip ? (
              <span key={i} className="td-chip-bg">
                {part.text}
              </span>
            ) : (
              <span key={i}>{part.text}</span>
            ),
          )}
          {'\n'}
        </div>
        <textarea
          ref={fieldRef}
          rows={1}
          autoFocus={autoFocus}
          value={text}
          placeholder={placeholder}
          aria-label={label}
          role="combobox"
          aria-expanded={open}
          aria-autocomplete="list"
          aria-controls={open ? 'td-mention-list' : undefined}
          onChange={(event) => {
            setText(event.target.value);
            setIndex(0);
            // A closed list stays closed for that `@`; leaving it, or starting a new one, lifts the dismissal.
            if (!activeMention(event.target.value, event.target.selectionStart)) setDismissedAt(null);
            track(event.target);
            event.target.style.height = 'auto';
            event.target.style.height = `${Math.min(event.target.scrollHeight, 132)}px`;
          }}
          onSelect={(event) => track(event.currentTarget)}
          onKeyDown={(event) => {
            if (open) {
              if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
                event.preventDefault();
                setIndex(stepIndex(active, event.key === 'ArrowDown' ? 1 : -1, list.length));
                return;
              }
              if (event.key === 'Enter' || event.key === 'Tab') {
                event.preventDefault();
                choose(list[active]);
                return;
              }
              if (event.key === 'Escape') {
                // Closes the list only; a second Esc cancels the comment.
                event.preventDefault();
                event.stopPropagation();
                setDismissedAt(match?.start ?? null);
                return;
              }
            }
            if (event.key === 'Escape') onCancel?.();
            if (event.key === 'Enter' && !event.shiftKey) {
              event.preventDefault();
              void submit();
            }
          }}
        />
      </div>
      <button
        type="button"
        className="td-send"
        data-ready={ready}
        disabled={!ready}
        onClick={() => void submit()}
        aria-label={t('docs.mentionField.send')}
      >
        <ArrowUp />
      </button>
      {open && (
        <ul
          id="td-mention-list"
          className="td-mention-list"
          role="listbox"
          aria-label={t('docs.mentionField.people')}
          style={listStyle}
        >
          {list.map((person, i) => (
            <li key={person.id} role="option" aria-selected={i === active}>
              <button
                type="button"
                className={cx('td-mention-item', i === active && 'is-active')}
                onMouseDown={(event) => event.preventDefault()}
                onMouseEnter={() => setIndex(i)}
                onClick={() => choose(person)}
              >
                <span className="td-mention-avatar" aria-hidden="true">
                  {person.initial}
                </span>
                <span className="td-mention-name">{person.name}</span>
                <span className="td-mention-id">{person.id}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
};
