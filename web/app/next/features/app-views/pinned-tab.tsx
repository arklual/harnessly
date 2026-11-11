// The app's pinned tab in the chat's right pane: beside a document of an app's type made of a Sinopia site,
// the pane's first tab after the tools is that app's view (views[].pinned), bound to the document and the chat. It is
// never closed and is there only in that app's documents. The view is the app's: its sections, their skeleton while
// they load and «Повторить» when they fail are drawn by it; the frame's own failure is the host's (AppFrame).
import { useEffect, useState } from 'react';
import AppFrame from '@/next/features/app-views/app-frame';
import { useInstalledApps, type IAppView, type IInstalledApp } from '@/next/features/app-views/api';
import AppAsks from '@/next/features/app-views/asks';
import { openKindDocument } from '@/next/features/app-catalog/api';
import type { ConvId, MachineId } from '@/next/protocol/v2';

/** The id of the pinned tab in the pane's strip. */
export const PINNED_APP_TAB = 'app-pinned';

export interface IPinnedApp {
  app: IInstalledApp;
  view: IAppView;
  /** `sinopia.<type>`: the target the view is shown with. */
  target: `sinopia.${string}`;
  document: string;
}

/** What the runtime says the document is: its app and type, when it is made of a site; null for any other. */
type TDocApp = { document: string; app: string; target: `sinopia.${string}` } | null;

/** The pinned view for the design beside the pane; null when there is none (another document, the app off, loading). */
export const usePinnedApp = (machine: MachineId | null, design: string | null): IPinnedApp | null => {
  const { list } = useInstalledApps(machine ?? ('' as MachineId), 0, machine !== null && design !== null);
  const [docApp, setDocApp] = useState<TDocApp>(null);
  useEffect(() => {
    if (machine === null || design === null) return;
    let live = true;
    // A design of Sinopia's own is not a document of an app's type: the runtime answers 404, and there is no tab.
    openKindDocument(machine, design).then(
      (open) => {
        if (!live) return;
        const madeOfSite =
          open.extends === 'site' && open.app !== undefined && open.target?.startsWith('sinopia.');
        setDocApp(
          madeOfSite
            ? { document: design, app: open.app!, target: open.target as `sinopia.${string}` }
            : null,
        );
      },
      () => live && setDocApp(null),
    );
    return () => {
      live = false;
    };
  }, [machine, design]);
  if (design === null || docApp === null || docApp.document !== design || list === null) return null;
  const app = list.apps.find((a) => a.id === docApp.app);
  const view = app?.views.find((v) => v.pinned === true && v.targets.includes(docApp.target));
  if (!app || !view) return null;
  return { app, view, target: docApp.target, document: design };
};

interface IPinnedAppTabProps {
  machine: MachineId;
  conversation: ConvId;
  pinned: IPinnedApp;
}

const PinnedAppTab = ({ machine, conversation, pinned }: IPinnedAppTabProps) => {
  const { list } = useInstalledApps(machine);
  if (list === null) return <div className="hx-appview__wait" aria-busy="true" />;
  return (
    <>
      <AppFrame
        machine={machine}
        app={pinned.app}
        view={pinned.view}
        target={pinned.target}
        viewPort={list.viewPort}
        conversation={conversation}
        document={pinned.document}
      />
      <AppAsks machine={machine} conversation={conversation} />
    </>
  );
};

export default PinnedAppTab;
