// What an app reaches, in the person's words (HRN-849): grouped from the most dangerous down, a high or critical line
// says what it can do to the person, the manifest's own name of it shows small on hover. On an update only the new
// lines are given, each marked «новое».
import { useState } from 'react';
import Tooltip from '@/next/ui/tooltip';
import { Button } from '@/next/ui/button';
import { ACCESS_CLASS, type IAccess, type IText, type TAccessClass } from '@/next/features/app-views/api';
import { locale, t } from '@/next/i18n';
import '@/next/styles/app-consent.css';

/** The classes from the most dangerous: the order of the groups. */
export const CLASS_ORDER = [
  ACCESS_CLASS.critical,
  ACCESS_CLASS.high,
  ACCESS_CLASS.medium,
  ACCESS_CLASS.low,
] as const satisfies readonly TAccessClass[];

/** A line in the page's language. */
export const say = (text: IText | undefined): string =>
  text ? text[locale() === 'ru' ? 'ru' : 'en'] || text.en : '';

/** Loud classes are shown with their threat and journal what they do. */
export const isLoud = (c: TAccessClass): boolean => c === ACCESS_CLASS.critical || c === ACCESS_CLASS.high;

/** The groups of a list, from the most dangerous; a class with nothing is left out. */
export const groupsOf = (access: readonly IAccess[]): { cls: TAccessClass; items: IAccess[] }[] =>
  CLASS_ORDER.map((cls) => ({ cls, items: access.filter((a) => a.class === cls) })).filter(
    (g) => g.items.length > 0,
  );

/** The second sentence of the warning: what the loud lines can do, each threat once. */
export const threatsOf = (access: readonly IAccess[]): string[] => [
  ...new Set(access.filter((a) => isLoud(a.class) && a.threat).map((a) => say(a.threat))),
];

export interface IRevoke {
  /** The keys the app is given now: only these lines get «Снять». */
  granted: readonly string[];
  /** Takes one access back; rejects with the text to show. Pessimistic: the line stays until it is done. */
  onRevoke: (key: string) => Promise<void>;
}

const RevokeButton = ({ revoke, access }: { revoke: IRevoke; access: IAccess }) => {
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const press = async () => {
    setBusy(true);
    setFailure(null);
    try {
      await revoke.onRevoke(access.key);
    } catch (e) {
      setFailure(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <span className="hx-access__revoke">
      {failure && (
        <span className="hx-access__fail" role="alert">
          {failure}
        </span>
      )}
      <Button size="sm" variant="ghost" busy={busy} onClick={() => void press()}>
        {t('next.consent.revoke')}
      </Button>
    </span>
  );
};

const AccessList = ({
  access,
  fresh = false,
  revoke,
}: {
  access: readonly IAccess[];
  fresh?: boolean;
  /** HRN-965: every line the app is given can be taken back alone. */
  revoke?: IRevoke;
}) => {
  if (access.length === 0) return <p className="hx-access__none">{t('next.consent.nothing')}</p>;
  return (
    <div className="hx-access">
      {groupsOf(access).map((g) => (
        <section key={g.cls} className="hx-access__group" data-class={g.cls}>
          <h4 className="hx-access__h">{t(`next.consent.class.${g.cls}`)}</h4>
          <ul className="hx-access__list">
            {g.items.map((a) => (
              <Tooltip key={a.key} label={a.code}>
                <li className="hx-access__item" tabIndex={0}>
                  <div className="hx-access__row">
                    <span className="hx-access__label">
                      {say(a.label)}
                      {fresh && <span className="hx-access__new">{t('next.consent.new')}</span>}
                    </span>
                    {revoke?.granted.includes(a.key) && <RevokeButton revoke={revoke} access={a} />}
                  </div>
                  {isLoud(a.class) && a.threat && <span className="hx-access__threat">{say(a.threat)}</span>}
                </li>
              </Tooltip>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
};

export default AccessList;
