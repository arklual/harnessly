// The panel's «Заметки» tab: the speech by slide. A card per slide (number, title, «~N с», «из диктовки», the mic) with
// the text editable in place and saved when the focus leaves it; the card of the slide on screen is lit; a click on a
// card selects the slide. «Надиктовать всю презентацию» lets the model split a dictation by slide.
import { useLayoutEffect, useRef } from 'react';
import { RuntimeError } from '@/next/core/net/runtime-json';
import { putDeckNotes } from '@/next/features/designly/deck/api';
import type { IDeckDictation } from '@/next/features/designly/deck/dictation';
import { DICT } from '@/next/features/designly/deck/dictation-state';
import type { IDeckSlide } from '@/next/features/designly/deck/types';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { putDeckSlide, useDeck } from '@/next/stores/deck';
import Icon from '@/next/ui/icon';
import { toast } from '@/next/ui/toast';

/** The four bars beside the words while the machine listens. */
export const DeckWave = () => (
  <span className="hx-deck-wave" aria-hidden="true">
    <i />
    <i />
    <i />
    <i />
  </span>
);

/** The round mic: grey, red and pulsing while it listens (then it is a stop). */
export const DeckMic = ({
  on,
  label,
  onClick,
  disabled = false,
}: {
  on: boolean;
  label: string;
  onClick: () => void;
  /** The notes are being made of the last dictation. */
  disabled?: boolean;
}) => (
  <button
    className="hx-deck-mic"
    type="button"
    data-on={on ? '1' : '0'}
    aria-label={label}
    aria-pressed={on}
    disabled={disabled}
    onClick={(event) => {
      event.stopPropagation();
      onClick();
    }}
  >
    <Icon name={on ? 'square' : 'mic'} size="sm" />
  </button>
);

/** Saves a slide's notes; true when the runtime took them. Pessimistic: the deck changes with its answer. */
export const saveNotes = async (
  machine: MachineId,
  artifact: string,
  slide: IDeckSlide,
  text: string,
): Promise<boolean> => {
  if (text === slide.notes) return true;
  try {
    putDeckSlide(artifact, await putDeckNotes(machine, artifact, slide.id, text));
    return true;
  } catch (failure) {
    toast(failure instanceof RuntimeError ? failure.message : t('next.failure.http'), { tone: 'error' });
    return false;
  }
};

/** A paragraph edited in place; `onSave` runs on blur with the text when it changed. */
const Editable = ({ value, onSave }: { value: string; onSave: (text: string) => void }) => {
  const ref = useRef<HTMLParagraphElement>(null);
  // What the deck holds is shown unless the person is typing: their words stay until the focus leaves.
  useLayoutEffect(() => {
    const el = ref.current;
    if (el && document.activeElement !== el && el.innerText !== value) el.innerText = value;
  });
  return (
    <p
      ref={ref}
      contentEditable="plaintext-only"
      suppressContentEditableWarning
      onClick={(event) => event.stopPropagation()}
      onBlur={(event) => {
        const text = event.currentTarget.innerText.trim();
        if (text !== value) onSave(text);
      }}
    />
  );
};

interface INotesTabProps {
  machine: MachineId;
  artifact: string;
  /** 0-based. */
  slideIndex: number;
  onGo: (index: number) => void;
  dict: IDeckDictation;
}

export const NotesTab = ({ machine, artifact, slideIndex, onGo, dict }: INotesTabProps) => {
  const deck = useDeck(artifact);
  const slides = deck?.slides ?? null;
  const all = { kind: 'all' } as const;
  const listening = dict.state.phase === DICT.listening;

  return (
    <div className="hx-deck-pbody" role="tabpanel">
      {dict.available && (
        <button
          className="hx-deck-all"
          type="button"
          disabled={dict.state.phase !== DICT.idle}
          onClick={() => {
            onGo(0);
            dict.start(all, t('next.deck.notes.doneAllToast'));
            toast(t('next.deck.notes.dictateAllToast'));
          }}
        >
          <Icon name="mic" size="sm" />
          <span>{t('next.deck.notes.dictateAll')}</span>
        </button>
      )}
      {slides?.map((slide, i) => {
        const target = { kind: 'slide', page: slide.id } as const;
        const mine = dict.on(target) || (dict.on(all) && i === 0 && listening);
        const live = mine && listening;
        return (
          <div
            key={slide.id}
            className="hx-deck-tx"
            data-on={i === slideIndex ? '1' : '0'}
            data-dict={live ? '1' : '0'}
            onClick={() => onGo(i)}
          >
            <div className="hx-deck-tx__h">
              <b>{i + 1}</b>
              <span>{slide.title}</span>
              {t('next.deck.notes.seconds', { s: slide.seconds })}
              {slide.voice && (
                <span className="hx-deck-from-voice">
                  <Icon name="sparkles" size="sm" />
                  {t('next.deck.notes.fromVoice')}
                </span>
              )}
              {dict.available && (
                <DeckMic
                  on={live}
                  disabled={dict.state.phase === DICT.tidying}
                  label={t('next.deck.notes.mic', { n: i + 1 })}
                  onClick={() => {
                    onGo(i);
                    dict.toggle(target, t('next.deck.notes.doneToast', { n: i + 1 }));
                  }}
                />
              )}
            </div>
            {live ? (
              <p>
                <b>{dict.said === '' ? t('next.deck.notes.listening') : dict.said}</b>
              </p>
            ) : (
              <Editable
                value={slide.notes}
                onSave={(text) => {
                  void saveNotes(machine, artifact, slide, text);
                }}
              />
            )}
          </div>
        );
      })}
    </div>
  );
};
