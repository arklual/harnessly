// «Скриншоты»: the pictures the agent and its subagents looked at (images it read, screenshots a tool saved), newest
// first, as a strip of squares in the right column. Four show at first and «Ещё N» opens the rest by height; a press
// opens a picture full size, where the arrows, the side buttons and a swipe step through the others, and a line says
// which agent looked at it and when. It comes and goes with the pictures; until the conversation's snapshot has said
// what there is, a skeleton stands in the squares.
import { useState } from 'react';
import type { Shot } from '@/next/protocol/v2';
import { formatAgo } from '@/next/i18n/format';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import ImageView, { type IImageViewItem } from '@/next/ui/image-view';
import { usePresence } from '@/next/ui/presence';
import { Skeleton, useDelayed } from '@/next/ui/skeleton';

/** The squares shown before «Ещё N». */
export const SHOTS_HEAD = 4;
const EXIT_MS = 300;
const SKELETON_SQUARES = [0, 1, 2, 3] as const;

export interface IShotsStripProps {
  /** Newest first; null until the conversation's snapshot has come. */
  shots: readonly Shot[] | null;
  /** Where a picture's bytes are. */
  srcOf: (shot: Shot) => string;
  now: number;
}

const who = (shot: Shot): string => shot.agent ?? t('next.shots.byAgent');

const Square = ({ shot, src, now, onOpen }: { shot: Shot; src: string; now: number; onOpen: () => void }) => {
  const [state, setState] = useState<'loading' | 'ready' | 'broken'>('loading');
  return (
    <button
      type="button"
      className="hx-shots__sq"
      data-state={state}
      aria-label={`${shot.name} · ${who(shot)} · ${formatAgo(shot.at, now)}`}
      onClick={onOpen}
    >
      <img
        className="hx-shots__img"
        src={src}
        alt=""
        loading="lazy"
        decoding="async"
        draggable={false}
        onLoad={() => setState('ready')}
        onError={() => setState('broken')}
      />
      {state === 'broken' && <Icon name="image" className="hx-shots__gone" />}
    </button>
  );
};

const ShotsList = ({
  shots,
  srcOf,
  now,
}: {
  shots: readonly Shot[];
  srcOf: IShotsStripProps['srcOf'];
  now: number;
}) => {
  const [more, setMore] = useState(false);
  const [viewing, setViewing] = useState<string | null>(null);
  const head = shots.slice(0, SHOTS_HEAD);
  const rest = shots.slice(SHOTS_HEAD);
  const at = viewing === null ? -1 : shots.findIndex((x) => x.id === viewing);
  const current = at >= 0 ? shots[at] : null;
  const item: IImageViewItem | null = current
    ? {
        src: srcOf(current),
        name: current.name,
        caption: `${who(current)} · ${formatAgo(current.at, now)}`,
      }
    : null;
  const step = (delta: 1 | -1) => {
    if (at < 0) return;
    setViewing(shots[(at + delta + shots.length) % shots.length].id);
  };
  const square = (shot: Shot) => (
    <Square key={shot.id} shot={shot} src={srcOf(shot)} now={now} onOpen={() => setViewing(shot.id)} />
  );
  return (
    <>
      <div className="hx-shots__grid">{head.map(square)}</div>
      {rest.length > 0 && (
        <>
          <div className="hx-shots__rest" data-open={more}>
            <div className="hx-shots__rest-clip" inert={!more || undefined}>
              <div className="hx-shots__grid">{rest.map(square)}</div>
            </div>
          </div>
          <button
            type="button"
            className="hx-side__more hx-shots__more"
            aria-expanded={more}
            onClick={() => setMore((v) => !v)}
          >
            {more ? t('next.bg.moreLess') : t('next.bg.more', { n: rest.length })}
            <Icon name="chevron-down" size="sm" className="hx-side__more-chev" />
          </button>
        </>
      )}
      <ImageView
        item={item}
        onClose={() => setViewing(null)}
        position={at >= 0 ? { at: at + 1, of: shots.length } : undefined}
        onStep={step}
      />
    </>
  );
};

const NONE: readonly Shot[] = [];

const ShotsStrip = ({ shots, srcOf, now }: IShotsStripProps) => {
  const has = shots !== null && shots.length > 0;
  // The last pictures stay on screen while the section leaves.
  const { value, state } = usePresence(has ? shots : null, EXIT_MS);
  const loading = shots === null;
  // A skeleton only for a snapshot that is slow; a fast one shows nothing before the squares.
  const waited = useDelayed(loading);
  if (loading ? !waited : !value) return null;
  return (
    <section className="hx-shots" data-state={state} aria-label={t('next.shots.title')}>
      <div className="hx-shots__clip">
        <div className="hx-shots__body">
          <h3 className="hx-side__ttl">
            <Icon name="images" size="sm" />
            {t('next.shots.title')}
          </h3>
          {loading ? (
            <div className="hx-shots__grid" aria-busy="true">
              {SKELETON_SQUARES.map((i) => (
                <Skeleton key={i} className="hx-shots__sk" />
              ))}
            </div>
          ) : (
            <ShotsList shots={value ?? NONE} srcOf={srcOf} now={now} />
          )}
        </div>
      </div>
    </section>
  );
};

export default ShotsStrip;
