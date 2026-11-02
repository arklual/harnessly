// The editor's side of the apps in a document (spec: Sinopia): which apps are on in it, the boards that answer clicks on
// bound elements, the selection the apps are told about, the marks drawn on the boards, and the gate before sharing.
// The runtime holds the rules (which app is on in which document, what blocks a share); this holds what the page shows.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { sendMessage } from '@/next/commands/messages';
import {
  callTool,
  createViewSession,
  runSinopiaChecks,
  syncSinopia,
  useInstalledApps,
  type IAppView,
  type IInstalledApp,
} from '@/next/features/app-views/api';
import type { IViewNotice } from '@/next/features/app-views/app-frame';
import { targetAllowed } from '@/next/features/app-views/bridge';
import { commentMessage, selectionMessage } from '@/next/features/app-views/sinopia/annotation';
import {
  activeApps,
  inspectorFor,
  selectedOf,
  type IActiveApp,
  type ISelectionAction,
} from '@/next/features/app-views/sinopia/contributions';
import { gateShare } from '@/next/features/app-views/sinopia/share-gate';
import { HlHub, type THlEvent } from '@/next/features/app-views/sinopia/hub';
import type {
  ISelectedNode,
  ISinopiaCheckResult,
  ISinopiaState,
} from '@/next/features/app-views/sinopia/types';
import { readBridgeMessage } from '@/next/features/designly/page-bridge';
import { t } from '@/next/i18n';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { toast } from '@/next/ui/toast';

/** How often the runtime is asked what it keeps of the document (the marks of the save checks, the apps on in it). */
export const POLL_MS = 3000;

/** The view the editor opened for a click on a bound element. */
export interface IFocus {
  app: IInstalledApp;
  view: IAppView;
  field: string;
}

export interface ISinopiaOptions {
  machine: MachineId;
  /** The document's (project's) id. */
  id: string;
  /** The document's kind; null while the project is not read. */
  kind: string | null;
  /** The chat linked to the document: where the agent tasks go. */
  conversation: ConvId | null;
  /** The slot's origin the boards are shown at; null while there is none. */
  slotOrigin: string | null;
}

/** A block the presentation editor selected: the board's file, its number, the board's fingerprint and words. */
export interface IPickedBlock {
  path: string;
  tpl: number;
  was: string;
  text: string;
  tag: string;
  rect: readonly [number, number, number, number];
}

export interface ISinopia {
  /** The apps on in the document with what they put into it. */
  active: IActiveApp[];
  viewPort: number | null;
  state: ISinopiaState | null;
  selection: ISelectedNode[];
  focus: IFocus | null;
  notice: IViewNotice | null;
  /** The share gate's findings, while the dialog shows them. */
  blocked: ISinopiaCheckResult[] | null;
  clearSelection: () => void;
  closeFocus: () => void;
  dismissBlocked: () => void;
  runAction: (action: ISelectionAction) => Promise<void>;
  /** A block selected in the presentation editor (null: none); stable, it is called from an effect. */
  pickBlock: (picked: IPickedBlock | null) => void;
  /** Runs `proceed` (the share, the publish) if the checks that run on share let it go. */
  gate: (proceed: () => void) => void;
  /** A Document API call of a view went through. */
  onViewCall: (app: IInstalledApp, method: string, params: Record<string, unknown>) => void;
}

const NONE: IActiveApp[] = [];

export const useSinopia = ({ machine, id, kind, conversation, slotOrigin }: ISinopiaOptions): ISinopia => {
  const installed = useInstalledApps(machine, 0, kind !== null);
  const apps = installed.list?.apps ?? null;
  // The runtime is asked about the document only if some installed app works in this kind of document at all.
  const wants =
    kind !== null && apps !== null && apps.some((a) => targetAllowed(a.targets, `sinopia.${kind}`));
  const [state, setState] = useState<ISinopiaState | null>(null);
  const [selection, setSelection] = useState<ISelectedNode[]>([]);
  const [picked, setFocus] = useState<IFocus | null>(null);
  const [notice, setNotice] = useState<IViewNotice | null>(null);
  const [blocked, setBlocked] = useState<ISinopiaCheckResult[] | null>(null);

  const active = useMemo(
    () =>
      wants && state !== null && apps !== null && kind !== null ? activeApps(apps, state.apps, kind) : NONE,
    [wants, state, apps, kind],
  );
  // A focus whose app is no longer on in the document is not shown.
  const focus = picked !== null && active.some((a) => a.app.id === picked.app.id) ? picked : null;
  const latest = useRef({ state, active, selection, conversation, noticeSeq: 0 });
  useEffect(() => {
    latest.current = { ...latest.current, state, active, selection, conversation };
  });

  const [hub] = useState(() => new HlHub(slotOrigin));
  useEffect(() => hub.setOrigin(slotOrigin), [hub, slotOrigin]);

  // What the runtime keeps of the document; the first answer says which apps are on in it.
  useEffect(() => {
    if (!wants) return;
    let live = true;
    let failing = false;
    const ask = () => {
      if (document.hidden) return;
      syncSinopia(machine, id, conversation).then(
        (next) => {
          if (!live) return;
          failing = false;
          setState(next);
        },
        (e: Error) => {
          // Said once for a run of failures: the page keeps asking, and a machine that is away is not a toast a second.
          if (live && !failing)
            toast(t('next.sinopia.syncFailed'), { description: e.message, tone: 'error' });
          failing = true;
        },
      );
    };
    ask();
    const timer = window.setInterval(ask, POLL_MS);
    return () => {
      live = false;
      window.clearInterval(timer);
    };
  }, [wants, machine, id, conversation]);

  const bound = active.some((a) => a.bindings !== null);
  useEffect(() => hub.setOn(bound), [hub, bound]);
  useEffect(() => hub.setMarks(state?.marks ?? [], state?.boards ?? []), [hub, state]);

  const pushSelection = useCallback(
    async (next: ISelectedNode[]) => {
      setSelection(next);
      try {
        setState(await syncSinopia(machine, id, latest.current.conversation, next));
      } catch (e) {
        toast(t('next.sinopia.syncFailed'), {
          description: e instanceof Error ? e.message : undefined,
          tone: 'error',
        });
      }
    },
    [machine, id],
  );

  const onClick = useCallback(
    async (got: Extract<THlEvent, { k: 'click' }>) => {
      const target = inspectorFor(latest.current.active, got.click.hl);
      // An app that binds nothing here leaves the board's click to the board.
      if (target === null) return;
      const view = target.app.views.find((v) => v.id === target.view);
      if (view === undefined) return;
      const node = selectedOf(got.click, got.board);
      await pushSelection([node]);
      setFocus({ app: target.app, view, field: got.click.field });
      latest.current.noticeSeq += 1;
      setNotice({
        seq: latest.current.noticeSeq,
        method: 'harnessly/notifications/selection',
        params: { document: id, field: got.click.field, nodes: [node] },
      });
    },
    [id, pushSelection],
  );

  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      const current = latest.current.state;
      const got = hub.onMessage(event, current?.boards ?? []);
      if (got?.k === 'hello') hub.setMarks(current?.marks ?? [], current?.boards ?? []);
      else if (got?.k === 'click') void onClick(got);
      else if (got === null && hub.knows(event)) {
        // A block picked on a board with the pick tool, when it is an element of the template.
        const message = readBridgeMessage(event.data);
        if (message?.k === 'picked' && message.picked.tpl !== undefined)
          void pushSelection([
            {
              tpl: message.picked.tpl,
              ...(message.picked.fp ? { fp: message.picked.fp } : {}),
              selector: message.picked.selector,
              tag: message.picked.tag,
              ...(message.picked.text ? { text: message.picked.text } : {}),
              rect: message.picked.rect,
              ...(message.picked.hl
                ? {
                    hl: message.picked.hl,
                    ...(message.picked.hl.field ? { field: message.picked.hl.field } : {}),
                  }
                : {}),
            },
          ]);
      }
    };
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, [hub, onClick, pushSelection]);

  const pickBlock = useCallback(
    (picked: IPickedBlock | null) => {
      const { selection: have, state: now } = latest.current;
      if (picked === null) {
        if (have.length > 0) void pushSelection([]);
        return;
      }
      const board = now?.boards.find((b) => b.file === picked.path);
      // The runtime has not said which boards the document has yet: the next selection finds them.
      if (board === undefined) return;
      const id = `${board.id}:${picked.tpl}`;
      if (have.length === 1 && have[0].id === id && have[0].text === picked.text) return;
      const [x, y, width, height] = picked.rect;
      void pushSelection([
        {
          id,
          page: board.id,
          tpl: picked.tpl,
          ...(picked.was ? { fp: picked.was } : {}),
          tag: picked.tag || 'div',
          ...(picked.text ? { text: picked.text } : {}),
          rect: { x, y, width, height },
        },
      ]);
    },
    [pushSelection],
  );

  const runAction = useCallback(
    async (action: ISelectionAction) => {
      const { selection: picked, conversation: chat } = latest.current;
      const { app, item } = action;
      if (item.agent !== undefined) {
        if (chat === null) {
          toast(app.name, { description: t('next.sinopia.noChat') });
          return;
        }
        sendMessage(machine, {
          conversationId: chat,
          text: selectionMessage({ document: id, app: app.name, task: item.agent, nodes: picked }),
        });
        return;
      }
      if (item.tool === undefined || kind === null) return;
      // A tool is called as a view calls it: through a session of the app for this document.
      const view = app.views.find((v) => v.path !== undefined && targetAllowed(v.targets, `sinopia.${kind}`));
      if (view === undefined) {
        toast(app.name, { description: t('next.sinopia.noView'), tone: 'error' });
        return;
      }
      try {
        const made = await createViewSession(machine, {
          app: app.id,
          view: view.id,
          target: `sinopia.${kind}`,
          conversation: null,
          document: id,
        });
        await callTool(machine, made.session, item.tool, { document: id, nodes: picked });
        toast(app.name, { description: item.title });
        setState(await syncSinopia(machine, id, chat));
      } catch (e) {
        toast(app.name, { description: e instanceof Error ? e.message : undefined, tone: 'error' });
      }
    },
    [machine, id, kind],
  );

  const gate = useCallback(
    (proceed: () => void) => {
      // Nothing to ask where no app on in the document has a check.
      if (!latest.current.active.some((a) => (a.app.sinopia?.checks?.length ?? 0) > 0)) {
        proceed();
        return;
      }
      void gateShare(() => runSinopiaChecks(machine, id, 'share'), proceed).then((got) => {
        if (got.ok) return;
        if ('error' in got) toast(t('next.sinopia.checkFailed'), { description: got.error, tone: 'error' });
        else setBlocked(got.results);
        // The marks the checks left are on the boards at once.
        void syncSinopia(machine, id, latest.current.conversation).then(setState, () => undefined);
      });
    },
    [machine, id],
  );

  const onViewCall = useCallback(
    (app: IInstalledApp, method: string, params: Record<string, unknown>) => {
      // A comment of a view is also a note to the chat linked to the document, under the app's name — only for an app
      // the person let write to the chat (chat.prompt); without it the comment stays a mark on the board.
      if (
        method === 'comment' &&
        app.permissions.includes('chat.prompt') &&
        latest.current.conversation !== null &&
        typeof params.node === 'string' &&
        typeof params.text === 'string'
      ) {
        const node = latest.current.selection.find((n) => n.id === params.node) ?? null;
        sendMessage(machine, {
          conversationId: latest.current.conversation,
          text: commentMessage(id, app.name, node, params.node, params.text),
        });
      }
      void syncSinopia(machine, id, latest.current.conversation).then(setState, () => undefined);
    },
    [machine, id],
  );

  return {
    active,
    viewPort: installed.list?.viewPort ?? null,
    state,
    selection,
    focus,
    notice,
    blocked,
    clearSelection: () => void pushSelection([]),
    closeFocus: () => setFocus(null),
    dismissBlocked: () => setBlocked(null),
    runAction,
    pickBlock,
    gate,
    onViewCall,
  };
};
