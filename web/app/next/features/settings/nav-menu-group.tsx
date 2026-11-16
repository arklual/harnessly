// «Меню слева» in Settings → «Оформление»: the sidebar menu as the person lays it out — the order of the items (by hand,
// a finger holds a row on a phone), an app carried between the main buttons and «Приложения», shown or hidden, raised to the top level beside Sinopia Design or put back
// under «Приложения». Pure: the binding says what there is and saves what this asks (the person's settings, every device).
import { useRef } from 'react';
import { Group } from '@/next/features/settings/rows';
import {
  type INavLayout,
  appKey,
  appOfKey,
  navSettingsOf,
  withHidden,
  withPlaced,
  withRaised,
} from '@/next/features/sidebar/nav-layout';
import type { NavSettings } from '@/next/stores/me';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import useDrag, { EDGE, type IDragSource, type IDropTarget } from '@/next/ui/use-drag';
import useFlip from '@/next/ui/use-flip';

export interface INavEntry {
  label: string;
  icon: string;
}

interface INavMenuProps {
  layout: INavLayout;
  /** What each key is called and wears: the menu's items and the apps (`app:<id>`). */
  entries: Readonly<Record<string, INavEntry>>;
  /** A change is being saved: the rows wait for it. */
  busy: boolean;
  /** Saves the layout; rejects when it was not taken (the rows stay as they were). */
  onSave: (nav: NavSettings) => Promise<unknown>;
  onReset: () => void;
}

const TOP = 'nav-top';
const APPS = 'nav-apps';
type TKind = typeof TOP | typeof APPS;
const KINDS: readonly TKind[] = [TOP, APPS];

/** A drop within a list or across them: an app goes up to the top level and back by hand too. */
const planOf = (
  layout: INavLayout,
  source: IDragSource<TKind>,
  target: IDropTarget<TKind>,
): INavLayout | null => {
  if (!target.id || !target.edge) return null;
  return withPlaced(layout, source.id, target.id, target.edge === EDGE.after);
};

const NavRow = ({
  kind,
  id,
  entry,
  hidden,
  busy,
  raised,
  onHidden,
  onRaise,
}: {
  kind: TKind;
  id: string;
  entry: INavEntry;
  hidden: boolean;
  busy: boolean;
  /** An app: true at the top level, false under «Приложения»; undefined for the menu's own items. */
  raised?: boolean;
  onHidden: () => void;
  onRaise?: () => void;
}) => (
  <div
    className="hx-navset__row"
    role="listitem"
    data-flip={id}
    data-drag={busy ? undefined : kind}
    data-drag-id={id}
    data-drop={kind}
    data-drop-id={id}
    data-hidden={hidden || undefined}
  >
    <span className="hx-navset__grip" aria-hidden="true">
      <Icon name="grip-vertical" size="sm" />
    </span>
    <Icon name={entry.icon} className="hx-navset__icon" />
    <span className="hx-navset__label">{entry.label}</span>
    {raised !== undefined && onRaise && (
      <button
        type="button"
        className="hx-btn"
        data-variant="ghost"
        data-size="sm"
        data-shape="icon"
        disabled={busy}
        aria-label={raised ? t('next.settings.nav.lower') : t('next.settings.nav.raise')}
        data-tip={raised ? t('next.settings.nav.lower') : t('next.settings.nav.raise')}
        onClick={onRaise}
      >
        <Icon name={raised ? 'pin-off' : 'pin'} size="sm" />
      </button>
    )}
    <button
      type="button"
      className="hx-btn"
      data-variant="ghost"
      data-size="sm"
      data-shape="icon"
      disabled={busy}
      aria-pressed={!hidden}
      aria-label={hidden ? t('next.settings.nav.show') : t('next.settings.nav.hide')}
      data-tip={hidden ? t('next.settings.nav.show') : t('next.settings.nav.hide')}
      onClick={onHidden}
    >
      <Icon name={hidden ? 'eye-off' : 'eye'} size="sm" />
    </button>
  </div>
);

const NavMenuGroup = ({ layout, entries, busy, onSave, onReset }: INavMenuProps) => {
  const rootRef = useRef<HTMLDivElement>(null);
  const save = (next: INavLayout) => onSave(navSettingsOf(next));
  useDrag<TKind, TKind, INavLayout>({
    listRef: rootRef,
    dragKinds: KINDS,
    dropKinds: KINDS,
    lined: (source, target) => source.kind === target.kind,
    resolve: (source, target) => (busy ? null : planOf(layout, source, target)),
    onDrop: (next) => save(next).catch(() => undefined),
  });
  useFlip(rootRef);
  const row = (kind: TKind, key: string) => {
    const entry = entries[key];
    if (!entry) return null;
    const app = appOfKey(key);
    return (
      <NavRow
        key={key}
        kind={kind}
        id={key}
        entry={entry}
        hidden={layout.hidden.has(key)}
        busy={busy}
        raised={app === null ? undefined : kind === TOP}
        onHidden={() => void save(withHidden(layout, key, !layout.hidden.has(key))).catch(() => undefined)}
        onRaise={
          app === null
            ? undefined
            : () => void save(withRaised(layout, app, kind !== TOP)).catch(() => undefined)
        }
      />
    );
  };
  return (
    <Group
      title={t('next.settings.nav.title')}
      foot={
        <>
          {t('next.settings.nav.foot')}{' '}
          <button type="button" className="hx-navset__reset" disabled={busy} onClick={onReset}>
            {t('next.settings.nav.reset')}
          </button>
        </>
      }
    >
      <div ref={rootRef} className="hx-navset">
        <div className="hx-navset__list" role="list" aria-label={t('next.settings.nav.top')}>
          {layout.top.map((key) => row(TOP, key))}
        </div>
        {layout.apps.length > 0 && (
          <>
            <div className="hx-navset__sub">{t('next.settings.nav.apps')}</div>
            <div className="hx-navset__list" role="list" aria-label={t('next.settings.nav.apps')}>
              {layout.apps.map((id) => row(APPS, appKey(id)))}
            </div>
          </>
        )}
      </div>
    </Group>
  );
};

export default NavMenuGroup;
