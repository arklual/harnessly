// Under the slide (the board's .pr-subs and .pr-pager): the speaker's notes as two lines of subtitles, edited in place
// on a wide window (a click opens the whole text, leaving it saves) and a button that opens the notes sheet on a phone;
// the meta line under them (length, «из диктовки», the mic, «все заметки»), and the pager with the switch that hides
// the notes. While a dictation listens the line shows what is heard.
import { useState } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { toast } from '@/next/ui/toast';
import type { IDeckSlide } from '@/next/features/designly/deck/types';

/** A dictation into one slide's notes (WP4's dictation.ts drives it). */
export interface IDeckSubsDictation {
  /** The slide it listens for; null when none listens. */
  page: string | null;
  /** What was heard so far; '' before the first words. */
  said: string;
  onMic: (page: string) => void;
  /** The machine can recognize speech here; without it no mic is offered. */
  available: boolean;
}

export interface IDeckNotesLineProps {
  /** null while the deck is not read yet. */
  slide: IDeckSlide | null;
  n: number;
  phone: boolean;
  dictation: IDeckSubsDictation;
  /** Saves the notes; a refusal is thrown with its reason. */
  onSave: (text: string) => Promise<void>;
  onAll: () => void;
  onSheet: () => void;
}

/** The text of an edited line: its words with the line breaks kept, blanks at the ends dropped. */
const textOf = (el: HTMLElement) => el.innerText.replace(/\u00a0/g, ' ').trim();

export const DeckNotesLine = ({
  slide,
  n,
  phone,
  dictation,
  onSave,
  onAll,
  onSheet,
}: IDeckNotesLineProps) => {
  // A save that is under way shows its text; the line takes the saved notes back when it lands or fails.
  const [saving, setSaving] = useState<string | null>(null);
  const [round, setRound] = useState(0);
  if (slide === null) return <div className="hx-deck-subs" data-loading="1" aria-busy="true" />;
  const listening = dictation.page === slide.id;
  const text = saving ?? slide.notes;
  const save = (el: HTMLElement) => {
    const next = textOf(el);
    if (next === slide.notes) return;
    setSaving(next);
    onSave(next).then(
      () => setSaving(null),
      (failure: Error) => {
        setSaving(null);
        setRound((r) => r + 1);
        toast(t('next.deck.subs.saveFailed'), { tone: 'error', description: failure.message });
      },
    );
  };
  return (
    <div className="hx-deck-subs" data-live={listening ? '1' : '0'}>
      {listening && (
        <div className="hx-deck-subs__t">
          {dictation.said === '' ? t('next.deck.subs.listening') : dictation.said}
        </div>
      )}
      {!listening && phone && (
        <button
          type="button"
          className="hx-deck-subs__t hx-deck-subs__btn"
          data-empty={text === '' ? '1' : undefined}
          data-ph={t('next.deck.subs.empty')}
          aria-label={t('next.deck.subs.edit', { n })}
          onClick={onSheet}
        >
          {text}
        </button>
      )}
      {!listening && !phone && (
        <div
          key={`${slide.id}:${round}`}
          className="hx-deck-subs__t"
          contentEditable="plaintext-only"
          suppressContentEditableWarning
          role="textbox"
          aria-multiline="true"
          aria-label={t('next.deck.subs.label', { n })}
          data-ph={t('next.deck.subs.empty')}
          aria-busy={saving !== null || undefined}
          onBlur={(event) => save(event.currentTarget)}
          onKeyDown={(event) => {
            if (event.key === 'Escape') event.currentTarget.blur();
          }}
        >
          {text}
        </div>
      )}
      <div className="hx-deck-subs__meta">
        <span>{t('next.deck.subs.meta', { s: slide.seconds })}</span>
        {slide.voice && <span>{t('next.deck.subs.fromVoice')}</span>}
        {dictation.available && (
          <button
            type="button"
            className="hx-deck-mic"
            data-on={listening ? '1' : '0'}
            aria-label={t('next.deck.subs.mic')}
            aria-pressed={listening}
            onClick={() => dictation.onMic(slide.id)}
          >
            <Icon name={listening ? 'square' : 'mic'} size="sm" />
          </button>
        )}
        <button type="button" className="hx-deck-subs__lnk" onClick={onAll}>
          {t('next.deck.subs.all')}
        </button>
      </div>
    </div>
  );
};

export interface IDeckPagerProps {
  n: number;
  total: number;
  onPrev: () => void;
  onNext: () => void;
  /** Absent: no switch (a guest's link). */
  notes?: { on: boolean; onToggle: () => void };
}

export const DeckPager = ({ n, total, onPrev, onNext, notes }: IDeckPagerProps) => (
  <div className="hx-deck-pager">
    <button
      type="button"
      className="hx-btn"
      data-variant="ghost"
      data-size="sm"
      data-shape="icon"
      aria-label={t('next.deck.pager.prev')}
      disabled={n <= 1}
      onClick={onPrev}
    >
      <Icon name="chevron-left" />
    </button>
    <span>{t('next.deck.pager.count', { n, total })}</span>
    <button
      type="button"
      className="hx-btn"
      data-variant="ghost"
      data-size="sm"
      data-shape="icon"
      aria-label={t('next.deck.pager.next')}
      disabled={n >= total}
      onClick={onNext}
    >
      <Icon name="chevron-right" />
    </button>
    {notes && (
      <button
        type="button"
        className="hx-deck-subs-tg"
        data-on={notes.on ? '1' : '0'}
        aria-pressed={notes.on}
        aria-label={t('next.deck.pager.toggle')}
        onClick={notes.onToggle}
      >
        <Icon name="file-text" size="sm" />
        <span>{notes.on ? t('next.deck.pager.hide') : t('next.deck.pager.show')}</span>
      </button>
    )}
  </div>
);
