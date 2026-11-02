// A document of a type an installed app adds, /designly/doc/<id>: asks the runtime what to open it with (kinds/open, which
// also brings an older document up to the app's format) and shows the app's editor in it, through the same frame as any
// view, with the document as its target. A document that may only be read shows the editor in read-only mode, with a line saying why;
// one whose app is missing says that instead.
import { useEffect, useState, type ReactNode } from 'react';
import AppFrame from '@/next/features/app-views/app-frame';
import { useInstalledApps } from '@/next/features/app-views/api';
import AppMain from '@/next/features/app-main/app-main';
import { PLACEMENT } from '@/next/features/app-main/placement';
import { NeedsApp, ReadOnly } from '@/next/features/app-doc/app-doc-notice';
import {
  loadKindDocs,
  openKindDocument,
  type IKindDoc,
  type IKindOpen,
} from '@/next/features/app-catalog/api';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE, routePath } from '@/next/routes/routes';
import { useTabTitle } from '@/next/stores/tabs';
import { t } from '@/next/i18n';

interface IAppDocPageProps {
  machine: MachineId;
  id: string;
  onOpenDrawer: () => void;
}

/** What the runtime said, or why it could not; null while it is asked. */
type TOpening = { k: 'opened'; open: IKindOpen; doc: IKindDoc | null } | { k: 'failed'; message: string };

/**
 * The document's editor as it stands in a page or a pane: the app's editor view in a frame with the document as its
 * target (in read-only mode when the document may only be read), or why there is none. `title` is the document's, once known.
 */
export const useAppDocStage = (
  machine: MachineId,
  id: string,
  conversation: ConvId | null,
): { title: string | null; stage: ReactNode } => {
  const { list, error } = useInstalledApps(machine);
  const [opening, setOpening] = useState<TOpening | null>(null);

  useEffect(() => {
    let live = true;
    // The title comes from the list of documents; the opening is what decides, so a list that fails does not stop it.
    Promise.all([openKindDocument(machine, id), loadKindDocs(machine).catch(() => [] as IKindDoc[])]).then(
      ([open, docs]) =>
        live && setOpening({ k: 'opened', open, doc: docs.find((d) => d.document === id) ?? null }),
      (e: Error) => live && setOpening({ k: 'failed', message: e.message }),
    );
    return () => {
      live = false;
    };
  }, [machine, id]);

  const doc = opening?.k === 'opened' ? opening.doc : null;
  const title = doc?.title ?? null;
  const say = (text: string) => ({
    title,
    stage: <p className="hx-appview__error">{text}</p>,
  });

  if (error) return say(error);
  if (opening === null || list === null)
    return { title, stage: <div className="hx-appview__wait" aria-busy="true" /> };
  if (opening.k === 'failed') return say(`${t('next.appdoc.failed')}: ${opening.message}`);

  const { open } = opening;
  if (open.needsApp) {
    return {
      title,
      stage: <NeedsApp app={doc?.kindTitle ?? open.kind} onOpenApps={() => goTo({ kind: ROUTE.apps })} />,
    };
  }

  const app = list.apps.find((a) => a.id === open.app) ?? null;
  const view = app?.views.find((v) => v.id === open.editor) ?? null;
  if (!app || !view || !open.target?.startsWith('sinopia.')) return say(t('next.appdoc.noView'));
  // A document that may only be read still shows the editor, told so: it draws the document and offers no way to change it.
  const readOnly = open.readOnly === true;
  return {
    title,
    stage: (
      <>
        {readOnly && <ReadOnly code={open.code} reason={open.reason} />}
        <AppFrame
          machine={machine}
          app={app}
          view={view}
          target={open.target as `sinopia.${string}`}
          viewPort={list.viewPort}
          conversation={conversation}
          document={id}
          readOnly={readOnly}
        />
      </>
    ),
  };
};

const AppDocPage = ({ machine, id, onOpenDrawer }: IAppDocPageProps) => {
  const { title, stage } = useAppDocStage(machine, id, null);
  useTabTitle(routePath({ kind: ROUTE.appDoc, id }), title);
  return (
    <AppMain
      title={title ?? ''}
      chatTitle={null}
      placement={PLACEMENT.left}
      float={null}
      onPlacement={() => undefined}
      onFloat={() => undefined}
      onBack={null}
      onOpenDrawer={onOpenDrawer}
      menu={[]}
      stage={stage}
      chat={null}
      island={null}
      inspector={null}
    />
  );
};

export default AppDocPage;
