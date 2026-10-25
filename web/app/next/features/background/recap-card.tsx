// «Что сейчас происходит»: what Claude Code wrote, when the person came back, about where the session stands. It is the
// first thing of the right column when there is one; its words are the largest thing in it, under them how long ago
// it was written. A long recap folds to a few lines with a button to open it. It comes and goes with the recap.
import { useState } from 'react';
import type { Recap } from '@/next/protocol/v2';
import { age } from '@/next/i18n/format';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';

/** A recap longer than this many characters folds, with a button to open it. */
const FOLD_AT = 150;
const EXIT_MS = 300;

interface IRecapTextProps {
  recap: Recap;
  now: number;
}

const RecapText = ({ recap, now }: IRecapTextProps) => {
  const [open, setOpen] = useState(false);
  const long = recap.text.length > FOLD_AT;
  return (
    <>
      <h3 className="hx-side__ttl">
        <Icon name="sparkles" size="sm" />
        {t('next.recap.title')}
      </h3>
      <p className="hx-recap__text" data-open={open || !long}>
        {recap.text}
      </p>
      <div className="hx-recap__line">
        <span className="hx-recap__age">{age(Math.max(0, now - recap.at))}</span>
        {long && (
          <button type="button" className="hx-recap__more" onClick={() => setOpen((v) => !v)}>
            {open ? t('next.recap.less') : t('next.recap.more')}
          </button>
        )}
      </div>
    </>
  );
};

export interface IRecapCardProps {
  recap: Recap | null;
  now: number;
}

const RecapCard = ({ recap, now }: IRecapCardProps) => {
  const { value, state } = usePresence(recap, EXIT_MS);
  if (!value) return null;
  return (
    <section className="hx-recap" data-state={state} aria-label={t('next.recap.title')}>
      <div className="hx-recap__clip">
        <div className="hx-recap__body">
          {/* A newer recap is a new card: it comes folded. */}
          <RecapText key={value.at} recap={value} now={now} />
        </div>
      </div>
    </section>
  );
};

export default RecapCard;
