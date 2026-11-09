// The pane tab of an app: one of its views for the pane, in the sandboxed frame (which stands in the page's layer of
// kept views over the tab's place, so it runs on while the tab is out of sight: kept-frames). Under it, when the person asked for
// it, the inspector (the rights of the app; for a dev install also the view's calls and storage, and a reload). The
// agent's questions to the person inside the app (app.ask) are asked from here while the tab is open.
import { useState } from 'react';
import { KeptSlot } from '@/next/features/app-views/kept-frames';
import { useInstalledApps } from '@/next/features/app-views/api';
import AppAsks from '@/next/features/app-views/asks';
import AppInspector from '@/next/features/app-views/inspector';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';

interface IAppTabProps {
  /** The pane tab: its view is kept by it while the tab is out of sight (kept-frames). */
  tab: string;
  machine: MachineId;
  conversation: ConvId;
  /** `<app id>[/<view>]`. */
  app: string;
  /** null: the app's first view for the pane. */
  view: string | null;
  /** The inspector under the view: the pane's «Отладка» of a dev install turns it on. */
  inspect?: boolean;
}

const AppTab = ({ tab, machine, conversation, app: id, view: viewId, inspect = false }: IAppTabProps) => {
  const { list, error } = useInstalledApps(machine);
  const [reload, setReload] = useState(0);
  if (error) return <p className="hx-appview__error">{error}</p>;
  if (list === null) return <div className="hx-appview__wait" aria-busy="true" />;
  const app = list.apps.find((a) => a.id === id);
  const view = app?.views.find((v) => (viewId === null || v.id === viewId) && v.targets.includes('pane'));
  if (!app || !view) return <p className="hx-appview__error">{t('next.appview.gone')}</p>;
  return (
    <>
      <KeptSlot
        machine={machine}
        conversation={conversation}
        tab={tab}
        app={id}
        view={viewId}
        reload={reload}
      />
      {inspect && (
        <AppInspector
          machine={machine}
          app={app}
          view={view}
          target="pane"
          conversation={conversation}
          debug
          onReload={() => setReload((n) => n + 1)}
          agentWalled={list.agentWalled}
        />
      )}
      <AppAsks machine={machine} conversation={conversation} />
    </>
  );
};

export default AppTab;
