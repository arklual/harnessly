// The scene «Готово»: a dialog over the blurred stage, «Видео готово» with the recording's picture, the two switches of
// the montage (pauses and «э-э», subtitles) and the two ways on: «Монтаж» and «Скачать MP4». The dialog leaves with an
// animation (180 ms), whichever way it goes.
import { useEffect, useRef, useState } from 'react';
import { formatMs } from '@/next/features/designly/deck/clock';
import { cutRanges, skippedMs } from '@/next/features/designly/deck/cut-model';
import DeckPlayer from '@/next/features/designly/deck/deck-player';
import { useDeckRec } from '@/next/features/designly/deck/use-deck-rec';
import type { IDeckRecording, IDeckSlide } from '@/next/features/designly/deck/types';
import { plural, t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';

/** The dialog's exit animation. */
export const DONE_EXIT_MS = 180;

export interface IDeckDoneProps {
  machine: MachineId;
  /** The project's id. */
  artifact: string;
  recId: string;
  /** The recording as `finish` answered, shown at once while the runtime is read again. */
  initial: IDeckRecording | null;
  slides: readonly IDeckSlide[];
  frameSrc: (path: string) => string | null;
  /** The dialog has left, back to the editor (the backdrop, Esc). */
  onClose: () => void;
  /** «Монтаж»: the dialog has left, the montage opens. */
  onMontage: () => void;
  /** «Скачать MP4»: the dialog has left; the caller starts the export and tells the person (the toast is its). */
  onDownload: (recId: string) => void;
}

const DeckDone = ({
  machine,
  artifact,
  recId,
  initial,
  slides,
  frameSrc,
  onClose,
  onMontage,
  onDownload,
}: IDeckDoneProps) => {
  const { rec, busy, change } = useDeckRec(machine, artifact, recId, initial);
  const [closing, setClosing] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const leave = (then: () => void) => {
    if (closing) return;
    setClosing(true);
    timer.current = setTimeout(then, DONE_EXIT_MS);
  };
  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      e.preventDefault();
      leave(onClose);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
    // the key is bound once; `leave` and `onClose` are read when it is pressed
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const edit = rec?.edit;
  const skipped =
    rec && edit ? skippedMs(cutRanges(rec.segments, { ...edit, cut: true, kept: edit.kept })) : 0;
  const length = rec && edit ? rec.summary.durationMs - (edit.cut ? skipped : 0) : 0;
  const retaken = rec?.retaken ?? [];

  return (
    <>
      <div className="hx-deckm-backdrop" data-closing={closing ? '1' : '0'} onClick={() => leave(onClose)} />
      <section
        className="hx-deckm-done"
        data-closing={closing ? '1' : '0'}
        role="dialog"
        aria-label={t('next.deck.done.title')}
      >
        <h3>{t('next.deck.done.title')}</h3>
        <div className="sub">
          {rec ? plural(slides.length, 'next.deck.done.summary', { len: formatMs(length) }) : ' '}
          {retaken.length > 0 ? ` ${t('next.deck.done.retaken', { k: retaken.join(', ') })}` : ''}
        </div>
        {rec && edit ? (
          <>
            <DeckPlayer
              machine={machine}
              artifact={artifact}
              rec={rec}
              edit={edit}
              slides={slides}
              frameSrc={frameSrc}
              subs={false}
            />
            <div className="hx-deckm-opts" aria-busy={busy}>
              <div className="hx-deckm-opt">
                <span>{t('next.deck.done.cut')}</span>
                <small>−{formatMs(skipped)}</small>
                <button
                  className="hx-deckm-tog"
                  type="button"
                  data-on={edit.cut ? '1' : '0'}
                  disabled={busy}
                  aria-label={t('next.deck.done.cutLabel')}
                  aria-pressed={edit.cut}
                  onClick={() => void change({ ...edit, cut: !edit.cut, kept: [] })}
                />
              </div>
              <div className="hx-deckm-opt">
                <span>{t('next.deck.done.subs')}</span>
                <button
                  className="hx-deckm-tog"
                  type="button"
                  data-on={edit.subs ? '1' : '0'}
                  disabled={busy}
                  aria-label={t('next.deck.done.subs')}
                  aria-pressed={edit.subs}
                  onClick={() => void change({ ...edit, subs: !edit.subs })}
                />
              </div>
            </div>
          </>
        ) : null}
        <div className="hx-deckm-row">
          <button
            className="hx-btn"
            data-size="sm"
            type="button"
            onClick={() => leave(onMontage)}
            disabled={!rec}
          >
            <Icon name="pencil" size="sm" />
            {t('next.deck.done.edit')}
          </button>
          <button
            className="hx-btn"
            data-variant="primary"
            data-size="sm"
            type="button"
            onClick={() => leave(() => onDownload(recId))}
          >
            {t('next.deck.done.download')}
          </button>
        </div>
      </section>
    </>
  );
};

export default DeckDone;
