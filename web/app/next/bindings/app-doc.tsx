// A document of a type an installed app adds, /designly/doc/<id>: asks the runtime what to open it with (kinds/open, which
// also brings an older document up to the app's format) and shows the app's editor in it, through the same frame as any
// view, with the document as its target. A document that may only be read shows the editor in read-only mode, with a line saying why;
// one whose app is missing says that instead.
import { useEffect, useState, type ReactNode } from 'react';
import ModeButton from '@/next/features/designly/mode-button';
import { MODE_KIND, modeStateKey, useDesignMode } from '@/next/features/designly/design-mode';
import type { ISiteMode } from '@/next/features/designly/site-view';
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
  /** The chat the document was opened from: its mode is that chat's (design-mode); null: the address's. */
  chat?: string | null;
  /** The editing mode (`?edit=1`); without it the document is looked at, as read-only. */
  edit?: boolean;
  /** Turns the editing mode on or off (the address keeps it). */
  onMode?: (edit: boolean) => void;
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
  /** Looked at, not edited: the editor is shown as for a person who may only read. */
  looking = false,
): { title: string | null; stage: ReactNode; mayEdit: boolean } => {
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
    mayEdit: false,
  });

  if (error) return say(error);
  if (opening === null || list === null)
    return { title, stage: <div className="hx-appview__wait" aria-busy="true" />, mayEdit: false };
  if (opening.k === 'failed') return say(`${t('next.appdoc.failed')}: ${opening.message}`);

  const { open } = opening;
  if (open.needsApp) {
    return {
      title,
      mayEdit: false,
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
    mayEdit: !readOnly,
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
          readOnly={readOnly || looking}
        />
      </>
    ),
  };
};

const AppDocPage = ({ machine, id, chat = null, edit = false, onMode, onOpenDrawer }: IAppDocPageProps) => {
  const [asked, setMode] = useDesignMode({ conv: chat, id, edit, onMode });
  const { title, stage, mayEdit } = useAppDocStage(machine, id, null, onMode !== undefined && !asked);
  // A document is looked at unless the editing is asked for; one that may not be changed has no way into it (its line
  // says why).
  const mode: ISiteMode | null = (() => {
    if (!onMode) return null;
    const state = t(modeStateKey(MODE_KIND.doc, asked));
    if (asked)
      return {
        state,
        editing: true,
        action: { label: t('next.site.modeView'), icon: 'eye', onClick: () => setMode(false) },
      };
    return {
      state,
      editing: false,
      action: mayEdit
        ? { label: t('next.site.modeEdit'), icon: 'pencil', onClick: () => setMode(true) }
        : null,
    };
  })();
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
      action={mode && <ModeButton mode={mode} />}
      stage={stage}
      chat={null}
      island={null}
      inspector={null}
    />
  );
};

export default AppDocPage;
