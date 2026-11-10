// The chat's apps that work in the background (the right `background`, HRN-852): while the chat is open, each such app
// that is on in it, agreed to and not switched off keeps its first pane view running hidden, so its hooks hear the
// agent and its events arrive without the person opening the view. The view is told it is not visible.
import AppFrame from '@/next/features/app-views/app-frame';
import { type IInstalledApp, useInstalledApps } from '@/next/features/app-views/api';
import type { ConvId, MachineId } from '@/next/protocol/v2';

interface IAppBackgroundProps {
  machine: MachineId;
  conversation: ConvId;
  /** The apps on in the chat (ConvSummary.apps). */
  apps: readonly string[] | undefined;
}

/** The apps of the chat that run hidden: on in it, holding `background`, agreed to and not turned off. */
export const backgroundApps = (installed: readonly IInstalledApp[], on: readonly string[]): IInstalledApp[] =>
  installed.filter(
    (app) =>
      on.includes(app.id) &&
      app.permissions.includes('background') &&
      !app.off &&
      !app.muted &&
      !app.consent &&
      app.views.some((v) => v.targets.includes('pane')),
  );

const AppBackground = ({ machine, conversation, apps }: IAppBackgroundProps) => {
  const on = apps ?? [];
  const { list } = useInstalledApps(machine, 0, on.length > 0);
  if (list === null) return null;
  const running = backgroundApps(list.apps, on);
  if (running.length === 0) return null;
  return (
    <div className="hx-appbg" aria-hidden="true">
      {running.map((app) => {
        const view = app.views.find((v) => v.targets.includes('pane'))!;
        return (
          <AppFrame
            key={app.id}
            machine={machine}
            app={app}
            view={view}
            target="pane"
            viewPort={list.viewPort}
            conversation={conversation}
          />
        );
      })}
    </div>
  );
};

export default AppBackground;
