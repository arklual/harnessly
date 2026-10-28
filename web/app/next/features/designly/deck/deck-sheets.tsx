// The phone's sheets over the editor, one component per sheet: the edit sheet of a tapped text (the shell's) and the
// notes sheet (the panel's).
//
// EditSheet (the board's .pr-esheet): a tap on text of the slide opens it; the slide rises above it (`--esh`, the
// shell's data-es). It has the text's tools in a row and a field to ask the agent about the block, by voice too.
//
// NotesSheet: the line under the slide is a button that opens it. «Слайд N · ~S с», the text in a 16 px field (the
// dictation while it listens), «Надиктовать» / «Остановить» and «✦ Короче». It rises over the slide, which moves up
// (`--esh`).
import { useState } from 'react';
import { saveNotes } from '@/next/features/designly/deck/deck-notes';
import type { IDeckDictation } from '@/next/features/designly/deck/dictation';
import { DICT } from '@/next/features/designly/deck/dictation-state';
import type { IDeckSlide } from '@/next/features/designly/deck/types';
import { deckNotesSeconds } from '@/next/features/designly/deck/notes-len';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import { PRESENCE, type TPresenceState, usePresence } from '@/next/ui/presence';
import { useVoice } from '@/next/bindings/use-voice';
import { DeckTextTools, type IDeckTextToolsProps } from '@/next/features/designly/deck/deck-toolbar';
import { toast } from '@/next/ui/toast';

/** The sheet's exit, ms (the board's 240 ms slide down). */
export const DECK_SHEET_EXIT_MS = 240;

export interface IEditSheetProps extends IDeckTextToolsProps {
  state: TPresenceState;
  machine: MachineId;
  n: number;
  onDone: () => void;
  /** Sends the words to the agent about the block; absent where the agent cannot be asked. */
  onAsk?: (text: string) => void;
}

export const EditSheet = ({ state, machine, n, onDone, onAsk, ...tools }: IEditSheetProps) => {
  const [text, setText] = useState('');
  const voice = useVoice(machine, text, setText);
  const send = () => {
    const words = text.trim();
    if (words === '' || !onAsk) return;
    onAsk(words);
    setText('');
  };
  return (
    <section
      className="hx-deck-esheet"
      data-closing={state === PRESENCE.closing ? '1' : undefined}
      role="dialog"
      aria-label={t('next.deck.sheets.editLabel', { label: tools.sel.label })}
    >
      <div className="hx-deck-grab" />
      <div className="hx-deck-es__h">
        <b>{tools.sel.label}</b>
        <span>{t('next.deck.stage.slide', { n })}</span>
        <button type="button" className="hx-btn" data-variant="primary" data-size="sm" onClick={onDone}>
          <span className="hx-btn__text">{t('next.deck.sheets.done')}</span>
        </button>
      </div>
      <div className="hx-deck-es__row">
        <DeckTextTools {...tools} sheet />
      </div>
      <form
        className="hx-deck-es__ask"
        onSubmit={(event) => {
          event.preventDefault();
          send();
        }}
      >
        <input
          type="text"
          value={text}
          placeholder={t('next.deck.sheets.input')}
          aria-label={t('next.deck.mdock.askLabel')}
          enterKeyHint="send"
          onChange={(event) => setText(event.target.value)}
        />
        {(voice.onMic || voice.recording) && (
          <button
            type="button"
            className="hx-deck-foot-mic"
            data-on={voice.recording ? '1' : '0'}
            aria-label={t('next.deck.sheets.mic')}
            aria-pressed={voice.recording !== null}
            onClick={voice.recording ? voice.onFinishRecording : voice.onMic}
          >
            <Icon name={voice.recording ? 'square' : 'mic'} size="sm" />
          </button>
        )}
        <button
          type="submit"
          className="hx-sidechat__send"
          aria-label={t('next.deck.sheets.send')}
          disabled={!onAsk || text.trim() === ''}
        >
          <Icon name="arrow-up" size="sm" />
        </button>
      </form>
    </section>
  );
};

export interface INotesSheetProps {
  machine: MachineId;
  artifact: string;
  /** The slide on screen; null while the deck is read. */
  slide: IDeckSlide | null;
  /** 0-based. */
  slideIndex: number;
  open: boolean;
  onClose: () => void;
  dict: IDeckDictation;
  /** The site stage's `ask`: «Короче» sends it. */
  ask?: (text: string) => void;
}

const Body = ({
  machine,
  artifact,
  slide,
  slideIndex,
  closing,
  onClose,
  dict,
  ask,
}: Omit<INotesSheetProps, 'open' | 'slide'> & { slide: IDeckSlide; closing: boolean }) => {
  const [text, setText] = useState(slide.notes);
  const [saving, setSaving] = useState(false);
  const target = { kind: 'slide', page: slide.id } as const;
  const listening = dict.state.phase === DICT.listening && dict.on(target);
  const tidying = dict.state.phase === DICT.tidying && dict.on(target);

  // The notes the dictation made replace the text in the field; what the person types stays theirs.
  const [seen, setSeen] = useState(slide.notes);
  if (slide.notes !== seen) {
    setSeen(slide.notes);
    setText(slide.notes);
  }

  const done = async () => {
    if (saving) return;
    setSaving(true);
    const ok = await saveNotes(machine, artifact, slide, text.trim());
    setSaving(false);
    if (ok) onClose();
  };

  return (
    <section
      className="hx-deck-nsheet"
      data-closing={closing ? '1' : '0'}
      role="dialog"
      aria-label={t('next.deck.sheets.notesLabel', { n: slideIndex + 1 })}
    >
      <div className="hx-deck-grab" />
      <div className="hx-deck-nsheet__h">
        <b>{t('next.deck.sheets.notes')}</b>
        <span>{t('next.deck.sheets.notesMeta', { n: slideIndex + 1, s: deckNotesSeconds(text) })}</span>
        <button
          className="hx-btn"
          data-variant="primary"
          data-size="sm"
          type="button"
          disabled={saving}
          onClick={() => void done()}
        >
          {t('next.deck.sheets.done')}
        </button>
      </div>
      {listening ? (
        <div className="hx-deck-nsheet__text" data-live="1">
          {dict.said === '' ? t('next.deck.subs.listening') : dict.said}
        </div>
      ) : (
        <textarea
          className="hx-deck-nsheet__text"
          value={text}
          disabled={tidying}
          aria-label={t('next.deck.sheets.notesText')}
          onChange={(event) => setText(event.target.value)}
        />
      )}
      <div className="hx-deck-nsheet__row">
        {dict.available && (
          <button
            className="hx-deck-nsheet__mic"
            type="button"
            data-on={listening ? '1' : '0'}
            disabled={tidying}
            onClick={() => dict.toggle(target, t('next.deck.subs.doneToast'))}
          >
            <Icon name={listening ? 'square' : 'mic'} size="sm" />
            {listening ? t('next.deck.sheets.stop') : t('next.deck.sheets.dictate')}
          </button>
        )}
        <button
          className="hx-deck-nsheet__ai"
          type="button"
          onClick={() => {
            toast(t('next.deck.toolbar.shorterToast'));
            ask?.(t('next.deck.sheets.shorterNotesAsk', { n: slideIndex + 1 }));
          }}
        >
          <Icon name="sparkles" size="sm" />
          {t('next.deck.sheets.shorter')}
        </button>
      </div>
    </section>
  );
};

/** The phone's notes sheet. Mount inside the shell root; it leaves with its own animation. */
export const NotesSheet = ({ open, slide, ...rest }: INotesSheetProps) => {
  const shown = usePresence(open && slide ? slide : null, DECK_SHEET_EXIT_MS);
  if (!shown.value) return null;
  return (
    <Body key={shown.value.id} {...rest} slide={shown.value} closing={shown.state === PRESENCE.closing} />
  );
};
