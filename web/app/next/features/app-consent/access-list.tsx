// What an app reaches, in the person's words: grouped from the most dangerous down, a high or critical line
// says what it can do to the person, the manifest's own name of it shows small on hover. On an update only the new
// lines are given, each marked «новое».
import Tooltip from '@/next/ui/tooltip';
import { ACCESS_CLASS, type IAccess, type IText, type TAccessClass } from '@/next/features/app-views/api';
import { locale, t } from '@/next/i18n';

/** The classes from the most dangerous: the order of the groups. */
export const CLASS_ORDER = [
  ACCESS_CLASS.critical,
  ACCESS_CLASS.high,
  ACCESS_CLASS.medium,
  ACCESS_CLASS.low,
] as const satisfies readonly TAccessClass[];

/** A line in the page's language. */
export const say = (text: IText | undefined): string => (text ? text[locale()] || text.en : '');

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

const AccessList = ({ access, fresh = false }: { access: readonly IAccess[]; fresh?: boolean }) => {
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
                  <span className="hx-access__label">
                    {say(a.label)}
                    {fresh && <span className="hx-access__new">{t('next.consent.new')}</span>}
                  </span>
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
