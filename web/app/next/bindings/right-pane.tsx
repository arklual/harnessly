// The open chat's right pane over the machine: the header's button, the tabs and what each shows. A tab is the
// launcher, the side chat (questions about this chat, the agent is not disturbed), an own terminal of the chat (a shell
// the machine keeps until the tab closes it) or a browser tab with its own history. Only tools the machine really has are offered; the rest of the design's list waits for its API.
import {
  useEffect,
  useEffectEvent,
  useMemo,
  useRef,
  useState,
  type ComponentType,
  type ReactNode,
} from 'react';
import PaneDrawer from '@/next/features/shell/pane-drawer';
import { isShareable } from '@/next/features/filedoc/api';
import { FileShareButton } from '@/next/features/filedoc/file-share';
import ToolPane, { TOOLS_TAB, type IPaneTab } from '@/next/features/pane/tool-pane';
import ToolsLauncher, { type ITool, type IToolGroup } from '@/next/features/pane/tools-launcher';
import BrowserView from '@/next/features/pane/browser-view';
import { deliveredViewOf } from '@/next/features/pane/delivered-kind';
import SideChat from '@/next/features/pane/side-chat';
import { SideFloat } from '@/next/features/pane/side-float';
import { onOpenSubagent, openSubagent } from '@/next/features/subagents/open';
import { workflowsOf } from '@/next/features/workflows/run';
import { onOpenNotePage } from '@/next/features/designly/open-note';
import { onOpenAttached } from '@/next/features/pane/open-attached';
import type { MessageRef } from '@/next/protocol/v2';
import { useSubagents } from '@/next/bindings/subagents';
import { setPaneOpen } from '@/next/bindings/pane-open';
import LazyFileEditor from '@/next/features/pane/editor/lazy';
import { listFolder, readFile, saveFile } from '@/next/features/pane/chat-files-api';
import { useFileEdits, type IFileEdits } from '@/next/bindings/file-edits';
import Modal from '@/next/features/shell/modal';
import { Button } from '@/next/ui/button';
import type { IDraft } from '@/next/features/pane/editor/file-editor';
import { useSideChat } from '@/next/bindings/side-chat';
import Icon from '@/next/ui/icon';
import LazyView from '@/next/ui/lazy-view';
import { CLIENT_ID } from '@/next/bindings/agent-terminal';
import { pageText } from '@/next/bindings/outcome';
import { closeTerminal, loadOpenTarget, loadTasks, loadTerminals, openTerminal } from '@/next/commands/pages';
import { machineApi } from '@/next/core/net/machine';
import { fetchPorts, openPreview, type IListeningPort } from '@/next/core/net/preview';
import { useLinkRestored } from '@/next/stores/connection';
import { useBackground } from '@/next/stores/conversation';
import { hasFeature, useMachines } from '@/next/stores/machines';
import type {
  ConvId,
  Conversation,
  DirListing,
  FileContent,
  OpenTarget,
  Task,
  TurnOutput,
} from '@/next/protocol/v2';
import { toast } from '@/next/ui/toast';
import { usePresence } from '@/next/ui/presence';
import { plural, t } from '@/next/i18n';
import { isLegacyTab, useLegacyPane, type ILegacyPane, type TLegacyHandoff } from '@/next/legacy/pane';
import { browserAt, placeRequest, type IPaneTabs, type TTab } from '@/next/features/pane/pane-tabs';
import { requestOf, type IPaneIntent, type IPaneRequest } from '@/next/features/pane/intents';
import { usePaneIntents } from '@/next/bindings/pane-intents';
import { usePhone } from '@/next/ui/use-media';
import { chatPaneOf, chatPanes, useChatPane, type IChatPane } from '@/next/stores/pane-state';
import { conversationsStore, convsLoadedStore } from '@/next/stores/conversations';

// The views a tab shows come in their own chunk the first time that tab opens, not with the page (a shell is xterm).
const later =
  <P extends object>(load: () => Promise<{ default: ComponentType<P> }>) =>
  (props: P) => <LazyView load={load} props={props} fallback={null} title={t('next.load.screen')} />;
const ShellView = later(() => import('@/next/features/pane/shell-view'));
const PlanView = later(() => import('@/next/features/pane/plan-view'));
const SubagentView = later(() => import('@/next/features/pane/subagent-view'));
const MachineTab = later(() => import('@/next/bindings/machine-tab'));
const DesignlyPane = later(() => import('@/next/bindings/designly-pane'));
const FilesView = later(() => import('@/next/features/pane/files-view'));
const DeliveredView = later(() => import('@/next/features/pane/delivered-view'));

/** The launcher's tools in the design's order; one not named keeps its place after them. */
const TOOL_ORDER = [
  'side',
  'plan',
  'L:changes',
  'subagents',
  'files',
  'L:gallery',
  'L:artifacts',
  'L:peers',
  'L:economics',
  'terminal',
  'shell',
  'browser',
  'L:disk',
  'machine',
  'L:gitlab',
  'designly',
  'L:vault',
] as const;
const rankOf = (id: string): number => {
  const at = (TOOL_ORDER as readonly string[]).indexOf(id);
  return at < 0 ? TOOL_ORDER.length : at;
};

/** Browser tabs take these slots: ports one, sites another (the runtime keeps 1–6 for ports, 7–8 for sites). */
const PORT_SLOT = 3;
const SITE_SLOT = 7;

/** The side chat's window: its exit, and the pause between the tab going and the window coming. */
const FLOAT_OUT_MS = 200;
const FLOAT_SWAP_MS = 120;
/** The question about unsaved text sinks out as long as a dialog does. */
const LEAVE_EXIT_MS = 220;

let tabSeq = 0;
/** Unique across reloads too: a chat's pane keeps its tabs (stores/pane-state.ts). */
const nextId = () => `p${Date.now().toString(36)}${(++tabSeq).toString(36)}`;

/** One line about each tool that comes from Legacy's pane, under its name in the tools. */
const LEGACY_HINTS: Record<string, string> = {
  'L:changes': 'next.pane.hint.changes',
  'L:gallery': 'next.pane.hint.gallery',
  'L:artifacts': 'next.pane.hint.artifacts',
  'L:peers': 'next.pane.hint.peers',
  'L:economics': 'next.pane.hint.economics',
  'L:disk': 'next.pane.hint.disk',
  'L:gitlab': 'next.pane.hint.gitlab',
  'L:vault': 'next.pane.hint.vault',
};

/** «Открыто: N» for the tools that can have several tabs; nothing while there are none. */
const openCount = (tabs: readonly TTab[], kind: TTab['kind']): string | undefined => {
  const n = tabs.filter((x) => x.kind === kind).length;
  return n > 0 ? t('next.pane.liveOpen', { n }) : undefined;
};

const tabInfo = (tab: TTab, machine: string, chat: string): IPaneTab & { sub: string } => {
  if (tab.kind === 'side')
    return {
      id: tab.id,
      icon: 'message-square-text',
      name: t('next.pane.side'),
      sub: t('next.pane.sideAbout', { title: chat }),
    };
  if (tab.kind === 'plan')
    return { id: tab.id, icon: 'list-checks', name: t('next.pane.plan'), sub: t('next.pane.planHint') };
  if (tab.kind === 'subagent')
    return { id: tab.id, icon: 'workflow', name: t('next.subs.pane'), sub: t('next.subs.paneHint') };
  if (tab.kind === 'machine')
    return { id: tab.id, icon: 'cpu', name: t('next.pane.machine'), sub: t('next.pane.machineHint') };
  if (tab.kind === 'designly')
    return { id: tab.id, icon: 'palette', name: t('next.pane.designly'), sub: t('next.pane.designlyHint') };
  if (tab.kind === 'files')
    return {
      id: tab.id,
      icon: 'files',
      name: (tab.file && tab.path.split('/').pop()) || t('next.pane.files'),
      sub: tab.path || t('next.pane.filesRoot'),
    };
  if (tab.kind === 'shell') return { id: tab.id, icon: 'terminal', name: t('next.pane.shell'), sub: machine };
  const at = tab.hist[tab.hi];
  return {
    id: tab.id,
    icon: 'globe',
    name: at ? at.text.split('/')[0] : t('next.pane.newTab'),
    sub: at ? at.text : t('next.pane.browser'),
  };
};

interface IBrowserTabProps {
  conv: Conversation;
  tab: Extract<TTab, { kind: 'browser' }>;
  onChange: (tab: Extract<TTab, { kind: 'browser' }>) => void;
  onNewTab: () => void;
}

/** One browser tab: the page in its slot, asked for again on every address and reload. */
const BrowserTab = ({ conv, tab, onChange, onNewTab }: IBrowserTabProps) => {
  const [reloadKey, setReloadKey] = useState(0);
  // A reload asked from outside the tab (the agent published the page again) is one more press of «Обновить».
  const asked = tab.reload ?? 0;
  const [heard, setHeard] = useState(asked);
  if (asked !== heard) {
    setHeard(asked);
    setReloadKey((n) => n + 1);
  }
  const [slot, setSlot] = useState<{ key: string; src: string | null; error: string | null } | null>(null);
  const [ports, setPorts] = useState<IListeningPort[] | null>(null);
  const [portsError, setPortsError] = useState<string | null>(null);
  const [portsAttempt, setPortsAttempt] = useState(0);
  const restored = useLinkRestored(conv.machineId);
  const at = tab.hist[tab.hi] ?? null;
  const key = at ? `${at.text}|${reloadKey}` : '';

  useEffect(() => {
    if (!at) return;
    let live = true;
    const slotNo = at.target.kind === 'port' ? PORT_SLOT : SITE_SLOT;
    const asked = `${at.text}|${reloadKey}`;
    const { target } = at;
    // A port nobody listens on gets words, not the browser's broken page in the frame.
    const down =
      target.kind === 'port'
        ? fetchPorts(conv.machineId, []).then(
            (list) => !list.some((p) => p.port === target.port),
            () => false,
          )
        : Promise.resolve(false);
    Promise.all([openPreview(conv.machineId, target, slotNo, at.path), down]).then(
      ([opened, isDown]) => {
        if (!live) return;
        if (isDown && target.kind === 'port')
          setSlot({ key: asked, src: null, error: t('next.pane.portDown', { port: target.port }) });
        else setSlot({ key: asked, src: opened.enterUrl, error: null });
      },
      (failure: Error) => live && setSlot({ key: asked, src: null, error: failure.message }),
    );
    return () => {
      live = false;
    };
  }, [conv.machineId, at, reloadKey]);

  const empty = at === null;
  useEffect(() => {
    if (!empty) return;
    const abort = new AbortController();
    fetchPorts(conv.machineId, [conv.cwd], abort.signal).then(
      (list) => {
        const shown = list.filter((p) => p.responding && p.kind === 'site');
        setPorts([...shown.filter((p) => p.group === 'chat'), ...shown.filter((p) => p.group !== 'chat')]);
        setPortsError(null);
      },
      (failure: Error) => {
        if (!abort.signal.aborted) setPortsError(failure.message);
      },
    );
    return () => abort.abort();
  }, [conv.machineId, conv.cwd, empty, portsAttempt, restored]);

  return (
    <BrowserView
      address={at?.text ?? ''}
      canBack={tab.hi > 0}
      canForward={tab.hi < tab.hist.length - 1}
      src={slot?.key === key ? slot.src : null}
      error={slot?.key === key ? slot.error : null}
      ports={ports}
      portsError={ports === null ? portsError : null}
      onRetryPorts={() => {
        setPortsError(null);
        setPortsAttempt((n) => n + 1);
      }}
      reloadKey={reloadKey}
      onGo={(next) => onChange({ ...tab, hist: [...tab.hist.slice(0, tab.hi + 1), next], hi: tab.hi + 1 })}
      onHistory={(step) =>
        onChange({ ...tab, hi: Math.max(0, Math.min(tab.hist.length - 1, tab.hi + step)) })
      }
      onReload={() => setReloadKey((n) => n + 1)}
      onNewTab={onNewTab}
    />
  );
};

/** How long the plan waits after the agent's state moved before it asks again (there is no event for the plan). */
const PLAN_DEBOUNCE_MS = 1000;

/** The plan, asked again as the agent works: on every change of its state, a second after it. */
const PlanTab = ({ conv }: { conv: Conversation }) => {
  const [tasks, setTasks] = useState<Task[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  // The link coming back asks again, quietly: the plan on screen stays until the new one is here.
  const restored = useLinkRestored(conv.machineId);
  const moved = `${conv.state.k}|${conv.state.since}|${conv.lastActivityAt}|${attempt}|${restored}`;
  useEffect(() => {
    let live = true;
    const timer = window.setTimeout(() => {
      void loadTasks(conv.machineId, conv.id).then((res) => {
        if (!live) return;
        if ('ok' in res) {
          setTasks(res.value);
          setError(null);
        } else setError(pageText(res));
      });
    }, PLAN_DEBOUNCE_MS);
    return () => {
      live = false;
      window.clearTimeout(timer);
    };
  }, [conv.machineId, conv.id, moved]);
  return (
    <PlanView
      tasks={tasks}
      error={error}
      onRetry={() => {
        setError(null);
        setAttempt((n) => n + 1);
      }}
    />
  );
};

/** The subagents the chat started, drawn off the stores; the one asked for comes open. */
const SubagentTab = ({ conv, focus }: { conv: Conversation; focus: string | null }) => {
  const tasks = useBackground(conv.id);
  const workflows = useMemo(() => workflowsOf(tasks), [tasks]);
  return <SubagentView agents={useSubagents(conv.id)} workflows={workflows} focus={focus} />;
};

interface IShownFile {
  key: string;
  listing: DirListing | null;
  file: FileContent | null;
  /** What the editor opens with: the text an earlier visit left unsaved, or the disk's. */
  initial: IDraft | null;
  error: string | null;
}

/** The chat's folder: the place on screen is the tab's, so the crumbs survive a switch of tabs. A file opens in the
 *  editor; leaving it with unsaved text asks first. */
const FilesTab = ({
  conv,
  tab,
  edits,
  guard,
  onChange,
}: {
  conv: Conversation;
  tab: Extract<TTab, { kind: 'files' }>;
  edits: IFileEdits;
  guard: (tabs: readonly string[], run: () => void) => void;
  onChange: (tab: Extract<TTab, { kind: 'files' }>) => void;
}) => {
  const [shown, setShown] = useState<IShownFile | null>(null);
  // A zip, a picture, a PDF or a file to be saved is not read as text: the pane shows it its own way.
  const delivered = tab.file ? deliveredViewOf(tab.path) : null;
  // A new epoch is the disk's text read again over the editor's.
  const [epoch, setEpoch] = useState(0);
  // A folder is read again when the link comes back; a file is not, the text in the editor is the person's.
  const restored = useLinkRestored(conv.machineId);
  const listingAgain = tab.file ? 0 : restored;
  const key = `${tab.file ? 'f' : 'd'}:${tab.path}:${epoch}`;
  const { draftOf } = edits;
  useEffect(() => {
    const abort = new AbortController();
    const asked = `${tab.file ? 'f' : 'd'}:${tab.path}:${epoch}`;
    const done = (next: Omit<IShownFile, 'key'>) => {
      if (!abort.signal.aborted) setShown({ key: asked, ...next });
    };
    // A listing that is on screen stays when asking it again fails (the link was away); only a place with nothing to
    // show says why.
    const fail = (failure: Error) => {
      if (abort.signal.aborted) return;
      setShown((current) =>
        current?.key === asked && (current.listing || current.file)
          ? current
          : { key: asked, listing: null, file: null, initial: null, error: failure.message },
      );
    };
    if (delivered) return undefined;
    if (tab.file)
      readFile(conv.machineId, conv.id, tab.path, abort.signal).then(
        (file) =>
          done({
            listing: null,
            file,
            initial: draftOf(tab.id, tab.path) ?? {
              text: file.content,
              base: file.content,
              revision: file.revision,
            },
            error: null,
          }),
        fail,
      );
    else
      listFolder(conv.machineId, conv.id, tab.path, abort.signal).then(
        (listing) => done({ listing, file: null, initial: null, error: null }),
        fail,
      );
    return () => abort.abort();
  }, [conv.machineId, conv.id, tab.id, tab.path, tab.file, epoch, draftOf, listingAgain, delivered]);
  const now = shown?.key === key ? shown : null;

  const save = async (text: string, revision: string, over: boolean) => {
    // Over whatever the disk has now: the revision it has now is the one the save names.
    const current = over ? (await readFile(conv.machineId, conv.id, tab.path)).revision : revision;
    return saveFile(conv.machineId, conv.id, tab.path, { content: text, revision: current });
  };
  const editor =
    now?.file && now.initial ? (
      <LazyFileEditor
        key={key}
        onHandle={edits.attach}
        path={now.file.path}
        initial={now.initial}
        onSave={save}
        onReload={() => {
          edits.discard([tab.id]);
          setEpoch((n) => n + 1);
        }}
        onLeave={(draft) => edits.onLeave(tab.id, tab.path, draft)}
        onState={(state) => edits.onState(tab.id, state)}
      />
    ) : null;
  if (delivered)
    return (
      <DeliveredView
        machine={conv.machineId}
        path={tab.path.startsWith('/') ? tab.path : `${conv.cwd.replace(/\/+$/, '')}/${tab.path}`}
        view={delivered}
      />
    );
  return (
    <FilesView
      root={conv.cwd}
      path={tab.path}
      listing={now?.listing ?? null}
      file={now?.file ?? null}
      editor={editor}
      error={now?.error ?? null}
      opening={tab.file ? 'file' : 'folder'}
      onRetry={() => setEpoch((n) => n + 1)}
      onOpen={(path, kind) => guard([tab.id], () => onChange({ ...tab, path, file: kind === 'file' }))}
    />
  );
};

interface IPaneBodyProps {
  conv: Conversation;
  machineName: string;
  tabs: TTab[];
  /** The tabs opened behind the one on screen and not looked at yet. */
  unread: readonly string[];
  setTabs: (change: (list: TTab[]) => TTab[]) => void;
  active: string;
  setActive: (id: string) => void;
  maxed: boolean;
  onToggleMax: () => void;
  onClose: () => void;
  revealAgentTerminal: (() => void) | null;
  side: ReturnType<typeof useSideChat>;
  /** The side chat opens over the window until it is docked; docked, it is a tab here. */
  sideDocked: boolean;
  onFloatSide: () => void;
  /** The tools Legacy's pane has; their tabs follow the pane's own in the strip. */
  legacy: ILegacyPane;
  /** The text being edited in the file tabs, and the question before any of it is given up. */
  edits: IFileEdits;
  guard: (tabs: readonly string[], run: () => void) => void;
  /** The header's buttons that stand in the strip while the pane is open. */
  strip: ReactNode;
}

const PaneBody = (props: IPaneBodyProps) => {
  const { conv, machineName, tabs, setTabs, active, setActive, edits, guard } = props;
  const [opening, setOpening] = useState(false);
  const running =
    useSubagents(conv.id).filter((x) => x.status === 'running').length +
    useBackground(conv.id).filter((x) => x.kind === 'workflow' && x.status === 'running').length;
  const shells = hasFeature(conv.machineId, 'terminal');
  const machineTool = hasFeature(conv.machineId, 'machine');
  const { legacy } = props;
  const legacyTab = isLegacyTab(active) ? legacy.tabs.find((x) => x.id === active) : undefined;
  // Neither is open: the tools, the permanent first tab (it is not among the pane's own).
  const tab = legacyTab ? undefined : tabs.find((x) => x.id === active);
  // A kept Legacy tab in front waits for Legacy's pane to come, rather than the tools showing for a moment.
  const launcher = !legacyTab && !tab && !(isLegacyTab(active) && !legacy.on);

  const groups: IToolGroup[] = [
    { id: 'chat', label: t('next.pane.chatGroup') },
    { id: 'vm', label: t('next.pane.machineGroup', { name: machineName }) },
    { id: 'links', label: t('next.pane.linksGroup') },
  ];
  const tools: ITool[] = [
    // The side chat forks the conversation's Claude session: Codex has none to fork.
    ...(conv.provider === 'claude'
      ? [
          {
            id: 'side',
            group: 'chat',
            icon: 'message-square-text',
            name: t('next.pane.side'),
            hint: t('next.pane.sideHint'),
            kbd: '⌥⌘S',
          },
        ]
      : []),
    {
      id: 'plan',
      group: 'chat',
      icon: 'list-checks',
      name: t('next.pane.plan'),
      hint: t('next.pane.planHint'),
    },
    { id: 'files', group: 'chat', icon: 'files', name: t('next.pane.files'), hint: t('next.pane.filesHint') },
    ...(props.revealAgentTerminal
      ? [
          {
            id: 'terminal',
            group: 'vm',
            icon: 'square-terminal',
            name: t('next.pane.agentTerminal'),
            hint: t('next.pane.agentTerminalHint'),
            kbd: '⌃`',
          },
        ]
      : []),
    ...(shells
      ? [
          {
            id: 'shell',
            group: 'vm',
            icon: 'terminal',
            name: t('next.pane.shell'),
            hint: t('next.pane.shellHint', { name: machineName }),
            live: openCount(tabs, 'shell'),
          },
        ]
      : []),
    {
      id: 'browser',
      group: 'vm',
      icon: 'globe',
      name: t('next.pane.browser'),
      hint: t('next.pane.browserHint'),
      live: openCount(tabs, 'browser'),
    },
    ...(machineTool
      ? [
          {
            id: 'machine',
            group: 'vm',
            icon: 'cpu',
            name: t('next.pane.machine'),
            hint: t('next.pane.machineHint'),
          },
        ]
      : []),
    {
      id: 'subagents',
      group: 'chat',
      icon: 'workflow',
      name: t('next.subs.pane'),
      hint: t('next.subs.paneHint'),
      live: running > 0 ? plural(running, 'next.bg.agentsRun') : undefined,
    },
    ...legacy.tools.map((x) => ({ ...x, hint: LEGACY_HINTS[x.id] ? t(LEGACY_HINTS[x.id]) : '' })),
    {
      id: 'designly',
      group: 'links',
      icon: 'palette',
      name: t('next.pane.designly'),
      hint: t('next.pane.designlyHint'),
    },
  ];
  // The design's order inside a group: what the chat did first, then what it made, then what it cost.
  tools.sort((a, b) => rankOf(a.id) - rankOf(b.id));

  /** What was picked in the tools opens as a tab beside them, which the tools stay. */
  const add = (next: TTab) => {
    setTabs((list) => [...list, next]);
    setActive(next.id);
  };

  const openTool = (id: string) => {
    if (isLegacyTab(id)) {
      setActive(legacy.open(id));
      return;
    }
    if (id === 'terminal') {
      props.revealAgentTerminal?.();
      return;
    }
    if (id === 'designly') {
      const have = tabs.find((x) => x.kind === 'designly');
      if (have) setActive(have.id);
      else add({ id: nextId(), kind: 'designly', design: null });
      return;
    }
    if (id === 'side' && !props.sideDocked) {
      props.onFloatSide();
      return;
    }
    if (id === 'subagents') {
      const have = tabs.find((x) => x.kind === 'subagent');
      // The tile asks for the list: a card an earlier «Открыть» put in focus does not stay open under it.
      if (have) {
        setTabs((list) =>
          list.map((x) => (x.id === have.id && x.kind === 'subagent' ? { ...x, focus: null } : x)),
        );
        setActive(have.id);
      } else add({ id: nextId(), kind: 'subagent', focus: null });
      return;
    }
    if (id === 'side' || id === 'plan' || id === 'files' || id === 'machine') {
      const have = tabs.find((x) => x.kind === id);
      if (have) setActive(have.id);
      else
        add(
          id === 'files'
            ? { id: nextId(), kind: 'files', path: '', file: false }
            : { id: nextId(), kind: id },
        );
      return;
    }
    if (id === 'browser') {
      // A browser tab that has no page yet is the new tab: it is looked at, not stacked on.
      const blank = tabs.find((x) => x.kind === 'browser' && x.hi < 0);
      if (blank) setActive(blank.id);
      else add({ id: nextId(), kind: 'browser', hist: [], hi: -1 });
      return;
    }
    if (id !== 'shell' || opening) return;
    setOpening(true);
    void openTerminal(conv.machineId, conv.id).then((res) => {
      setOpening(false);
      if ('ok' in res) {
        add({ id: nextId(), kind: 'shell', terminal: res.value.id });
        return;
      }
      const limit = res.r === 'refused' && res.code === 'conflict';
      toast(limit ? t('next.pane.shellLimit') : t('next.pane.shellFailed'), {
        tone: 'error',
        description: limit ? undefined : pageText(res),
      });
    });
  };

  const removeTab = (id: string) => {
    const at = tabs.findIndex((x) => x.id === id);
    const rest = tabs.filter((x) => x.id !== id);
    setTabs(() => rest);
    // The neighbour comes forward; with none left, the tools do.
    if (id === tab?.id) setActive(rest[Math.max(0, at - 1)]?.id ?? legacy.tabs[0]?.id ?? TOOLS_TAB);
  };

  const closeTab = (id: string) => {
    if (isLegacyTab(id)) {
      const at = legacy.tabs.findIndex((x) => x.id === id);
      legacy.close(id);
      if (id === active)
        setActive(legacy.tabs[at + 1]?.id ?? legacy.tabs[at - 1]?.id ?? tabs.at(-1)?.id ?? TOOLS_TAB);
      return;
    }
    const closing = tabs.find((x) => x.id === id);
    if (closing?.kind !== 'shell') {
      guard([id], () => removeTab(id));
      return;
    }
    // The shell ends on the machine first: a tab that went while its shell stayed would leave it running unseen.
    void closeTerminal(conv.machineId, conv.id, closing.terminal).then((res) => {
      if ('ok' in res || res.r === 'refused') {
        removeTab(id);
        return;
      }
      toast(t('next.pane.shellCloseFailed'), { tone: 'error', description: pageText(res) });
    });
  };

  const { side } = props;
  const saveButton = tab?.kind === 'files' && edits.live?.tab === tab.id;
  const paneActions = () => {
    if (tab?.kind === 'side')
      return (
        <>
          {side.messages.length > 0 && (
            <button
              type="button"
              className="hx-head-btn hx-tipped"
              aria-label={t('next.side.clear')}
              onClick={side.clear}
            >
              <Icon name="eraser" size="sm" />
              <span className="hx-tip" data-pos="below" data-align="end">
                {t('next.side.clear')}
              </span>
            </button>
          )}
          <button
            type="button"
            className="hx-head-btn hx-tipped"
            aria-label={t('next.side.float')}
            onClick={props.onFloatSide}
          >
            <Icon name="picture-in-picture-2" size="sm" />
            <span className="hx-tip" data-pos="below" data-align="end">
              {t('next.side.floatHint')}
            </span>
          </button>
        </>
      );
    if (!tab || tab.kind !== 'files') return null;
    // «Поделиться» of a file that can be a document, next to «Сохранить».
    const share = tab.file && isShareable(tab.path) && (
      <FileShareButton key={tab.path} machine={conv.machineId} conv={conv.id} path={tab.path} />
    );
    if (!saveButton) return share || null;
    return (
      <>
        {share}
        <Button
          variant="primary"
          size="sm"
          busy={edits.live?.saving}
          disabled={!edits.unsaved.has(tab.id)}
          onClick={() => void edits.save()}
        >
          {t('next.editor.save')}
        </Button>
      </>
    );
  };
  const actions = paneActions();

  return (
    <ToolPane
      tabs={[
        ...tabs.map((x) => ({
          ...tabInfo(x, machineName, conv.title),
          dirty: edits.unsaved.has(x.id),
          unread: props.unread.includes(x.id),
        })),
        ...legacy.tabs,
      ]}
      tab={legacyTab?.id ?? tab?.id ?? TOOLS_TAB}
      ownBar={tab?.kind === 'browser' || tab?.kind === 'designly'}
      maxed={props.maxed}
      onPick={(id) => {
        if (isLegacyTab(id)) legacy.pick(id);
        setActive(id);
      }}
      onCloseTab={closeTab}
      onToggleMax={props.onToggleMax}
      onClose={() =>
        guard(
          tabs.map((x) => x.id),
          props.onClose,
        )
      }
      actions={actions}
      strip={props.strip}
    >
      {legacyTab && legacy.render(legacyTab.id)}
      {launcher && (
        <ToolsLauncher
          tools={tools}
          groups={groups}
          onOpen={openTool}
          onAddress={(address) => {
            // An address already open in a tab is that tab, not a second one.
            const have = tabs.find((x) => browserAt(x, address.text));
            if (have) setActive(have.id);
            else add({ id: nextId(), kind: 'browser', hist: [address], hi: 0 });
          }}
        />
      )}
      {tab?.kind === 'side' && (
        <SideChat
          messages={side.messages}
          busy={side.busy}
          onAsk={(q) => side.ask(q)}
          onStop={side.stop}
          onRetry={side.retry}
        />
      )}
      {tab?.kind === 'plan' && <PlanTab key={tab.id} conv={conv} />}
      {tab?.kind === 'subagent' && <SubagentTab key={tab.id} conv={conv} focus={tab.focus} />}
      {tab?.kind === 'machine' && (
        <MachineTab key={tab.id} machineId={conv.machineId} machineName={machineName} />
      )}
      {tab?.kind === 'designly' && (
        <DesignlyPane
          key={tab.id}
          machine={conv.machineId}
          conv={conv.id}
          design={tab.design}
          onOpen={(design) => setTabs((list) => list.map((x) => (x.id === tab.id ? { ...tab, design } : x)))}
        />
      )}
      {tab?.kind === 'files' && (
        <FilesTab
          key={tab.id}
          conv={conv}
          tab={tab}
          edits={edits}
          guard={guard}
          onChange={(next) => setTabs((list) => list.map((x) => (x.id === next.id ? next : x)))}
        />
      )}
      {tab?.kind === 'shell' && (
        <ShellView
          key={tab.id}
          url={machineApi(conv.machineId).ws(
            `/terminal?conversation=${encodeURIComponent(conv.id)}&terminal=${encodeURIComponent(tab.terminal)}&clientId=${CLIENT_ID}`,
          )}
        />
      )}
      {tab?.kind === 'browser' && (
        <BrowserTab
          key={tab.id}
          conv={conv}
          tab={tab}
          onChange={(next) => setTabs((list) => list.map((x) => (x.id === next.id ? next : x)))}
          onNewTab={() => {
            const added: TTab = { id: nextId(), kind: 'browser', hist: [], hi: -1 };
            setTabs((list) => [...list, added]);
            setActive(added.id);
          }}
        />
      )}
    </ToolPane>
  );
};

/** How long the kept panes wait after the lists moved before the chats that went are forgotten. */
const PRUNE_MS = 2000;

/**
 * The pane of the open chat. Its tabs belong to the chat and are kept with it (stores/pane-state.ts): another chat
 * opens with its own, coming back finds the pane as it was left, and the shells the machine still keeps for a chat
 * come back as tabs when its pane opens.
 */
export const useRightPane = (
  conv: Conversation | null,
  machineName: string,
  revealAgentTerminal: (() => void) | null,
  strip: ReactNode = null,
) => {
  useMachines();
  const side = useSideChat(conv?.machineId ?? null, conv?.id ?? null);
  const convId = conv?.id ?? null;
  const machineId = conv?.machineId ?? null;
  // The chat's pane is read as the chat is: the first frame of another chat already draws its own pane.
  const state = useChatPane(convId);
  const { open, maxed } = state;
  const setState = (change: (pane: IChatPane) => IChatPane) => {
    if (convId) chatPanes.update(convId, machineId, change);
  };
  const setOpen = (next: boolean) => setState((s) => (s.open === next ? s : { ...s, open: next }));
  // The chat on screen now, for what answers after the person went to another one.
  const shownConv = useRef(convId);
  useEffect(() => {
    shownConv.current = convId;
  }, [convId]);
  // The side chat over the window: open, leaving (its exit plays), or not there; docked, it is a tab of the pane.
  const [float, setFloat] = useState<'open' | 'closing' | null>(null);
  const [floatConv, setFloatConv] = useState(convId);
  const [docked, setDocked] = useState(false);
  const floatTimer = useRef(0);
  const restored = useRef<string | null>(null);
  const edits = useFileEdits();
  // Giving up unsaved text asks first: what is asked, and what happens once the person agrees.
  const [leaving, setLeaving] = useState<{ tabs: string[]; run: () => void } | null>(null);
  const [leaveBusy, setLeaveBusy] = useState(false);
  // What has to give way to the pane reads it from the store (pane-open.ts).
  useEffect(() => {
    setPaneOpen(open);
    return () => setPaneOpen(false);
  }, [open]);
  // Another chat: the side chat's window was the other chat's.
  if (floatConv !== convId) {
    setFloatConv(convId);
    setFloat(null);
  }
  useEffect(() => () => window.clearTimeout(floatTimer.current), []);
  // The chats their machine no longer lists take their kept panes with them.
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | null = null;
    const prune = () => {
      timer = null;
      chatPanes.prune({
        listed: (machine) => convsLoadedStore.get(machine) === true,
        exists: (id) => id === shownConv.current || conversationsStore.get(id as ConvId) !== undefined,
      });
    };
    const later = () => {
      timer ??= setTimeout(prune, PRUNE_MS);
    };
    later();
    const offLoaded = convsLoadedStore.subscribeAll(later);
    const offConvs = conversationsStore.subscribeAll(later);
    return () => {
      if (timer !== null) clearTimeout(timer);
      offLoaded();
      offConvs();
    };
  }, []);

  const leaveFloat = () => {
    setFloat('closing');
    window.clearTimeout(floatTimer.current);
    floatTimer.current = window.setTimeout(() => setFloat(null), FLOAT_OUT_MS);
  };
  const openFloat = () => {
    window.clearTimeout(floatTimer.current);
    setFloat('open');
  };

  const setTabs = (change: (list: TTab[]) => TTab[]) => setState((s) => ({ ...s, tabs: change(s.tabs) }));
  const setMaxed = (next: boolean) => setState((s) => (s.maxed === next ? s : { ...s, maxed: next }));
  const setActive = (id: string) =>
    setState((s) => ({ ...s, active: id, unread: s.unread.filter((x) => x !== id) }));

  /** Runs `run` — which gives up the text of these tabs — at once, or after the person agrees. */
  const guard = (ids: readonly string[], run: () => void) => {
    const unsaved = ids.filter((id) => edits.unsaved.has(id) && state.tabs.some((x) => x.id === id));
    if (unsaved.length === 0) run();
    else setLeaving({ tabs: unsaved, run });
  };
  const closePane = () =>
    guard(
      state.tabs.map((x) => x.id),
      () => setOpen(false),
    );
  const onEscape = useEffectEvent(closePane);
  const unsavedTabs = state.tabs.filter((x) => edits.unsaved.has(x.id)).length;
  useEffect(() => {
    if (unsavedTabs === 0) return;
    const warn = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [unsavedTabs]);

  const show = () => setOpen(true);

  // The shells that did not come are asked again with a button on the toast, and by themselves when the link returns.
  const [terminalsAttempt, setTerminalsAttempt] = useState(0);
  const linkBack = useLinkRestored(conv?.machineId ?? null);
  useEffect(() => {
    if (!open || !conv || !hasFeature(conv.machineId, 'terminal')) return;
    const asked = `${conv.id}|${terminalsAttempt}|${linkBack}`;
    if (restored.current === asked) return;
    restored.current = asked;
    const forConv = conv.id;
    const ours = new Set(chatPaneOf(forConv).tabs.flatMap((x) => (x.kind === 'shell' ? [x.terminal] : [])));
    void loadTerminals(conv.machineId, conv.id).then((res) => {
      if (!('ok' in res)) {
        toast(t('next.load.terminals'), {
          tone: 'error',
          description: pageText(res),
          action: { label: t('next.load.retry'), onClick: () => setTerminalsAttempt((n) => n + 1) },
        });
        return;
      }
      const live = new Set(res.value.map((x) => x.id));
      chatPanes.update(forConv, conv.machineId, (s) => {
        // A kept shell tab whose shell the machine ended (a restart, a timeout) goes; a shell opened since the question
        // went out stays.
        const dead = s.tabs.filter(
          (x) => x.kind === 'shell' && ours.has(x.terminal) && !live.has(x.terminal),
        );
        const known = new Set(s.tabs.flatMap((x) => (x.kind === 'shell' ? [x.terminal] : [])));
        const back: TTab[] = res.value
          .filter((x) => !known.has(x.id))
          .map((x) => ({ id: nextId(), kind: 'shell', terminal: x.id }));
        if (back.length === 0 && dead.length === 0) return s;
        const tabs = [...back, ...s.tabs.filter((x) => !dead.includes(x))];
        const active = dead.some((x) => x.id === s.active) ? (tabs[0]?.id ?? '') : s.active;
        return { ...s, tabs, active, unread: s.unread.filter((id) => tabs.some((x) => x.id === id)) };
      });
    });
  }, [open, conv, terminalsAttempt, linkBack]);

  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented) return;
      // The editor's own Esc (search, completions) stays in it.
      if ((event.target as Element | null)?.closest?.('.xterm, .hx-fed')) return;
      onEscape();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open]);

  /** Docked: the window goes and the pane opens on the side chat's tab. */
  const dockSide = () => {
    setDocked(true);
    leaveFloat();
    setOpen(true);
    setState((s) => {
      const have = s.tabs.find((x) => x.kind === 'side');
      if (have) return { ...s, active: have.id };
      const added: TTab = { id: nextId(), kind: 'side' };
      return { ...s, tabs: [...s.tabs, added], active: added.id };
    });
  };
  /** Undocked: the tab goes (and the pane with it when it was the last) and the window opens. */
  const floatSide = () => {
    setDocked(false);
    const at = state.tabs.findIndex((x) => x.kind === 'side');
    if (at >= 0) {
      const rest = state.tabs.filter((_, i) => i !== at);
      const active =
        state.tabs[at].id === state.active ? (rest[Math.max(0, at - 1)]?.id ?? '') : state.active;
      setState((s) => ({ ...s, tabs: rest, active }));
      if (rest.length === 0) setOpen(false);
    }
    window.clearTimeout(floatTimer.current);
    floatTimer.current = window.setTimeout(openFloat, FLOAT_SWAP_MS);
  };

  // ⌥⌘S: the side chat where it lives, docked or over the window; again, the window goes.
  const onSideKey = useEffectEvent(() => {
    if (docked) dockSide();
    else if (float === 'open') {
      side.stop();
      leaveFloat();
    } else openFloat();
  });
  const sideKeyOn = conv?.provider === 'claude';
  useEffect(() => {
    if (!sideKeyOn) return;
    const onKey = (event: KeyboardEvent) => {
      if (!(event.altKey && (event.metaKey || event.ctrlKey) && event.code === 'KeyS')) return;
      event.preventDefault();
      onSideKey();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [sideKeyOn]);

  // What Legacy's pane got for the tools this pane has of its own comes here: an address to a browser tab, the plan
  // and the side chat to theirs.
  const handoff = (asked: TLegacyHandoff[]) => {
    setOpen(true);
    setState((s) => {
      // An address already open in a tab, the plan and the side chat that are there come forward instead of a second one.
      let tabs: IPaneTabs = s;
      for (const x of asked) {
        const have = x.kind === 'side' ? tabs.tabs.find((y) => y.kind === 'side') : undefined;
        if (have) tabs = { ...tabs, active: have.id, unread: tabs.unread.filter((y) => y !== have.id) };
        else if (x.kind === 'side') {
          const added: TTab = { id: nextId(), kind: 'side' };
          tabs = { ...tabs, tabs: [...tabs.tabs, added], active: added.id };
        } else {
          const request: IPaneRequest =
            x.kind === 'browser'
              ? { k: 'browser', target: x.target, reload: false }
              : { k: 'plan', reload: false };
          tabs = placeRequest(tabs, request, { busy: false, cwd: conv?.cwd ?? '', nextId }).next;
        }
      }
      return { ...s, ...tabs };
    });
  };
  const legacy = useLegacyPane(conv, {
    open,
    // Coming to a chat, its kept front stays; Legacy's front comes only where nothing was kept.
    onForward: (id, switched) => {
      if (!switched || state.active === '') setActive(id);
    },
    onHandoff: handoff,
  });

  // What the agent asked its pane to show, and what the runtime asked on seeing it work (a design it edits, a document
  // it publishes): each goes to its tab, in front or, while the person is busy in another tab, behind it with a dot;
  // the pane comes out for it with its motion. On the phone the pane is a sheet over the chat: it is not thrown over
  // what the person reads, the tabs wait under its button.
  const phone = usePhone();
  const applyRequests = (requests: readonly IPaneRequest[]) => {
    if (!conv) return;
    const busy = open && state.active !== '' && state.active !== TOOLS_TAB;
    let tabs: IPaneTabs = state;
    let front: string | null = null;
    let show = false;
    for (const request of requests) {
      const placed = placeRequest(tabs, request, { busy, cwd: conv.cwd, nextId });
      tabs = placed.next;
      show = show || placed.show;
      const effect = placed.effect;
      if (effect?.k === 'legacy') {
        if (effect.reload) legacy.reload(effect.kind, effect.target);
        else {
          const id = legacy.show(effect.kind, effect.target, effect.background);
          if (id && !effect.background) front = id;
        }
      } else if (effect?.k === 'terminal') revealAgentTerminal?.();
      else if (effect?.k === 'side') {
        if (docked) dockSide();
        else openFloat();
      }
    }
    const active = front ?? tabs.active;
    setState((s) => ({
      ...s,
      tabs: tabs.tabs,
      active,
      unread: tabs.unread.filter((x) => x !== active),
    }));
    if (show && !phone) setOpen(true);
  };
  // A plain function: the hook below calls the newest one from its own effect event.
  const applyIntents = (intents: IPaneIntent[]) =>
    applyRequests(intents.flatMap((intent) => requestOf(intent.command) ?? []));
  usePaneIntents(conv?.machineId ?? null, legacy.tabId, applyIntents);
  // What was asked for this chat while the person was in another one is placed now that they are back; a tab of
  // Legacy's pane waits for it to come.
  const applyPending = useEffectEvent(applyRequests);
  const legacyOn = legacy.on;
  useEffect(() => {
    if (!convId) return;
    // After the chat's first frame, which draws the pane as it was kept; what waited then moves in like a request.
    const timer = setTimeout(() => {
      const now = chatPaneOf(convId).pending.filter((x) => x.k !== 'legacy' || legacyOn);
      if (now.length === 0) return;
      chatPanes.update(convId, null, (s) => ({ ...s, pending: s.pending.filter((x) => !now.includes(x)) }));
      applyPending(now);
    }, 0);
    return () => clearTimeout(timer);
  }, [convId, legacyOn]);

  // A subagent the feed or the column names opens in the pane's own tab of subagents, on its card.
  const showSubagent = useEffectEvent((taskId: string) => {
    setOpen(true);
    setState((s) => {
      const have = s.tabs.find((x) => x.kind === 'subagent');
      if (have) {
        const tabs = s.tabs.map((x) => (x.id === have.id ? { ...have, focus: taskId } : x));
        return { ...s, tabs, active: have.id };
      }
      const added: TTab = { id: nextId(), kind: 'subagent', focus: taskId };
      return { ...s, tabs: [...s.tabs, added], active: added.id };
    });
  });
  useEffect(() => onOpenSubagent(showSubagent), []);

  // A sent note's chip opens its design in the pane's Designly tab; the viewer turns to the note's page itself.
  const showNotePage = useEffectEvent((design: string) => {
    setOpen(true);
    setState((s) => {
      const have = s.tabs.find((x) => x.kind === 'designly');
      if (have) {
        const tabs = s.tabs.map((x) => (x.id === have.id ? { ...have, design } : x));
        return { ...s, tabs, active: have.id };
      }
      const added: TTab = { id: nextId(), kind: 'designly', design };
      return { ...s, tabs: [...s.tabs, added], active: added.id };
    });
  });
  useEffect(() => onOpenNotePage((place) => showNotePage(place.design)), []);

  // What the feed names opens here (the design's paneOpenFile, openLink): a file of the turn in the chat's folder, a
  // link in a browser tab.
  const openFile = (path: string) => {
    if (!conv) return;
    const root = conv.cwd.replace(/\/+$/, '');
    const rel = path.startsWith(`${root}/`) ? path.slice(root.length + 1) : path;
    const tab: TTab = { id: nextId(), kind: 'files', path: rel, file: true };
    const replaced = state.tabs.filter((x) => x.kind === 'files').map((x) => x.id);
    guard(replaced, () => {
      edits.discard(replaced);
      setOpen(true);
      setState((s) => ({ ...s, tabs: [...s.tabs.filter((x) => x.kind !== 'files'), tab], active: tab.id }));
    });
  };
  // A place the runtime named opens in its own tab: a task, a merge request or a document, any other site in the
  // browser.
  const openTarget = ({ tab, target }: OpenTarget) => {
    if (tab === 'browser') {
      handoff([{ kind: 'browser', target }]);
      return;
    }
    const id = legacy.show(tab, target);
    if (!id) {
      toast(t('next.pane.notYet'), { tone: 'error' });
      return;
    }
    setOpen(true);
    setActive(id);
  };
  // A chip or a link of the feed opens where the agent's open command would put it (the runtime's rules). A ref that
  // fits several places opens the best; a merge request chip asks the person first (its own list, before this).
  const openRef = (ref: string) => {
    if (!conv) return;
    const forConv = conv.id;
    const forMachine = conv.machineId;
    void loadOpenTarget(conv.machineId, conv.id, ref).then((res) => {
      if ('ok' in res && shownConv.current !== forConv) {
        // The person went to another chat meanwhile: the place waits for them in the chat it was asked from.
        const request = requestOf({ kind: 'open', tab: res.value.tab, target: res.value.target });
        if (request) chatPanes.queue(forConv, forMachine, request);
        return;
      }
      if (!('ok' in res)) {
        const nothing = res.r === 'refused' && res.code === 'not-found';
        if (nothing) {
          toast(t('next.pane.nothingToOpen'), { tone: 'error' });
          return;
        }
        toast(t('next.load.openRef'), {
          tone: 'error',
          description: pageText(res),
          action: { label: t('next.load.retry'), onClick: () => openRef(ref) },
        });
        return;
      }
      openTarget(res.value);
    });
  };

  // A design the turn changed opens in the pane's Designly tab, on the design itself.
  const openDesign = (design: string) => {
    if (!conv) return;
    setOpen(true);
    setState((s) => {
      const have = s.tabs.find((x) => x.kind === 'designly');
      if (have) {
        const tabs = s.tabs.map((x) => (x.id === have.id ? { ...have, design } : x));
        return { ...s, tabs, active: have.id };
      }
      const added: TTab = { id: nextId(), kind: 'designly', design };
      return { ...s, tabs: [...s.tabs, added], active: added.id };
    });
  };
  // A card of what a turn made opens where its kind lives: a design in Designly, a document of the repository in the
  // Files, a page, a merge request or a stand where the runtime says (the catalogue's tab, GitLab, the browser).
  const openOutput = (output: TurnOutput) => {
    if (output.kind === 'design') openDesign(output.ref);
    else if (output.kind === 'doc' || output.kind === 'delivery') openFile(output.ref);
    else openRef(output.ref);
  };

  // A place attached to a message (its chip in the field or the feed, a design as it is attached) opens where its kind
  // lives: a design in Designly, a file or a document of the project in the Files, the rest where the runtime says.
  const showAttached = useEffectEvent((ref: Pick<MessageRef, 'kind' | 'id'>) => {
    if (ref.kind === 'design') openDesign(ref.id);
    else if (ref.kind === 'file' || (ref.kind === 'doc' && !/^https?:\/\//.test(ref.id))) openFile(ref.id);
    else openRef(ref.id);
  });
  useEffect(() => onOpenAttached(showAttached), []);

  // On the phone the pane is a sheet over the chat: the agent's terminal opens under the chat, so the sheet goes.
  const revealTerminalOver =
    revealAgentTerminal &&
    (() => {
      if (phone) setOpen(false);
      revealAgentTerminal();
    });
  const header = conv ? { open, onToggle: () => (open ? setOpen(false) : show()) } : null;
  const drawer = conv && (
    <PaneDrawer
      open={open}
      onClose={closePane}
      wide={maxed}
      width={state.width}
      onWidth={(width) => setState((s) => (s.width === width ? s : { ...s, width }))}
    >
      <PaneBody
        conv={conv}
        machineName={machineName}
        tabs={state.tabs}
        unread={state.unread}
        setTabs={setTabs}
        active={state.active}
        setActive={setActive}
        maxed={maxed}
        onToggleMax={() => setMaxed(!maxed)}
        onClose={() => setOpen(false)}
        revealAgentTerminal={revealTerminalOver}
        side={side}
        sideDocked={docked}
        onFloatSide={floatSide}
        legacy={legacy}
        edits={edits}
        guard={guard}
        strip={strip}
      />
      {legacy.extras}
    </PaneDrawer>
  );
  const dialogLeave = usePresence(leaving, LEAVE_EXIT_MS).value;
  const leaveSaveable =
    dialogLeave !== null && dialogLeave.tabs.length === 1 && edits.live?.tab === dialogLeave.tabs[0];
  const leaveDialog = (
    <Modal
      open={leaving !== null}
      onClose={() => setLeaving(null)}
      title={t('next.editor.leaveTitle')}
      lead={t('next.editor.leaveLead')}
      boxClass="hx-leave"
    >
      <div className="hx-modal__foot">
        <Button variant="ghost" onClick={() => setLeaving(null)}>
          {t('next.editor.leaveStay')}
        </Button>
        <Button
          variant="danger"
          onClick={() => {
            if (!leaving) return;
            edits.discard(leaving.tabs);
            leaving.run();
            setLeaving(null);
          }}
        >
          {t('next.editor.leaveDiscard')}
        </Button>
        {leaveSaveable && (
          <Button
            variant="primary"
            busy={leaveBusy}
            onClick={() => {
              if (!leaving) return;
              setLeaveBusy(true);
              void edits.save().then((saved) => {
                setLeaveBusy(false);
                setLeaving(null);
                if (saved) leaving.run();
              });
            }}
          >
            {t('next.editor.save')}
          </Button>
        )}
      </div>
    </Modal>
  );
  const sideFloat = conv && float && (
    <SideFloat
      about={conv.title}
      closing={float === 'closing'}
      messages={side.messages}
      busy={side.busy}
      onAsk={(q) => side.ask(q)}
      onStop={side.stop}
      onRetry={side.retry}
      onClear={side.clear}
      onDock={dockSide}
      onClose={() => {
        side.stop();
        leaveFloat();
      }}
    />
  );
  return {
    header,
    drawer: drawer && (
      <>
        {drawer}
        {leaveDialog}
      </>
    ),
    open,
    sideFloat,
    openFile,
    openRef,
    openOutput,
    openTarget,
    /** Opens a subagent (its id, a background task's id) in the pane: for the rows of the column of background work. */
    openSubagent,
  };
};
