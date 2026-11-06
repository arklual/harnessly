// A widget in the chat's feed (widgets.ts): an app's chat view drawn with the data the agent gave, or a widget the agent
// wrote itself, in the same sandboxed frame and through the same bridge as a card. The same id shown again changes it
// where it stands: an app's view is told the new data (`data`, and hl.context.data), the agent's own page is loaded anew.
import { useMemo } from 'react';
import AppFrame from '@/next/features/app-views/app-frame';
import { useInstalledApps } from '@/next/features/app-views/api';
import { useAppViewHost } from '@/next/features/app-views/host';
import { openAppTab } from '@/next/features/app-views/open';
import { useChatWidget, useSettled, widgetApp, type IChatWidget } from '@/next/features/app-views/widgets';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

const Skeleton = () => <div className="hx-appcard hx-appcard--loading" aria-busy="true" />;

const OwnWidget = ({ widget }: { widget: IChatWidget }) => {
  const host = useAppViewHost();
  const { list } = useInstalledApps(host?.machine ?? 'local');
  const rev = useSettled(widget.rev);
  const name = t('next.appview.widget');
  // A new object only with a new revision: the frame's bridge is made again for a new app.
  const { id, title } = widget;
  const app = useMemo(() => widgetApp(id, title, rev, name), [id, title, rev, name]);
  if (host === null || list === null) return <Skeleton />;
  return (
    <div className="hx-appcard">
      <AppFrame
        machine={host.machine}
        app={app}
        view={app.views[0]}
        target="chat"
        viewPort={list.viewPort}
        conversation={host.conversation}
        data={widget.data}
        reloadToken={rev}
        inChat
        fit
      />
    </div>
  );
};

const AppWidget = ({ widget }: { widget: IChatWidget }) => {
  const host = useAppViewHost();
  const { list } = useInstalledApps(host?.machine ?? 'local');
  const notice = useMemo(
    () => ({ seq: widget.rev, method: 'harnessly/notifications/data', params: widget.data }),
    [widget.rev, widget.data],
  );
  if (host === null || list === null) return <Skeleton />;
  const app = list.apps.find((a) => a.id === widget.app);
  const view = app?.views.find((v) => v.id === widget.view && v.targets.includes('chat'));
  // The app or its card is gone since: the step stays as it was.
  if (!app || !view) return null;
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
        data={widget.data}
        notice={notice}
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

const ChatWidget = ({ id }: { id: string }) => {
  const host = useAppViewHost();
  const { widget } = useChatWidget(host?.machine ?? null, host?.conversation ?? null, id);
  if (host === null) return null;
  if (widget === undefined) return <Skeleton />;
  if (widget === null) return null;
  return widget.kind === 'html' ? <OwnWidget widget={widget} /> : <AppWidget widget={widget} />;
};

export default ChatWidget;
