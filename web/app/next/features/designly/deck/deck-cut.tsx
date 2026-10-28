// The scene «Монтаж»: on the left the recording's picture with its subtitle, the track of slides (a segment is as wide
// as it lasts) and the four chips of the montage; on the right the transcript, where what the automatic cut took (pauses,
// «э-э», «ну вот») is struck through and a click brings it back. A change is saved before it shows (pessimistic).
import { useState } from 'react';
import { formatMs } from '@/next/features/designly/deck/clock';
import {
  cutRanges,
  isDropped,
  partKey,
  segmentAt,
  skippedMs,
  toggleKept,
  trackWeight,
} from '@/next/features/designly/deck/cut-model';
import DeckPlayer, { type IDeckSeek } from '@/next/features/designly/deck/deck-player';
import { useDeckRec } from '@/next/features/designly/deck/use-deck-rec';
import type { IDeckPart, IDeckRecording, IDeckSlide } from '@/next/features/designly/deck/types';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';

export interface IDeckCutProps {
  machine: MachineId;
  artifact: string;
  recId: string;
  initial: IDeckRecording | null;
  slides: readonly IDeckSlide[];
  frameSrc: (path: string) => string | null;
}

const partText = (part: IDeckPart) =>
  part.kind === 'pause'
    ? t('next.deck.cut.pause', {
        s: ((part.t1 - part.t0) / 1000).toLocaleString(undefined, { maximumFractionDigits: 1 }),
      })
    : part.text;

const DeckCut = ({ machine, artifact, recId, initial, slides, frameSrc }: IDeckCutProps) => {
  const { rec, building, failed, busy, change } = useDeckRec(machine, artifact, recId, initial);
  const [seek, setSeek] = useState<IDeckSeek | null>(null);
  const [ms, setMs] = useState(0);
  const [picked, setPicked] = useState<number | null>(null);

  if (!rec) {
    return (
      <section className="hx-deckm-cut" aria-busy={!failed}>
        {failed ? <p className="hx-deckm-cut__note">{t('next.deck.cut.failed')}</p> : null}
      </section>
    );
  }
  const edit = rec.edit;
  const skipped = skippedMs(cutRanges(rec.segments, { ...edit, cut: true, kept: [] }));
  const playing = segmentAt(rec.segments, ms);
  const current = picked ?? playing;
  const go = (segment: number) => {
    const s = rec.segments[segment];
    if (!s) return;
    setPicked(segment);
    setSeek({ ms: s.t0, at: {} });
  };

  return (
    <section className="hx-deckm-cut" aria-busy={busy}>
      <div className="hx-deckm-cut__l">
        <DeckPlayer
          machine={machine}
          artifact={artifact}
          rec={rec}
          edit={edit}
          slides={slides}
          frameSrc={frameSrc}
          subs={edit.subs}
          seek={seek}
          onTime={(time) => {
            setMs(time);
            setPicked(null);
          }}
        />
        <div className="hx-deckm-track" aria-label={t('next.deck.cut.track')}>
          {rec.segments.map((segment, i) => (
            <button
              key={segment.t0}
              type="button"
              data-on={i === current ? '1' : '0'}
              ref={(el) => el?.style.setProperty('--w', String(trackWeight(segment)))}
              aria-label={t('next.deck.stage.slide', {
                n: slides.findIndex((s) => s.id === segment.page) + 1,
              })}
              onClick={() => go(i)}
            />
          ))}
        </div>
        <div className="hx-deckm-cut__opts">
          <button
            className="hx-deckm-chip"
            type="button"
            data-on={edit.cut ? '1' : '0'}
            disabled={busy}
            onClick={() => void change({ ...edit, cut: !edit.cut, kept: [] })}
          >
            {t('next.deck.cut.noPauses', { t: formatMs(skipped) })}
          </button>
          <button
            className="hx-deckm-chip"
            type="button"
            data-on={edit.subs ? '1' : '0'}
            disabled={busy}
            onClick={() => void change({ ...edit, subs: !edit.subs })}
          >
            {t('next.deck.cut.subs')}
          </button>
          <button
            className="hx-deckm-chip"
            type="button"
            data-on={edit.cursor === 'smooth' ? '1' : '0'}
            disabled={busy}
            onClick={() => void change({ ...edit, cursor: edit.cursor === 'smooth' ? 'off' : 'smooth' })}
          >
            {t('next.deck.cut.cursor')}
          </button>
          {rec.summary.camera ? (
            <button
              className="hx-deckm-chip"
              type="button"
              data-on={edit.cam === 'off' ? '0' : '1'}
              disabled={busy}
              onClick={() => void change({ ...edit, cam: edit.cam === 'off' ? 'br' : 'off' })}
            >
              {t('next.deck.cut.cam')}
            </button>
          ) : null}
        </div>
      </div>
      <div className="hx-deckm-cut__r">
        <h4>{t('next.deck.cut.text')}</h4>
        {building ? <p className="hx-deckm-cut__note">{t('next.deck.cut.building')}</p> : null}
        {rec.segments.map((segment, si) => (
          <div className="hx-deckm-segt" key={segment.t0} data-on={si === current ? '1' : '0'}>
            <button type="button" className="hx-deckm-segt__t" onClick={() => go(si)}>
              {formatMs(segment.t0)}
            </button>
            <p onClick={() => setPicked(si)}>
              {segment.parts.map((part, pi) => {
                const key = partKey(si, pi);
                if (!part.cut) return <span key={key}>{partText(part)} </span>;
                const dropped = isDropped(edit, part, key);
                return (
                  <span key={key}>
                    <button
                      type="button"
                      className="hx-deckm-segt__cut"
                      data-kept={dropped ? '0' : '1'}
                      disabled={busy || !edit.cut}
                      onClick={() => void change({ ...edit, kept: toggleKept(edit, key) })}
                    >
                      {partText(part)}
                    </button>{' '}
                  </span>
                );
              })}
            </p>
          </div>
        ))}
      </div>
    </section>
  );
};

export default DeckCut;
