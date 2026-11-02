// An app's card in the chat feed (MCP Apps): the app's `card` view for a tool result, in the same sandboxed frame and
// through the same bridge as a pane tab. «Открыть в панели» opens the app's pane view.
import AppFrame from '@/next/features/app-views/app-frame';
import { useInstalledApps } from '@/next/features/app-views/api';
import { isOwnTool, type IAppCard } from '@/next/features/app-views/card';
import { useAppViewHost } from '@/next/features/app-views/host';
import { openAppTab } from '@/next/features/app-views/open';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

const AppCard = ({ card }: { card: IAppCard }) => {
  const host = useAppViewHost();
  const { list } = useInstalledApps(host?.machine ?? 'local');
  if (host === null || list === null) return null;
  const app = list.apps.find((a) => a.id === card.app);
  // Another server's result must not make the host draw an installed app's card with data of its choosing.
  if (!app || !isOwnTool(card, app)) return null;
  const view = app.views.find((v) => v.id === card.view && v.targets.includes('chat'));
  // An app that is gone, or whose view is not a card, leaves the plain tool step as it was.
  if (!view) return null;
  const pane = app.views.find((v) => v.targets.includes('pane'));
  return (
    <div className="hx-appcard">
      <AppFrame
        machine={host.machine}
        app={app}
        view={view}
        target="chat"
        viewPort={list.viewPort}
        conversation={host.conversation}
        data={card.data}
        fit
      />
      {pane && (
        <button type="button" className="hx-appcard__open" onClick={() => openAppTab(`${app.id}/${pane.id}`)}>
          <Icon name="panel-right-open" size="sm" />
          <span>{t('next.appview.openInPane')}</span>
        </button>
      )}
    </div>
  );
};

export default AppCard;
