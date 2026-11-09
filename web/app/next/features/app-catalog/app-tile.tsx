// The card of an app on /apps, the same for the installed apps and the two catalogs: the glyph, the name (the largest
// thing), one line of what the app does, where it works, and the state in words only when the person has to act.
import type { ReactNode } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import '@/next/styles/app-tile.css';

/** The places an app works in, from the targets of its views, in the order the person knows them. */
export const whereOf = (targets: readonly string[]): string => {
  const places: string[] = [];
  if (targets.includes('chat')) places.push(t('next.apptile.chat'));
  if (targets.includes('pane')) places.push(t('next.apptile.pane'));
  if (targets.includes('app')) places.push(t('next.apptile.app'));
  if (targets.some((x) => x.startsWith('sinopia.'))) places.push(t('next.apptile.sinopia'));
  return places.join(' · ');
};

interface IAppTileProps {
  icon?: string;
  name: string;
  /** What the app does, one line. */
  line?: string;
  /** Where it works. */
  where?: string;
  /** What the person has to do before it works; null when nothing. */
  note?: string | null;
  muted?: boolean;
  /** The whole card opens the app; absent for a catalog card, whose action is `action`. */
  onOpen?: () => void;
  /** A quiet button at the card's foot («Поставить»). */
  action?: ReactNode;
  /** A small control in the card's corner (the details of an installed app). */
  corner?: ReactNode;
}

const AppTile = ({
  icon,
  name,
  line,
  where,
  note = null,
  muted = false,
  onOpen,
  action,
  corner,
}: IAppTileProps) => {
  const body = (
    <>
      <span className="hx-appk__tile">
        <Icon name={icon || 'layout-grid'} />
      </span>
      <span className="hx-appk__name">{name}</span>
      {line && <span className="hx-appk__line">{line}</span>}
      {(where || note) && (
        <span className="hx-appk__foot">
          {where && <span>{where}</span>}
          {note && <span className="hx-appk__note">{note}</span>}
        </span>
      )}
    </>
  );
  return (
    <div className="hx-appk" data-muted={muted || undefined} data-open={onOpen ? true : undefined}>
      {onOpen ? (
        <button type="button" className="hx-appk__body" onClick={onOpen}>
          {body}
        </button>
      ) : (
        <div className="hx-appk__body">{body}</div>
      )}
      {action && <div className="hx-appk__action">{action}</div>}
      {corner}
    </div>
  );
};

export default AppTile;
