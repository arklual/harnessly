// «Записи»: the person's recordings, newest first — when, how long, what was recognized. A take plays here
// (one at a time) and downloads; in «+» a tap puts its text into the field, in Settings a take can be deleted. A take
// whose audio is gone (kept 30 days) keeps its text; one the machine could not recognize says so.
import '@/next/styles/composer.css';

import { useEffect, useRef, useState } from 'react';
import Icon from '@/next/ui/icon';
import { formatAgo } from '@/next/i18n/format';
import { t } from '@/next/i18n';

/** «Точно удалить?» waits this long for the second press. */
const SURE_MS = 4000;

export interface ITakeRow {
  id: string;
  at: number;
  ms: number;
  mime: string;
  /** What was recognized; null until then. */
  text: string | null;
  failed: boolean;
  /** Where its audio plays from; null when it is no longer kept. */
  audio: string | null;
}

interface ITakesListProps {
  rows: readonly ITakeRow[];
  now: number;
  loading?: boolean;
  /** In «+»: puts the take's text into the field. */
  onInsert?: (text: string) => void;
  /** In Settings: forgets the take, here and in the cluster. */
  onDelete?: (id: string) => void;
}

/** 0:42, 12:05: the length of a recording. */
export const takeLength = (ms: number) => {
  const seconds = Math.max(1, Math.round(ms / 1000));
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;
};

const extensionOf = (mime: string) => {
  if (mime.includes('mp4')) return 'm4a';
  if (mime.includes('aac')) return 'aac';
  if (mime.includes('ogg')) return 'ogg';
  return 'webm';
};

const fileName = (row: ITakeRow) => {
  const at = new Date(row.at);
  const pad = (n: number) => String(n).padStart(2, '0');
  const day = `${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())}`;
  return `${t('next.takes.file')}-${day}-${pad(at.getHours())}${pad(at.getMinutes())}.${extensionOf(row.mime)}`;
};

const TakesList = ({ rows, now, loading = false, onInsert, onDelete }: ITakesListProps) => {
  const player = useRef<HTMLAudioElement | null>(null);
  const [playing, setPlaying] = useState<string | null>(null);
  const [sure, setSure] = useState<string | null>(null);
  useEffect(() => () => player.current?.pause(), []);
  useEffect(() => {
    if (sure === null) return;
    const timer = setTimeout(() => setSure(null), SURE_MS);
    return () => clearTimeout(timer);
  }, [sure]);

  const play = (row: ITakeRow) => {
    const audio = player.current;
    if (!audio || !row.audio) return;
    if (playing === row.id) {
      audio.pause();
      setPlaying(null);
      return;
    }
    audio.src = row.audio;
    setPlaying(row.id);
    void audio.play().catch(() => setPlaying(null));
  };

  return (
    <div className="hx-takes" role="list" aria-label={t('next.takes.title')}>
      <audio ref={player} className="hx-takes__player" onEnded={() => setPlaying(null)} />
      {loading && <div className="hx-pick__state">{t('next.takes.loading')}</div>}
      {!loading && rows.length === 0 && <div className="hx-pick__state">{t('next.takes.empty')}</div>}
      {rows.map((row) => {
        const hint = [formatAgo(row.at, now), takeLength(row.ms)];
        if (row.failed) hint.push(t('next.takes.failed'));
        else if (!row.audio) hint.push(t('next.takes.textOnly'));
        const text = row.text?.trim() ?? '';
        const main = (
          <span className="hx-pick__text">
            <span className="hx-pick__name hx-takes__text">{text || t('next.takes.noText')}</span>
            <span className="hx-pick__hint">{hint.join(' · ')}</span>
          </span>
        );
        return (
          <div key={row.id} className="hx-takes__row" role="listitem">
            {row.audio ? (
              <button
                type="button"
                className="hx-takes__act"
                data-on={playing === row.id || undefined}
                aria-label={t(playing === row.id ? 'next.takes.pause' : 'next.takes.play')}
                onClick={() => play(row)}
              >
                <Icon name={playing === row.id ? 'pause' : 'play'} size="sm" />
              </button>
            ) : (
              <span className="hx-takes__act" aria-hidden="true">
                <Icon name="mic" size="sm" />
              </span>
            )}
            {onInsert && text ? (
              <button
                type="button"
                className="hx-pick__row"
                title={t('next.takes.insert')}
                onClick={() => onInsert(text)}
              >
                {main}
              </button>
            ) : (
              <div className="hx-pick__row">{main}</div>
            )}
            {row.audio && (
              <a
                className="hx-takes__act"
                href={row.audio}
                download={fileName(row)}
                aria-label={t('next.takes.download')}
              >
                <Icon name="download" size="sm" />
              </a>
            )}
            {onDelete && (
              <button
                type="button"
                className="hx-takes__act"
                data-sure={sure === row.id || undefined}
                aria-label={t(sure === row.id ? 'next.takes.sure' : 'next.takes.delete')}
                title={sure === row.id ? t('next.takes.sure') : undefined}
                onClick={() => {
                  if (sure !== row.id) {
                    setSure(row.id);
                    return;
                  }
                  setSure(null);
                  onDelete(row.id);
                }}
              >
                <Icon name={sure === row.id ? 'check' : 'trash-2'} size="sm" />
              </button>
            )}
          </div>
        );
      })}
    </div>
  );
};

export default TakesList;
