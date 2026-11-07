// The open chat's right pane over the machine: the header's button, the tabs and what each shows. A tab is the
// launcher, the side chat (questions about this chat, the agent is not disturbed), an own terminal of the chat (a shell
// the machine keeps until the tab closes it) or a browser tab with its own history. Only tools the machine really has are offered; the rest of the design's list waits for its API.
import { requestFilePlace } from '@/next/features/pane/file-place';
import {
  useCallback,
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
import MadeBlock from '@/next/features/pane/made-block';
import { heldTools, quietTools, toolOfRequest } from '@/next/features/pane/tools-order';
import { useChatMade } from '@/next/bindings/chat-made';
import BrowserTab from '@/next/bindings/browser-tab';
import { deliveredViewOf } from '@/next/features/pane/delivered-kind';
import { crumbsOf } from '@/next/features/pane/crumbs';
import { downloadHref, saveFrom } from '@/next/features/pane/download';
import { useFolderUploads } from '@/next/legacy/folder-uploads';
import FileMenu from '@/next/features/pane/file-menu';
import SideChat from '@/next/features/pane/side-chat';
import { SideFloat } from '@/next/features/pane/side-float';
import { onOpenSubagent, openSubagent } from '@/next/features/subagents/open';
import AppTab from '@/next/features/app-views/app-tab';
import { useInstalledApps } from '@/next/features/app-views/api';
import { onOpenAddressTab, onOpenAppTab, openAppTab } from '@/next/features/app-views/open';
import { workflowsOf } from '@/next/features/workflows/run';
import { onOpenNotePage } from '@/next/features/designly/open-note';
import { onOpenAttached } from '@/next/features/pane/open-attached';
import type { MadeItem, MessageRef } from '@/next/protocol/v2';
import { loadKindDocs } from '@/next/features/app-catalog/api';
import { goTo, ROUTE } from '@/next/routes/routes';
import { useSubagents } from '@/next/bindings/subagents';
import { setPaneOpen } from '@/next/bindings/pane-open';
import { usePaneOpenBeside } from '@/next/bindings/pane-beside';
import LazyFileEditor from '@/next/features/pane/editor/lazy';
import {
  findFiles,
  findText,
  entryOp,
  listFolder,
  readFile,
  readGitSide,
  saveFile,
} from '@/next/features/pane/chat-files-api';
import type { ICompare } from '@/next/features/pane/editor/file-editor';
import { addQuote } from '@/next/stores/quotes';
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
import { machineApi, runtimeSocket } from '@/next/core/net/machine';
import { useLinkRestored } from '@/next/stores/connection';
import { useBackground } from '@/next/stores/conversation';
import { hasFeature, useMachines } from '@/next/stores/machines';
import type {
  ConvId,
  MachineId,
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
import { MACHINE, browserAt, placeRequest, type IPaneTabs, type TTab } from '@/next/features/pane/pane-tabs';
import { requestOf, type IPaneIntent, type IPaneRequest } from '@/next/features/pane/intents';
import { usePaneIntents } from '@/next/bindings/pane-intents';
import { foldRailForDesign } from '@/next/bindings/rail';
import { usePhone } from '@/next/ui/use-media';
import { KEY_SCOPE } from '@/next/features/keys/commands';
import { useKeys } from '@/next/features/keys/use-keys';
import { useKeyHint } from '@/next/features/keys/keys-help';
import { chatPaneOf, chatPanes, markUsed, useChatPane, type IChatPane } from '@/next/stores/pane-state';
import { conversationsStore, convsLoadedStore } from '@/next/stores/conversations';
import { useAppsDebug } from '@/next/features/app-views/debug-mode';

// The views a tab shows come in their own chunk the first time that tab opens, not with the page (a shell is xterm).
const later =
  <P extends object>(load: () => Promise<{ default: ComponentType<P> }>) =>
  (props: P) => <LazyView load={load} props={props} fallback={null} title={t('next.load.screen')} />;
const ShellView = later(() => import('@/next/features/pane/shell-view'));
const PlanView = later(() => import('@/next/features/pane/plan-view'));
const SubagentView = later(() => import('@/next/features/pane/subagent-view'));
const MachineTab = later(() => import('@/next/bindings/machine-tab'));
const DesignlyPane = later(() => import('@/next/bindings/designly-pane'));

/** A design opened beside the chat, full screen: Sinopia Design with this chat; a document of an app's type, its own page. */
const openDesignFull = async (machine: MachineId, design: string, chat: ConvId) => {
  const docs = await loadKindDocs(machine).catch(() => []);
  foldRailForDesign();
  goTo(
    docs.some((d) => d.document === design)
      ? { kind: ROUTE.appDoc, id: design, chat }
      : { kind: ROUTE.site, id: design, chat, machine },
  );
};
const FilesView = later(() => import('@/next/features/pane/files-view'));
const UploadsStrip = later(() => import('@/next/legacy/uploads-strip'));
const DeliveredView = later(() => import('@/next/features/pane/delivered-view'));
const AgentBrowserView = later(() => import('@/next/features/pane/agent-browser-view'));

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
  'agent-browser',
  'machine',
  'L:gitlab',
  'designly',
  'L:vault',
] as const;
const rankOf = (id: string): number => {
  const at = (TOOL_ORDER as readonly string[]).indexOf(id);
  return at < 0 ? TOOL_ORDER.length : at;
};

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
  'L:gitlab': 'next.pane.hint.gitlab',
  'L:vault': 'next.pane.hint.vault',
};

/** «Открыто: N» for the tools that can have several tabs; nothing while there are none. */
/** The id of an app's tile in the tools: the prefix, then the app's id. */
const APP_TOOL = 'app:';

const openCount = (tabs: readonly TTab[], kind: TTab['kind']): string | undefined => {
  const n = tabs.filter((x) => x.kind === kind).length;
  return n > 0 ? t('next.pane.liveOpen', { n }) : undefined;
};

const tabInfo = (
  tab: TTab,
  machine: string,
  chat: string,
  appName: (id: string) => string | null,
): IPaneTab & { sub: string } => {
  if (tab.kind === 'app')
    return {
      id: tab.id,
      icon: 'app-window',
      name: appName(tab.app) ?? tab.app,
      sub: t('next.appview.tabHint'),
    };
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
  if (tab.kind === 'agent-browser')
    return {
      id: tab.id,
      icon: 'agent-browser',
      name: t('next.agentBrowser.name'),
      sub: t('next.agentBrowser.hint'),
    };
  if (tab.kind === 'shell') return { id: tab.id, icon: 'terminal', name: t('next.pane.shell'), sub: machine };
  const at = tab.hist[tab.hi];
  return {
    id: tab.id,
    icon: 'globe',
    name: at ? (at.title ?? at.text.split('/')[0]) : t('next.pane.newTab'),
    sub: at ? at.text : t('next.pane.browser'),
  };
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

/** A path of the chat's folder made absolute; an absolute one stays. */
/** The folder of the machine a tab is in: its own, or its file's; null for the chat's folder. */
const machineFolderOf = (path: string, file: boolean): string | null => {
  if (!path.startsWith('/')) return null;
  if (!file) return path;
  return path.slice(0, path.lastIndexOf('/')) || '/';
};

const absIn = (cwd: string, path: string) =>
  path.startsWith('/') ? path : `${cwd.replace(/\/+$/, '')}/${path}`;

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
  onOpenBeside,
  tools,
}: {
  conv: Conversation;
  tab: Extract<TTab, { kind: 'files' }>;
  edits: IFileEdits;
  guard: (tabs: readonly string[], run: () => void) => void;
  onChange: (tab: Extract<TTab, { kind: 'files' }>) => void;
  /** Opens a file or a folder in a tab of its own beside this one. */
  onOpenBeside: (path: string, kind: 'file' | 'directory') => void;
  /** The open text file's controls, in the folder's head row beside the crumbs. */
  tools: ReactNode;
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
  // «Перейти к файлу» searches the chat's folder; on the machine, the folder on screen (a file's own folder).
  const searchIn = machineFolderOf(tab.path, tab.file);
  const find = useCallback(
    async (query: string, signal: AbortSignal) => {
      const found = await findFiles(conv.machineId, searchIn ?? conv.cwd, query, signal);
      return searchIn === null ? found : found.map((p) => `${searchIn.replace(/\/+$/, '')}/${p}`);
    },
    [conv.machineId, conv.cwd, searchIn],
  );
  const findLines = useCallback(
    (query: string, signal: AbortSignal) => findText(conv.machineId, conv.id, searchIn ?? '', query, signal),
    [conv.machineId, conv.id, searchIn],
  );
  // New files, new names and the trash in the folder on screen; the list of the places has none.
  const entryOps = useMemo(
    () =>
      tab.file || tab.path === MACHINE
        ? null
        : {
            newFile: (name: string) => entryOp(conv.machineId, conv.id, 'newFile', tab.path, name),
            rename: (path: string, name: string) => entryOp(conv.machineId, conv.id, 'rename', path, name),
            trash: (path: string) => entryOp(conv.machineId, conv.id, 'trash', path),
          },
    [conv.machineId, conv.id, tab.file, tab.path],
  );
  // Files sent into the folder on screen and new folders in it: the list is read again as each arrives.
  const uploads = useFolderUploads(conv.id, tab.file ? null : absIn(conv.cwd, tab.path), () =>
    setEpoch((n) => n + 1),
  );

  // The state the file is shown against: git's revision or another file, read when the tab asks for it.
  const [compare, setCompare] = useState<{ key: string; value: ICompare } | null>(null);
  const against = tab.file ? (tab.against ?? null) : null;
  useEffect(() => {
    if (against === null) return undefined;
    const abort = new AbortController();
    const key = `${tab.path}|${against}`;
    const git = against.startsWith('git:') ? against.slice(4) : null;
    const label = git
      ? t('next.editor.diffGit', { ref: git })
      : t('next.editor.diffFile', { name: against.slice(against.lastIndexOf('/') + 1) });
    const put = (value: ICompare) => {
      if (!abort.signal.aborted) setCompare({ key, value });
    };
    put({ label, text: null, failure: null });
    const failed = (failure: Error) => put({ label, text: null, failure: failure.message });
    if (git)
      readGitSide(conv.machineId, conv.id, tab.path, git, abort.signal).then((side) => {
        if (!side.inRepo) put({ label, text: null, failure: t('next.editor.diffNoRepo') });
        else if (!side.tracked) put({ label, text: '', failure: null });
        else if (side.binary || side.truncated)
          put({ label, text: null, failure: t('next.editor.diffTooBig') });
        else put({ label, text: side.content, failure: null });
      }, failed);
    else
      readFile(conv.machineId, conv.id, against, abort.signal).then(
        (other) => put({ label, text: other.content, failure: null }),
        failed,
      );
    return () => abort.abort();
  }, [conv.machineId, conv.id, tab.path, against]);
  const shownCompare = against !== null && compare?.key === `${tab.path}|${against}` ? compare.value : null;

  // A page's neighbours (styles, scripts, pictures) read the way a download does: the same places, no secrets.
  const readAsset = useCallback(
    async (path: string) => {
      const res = await fetch(downloadHref(conv.machineId, conv.id, [path]), { credentials: 'same-origin' });
      return res.ok ? res.blob() : null;
    },
    [conv.machineId, conv.id],
  );
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
        readAsset={readAsset}
        compare={shownCompare}
        onCloseCompare={() => onChange({ ...tab, against: undefined })}
        at={tab.at ?? null}
        onAsk={(quote, note) => {
          addQuote(conv.id, quote, note);
          toast(t('next.editor.asked'), { tone: 'success' });
        }}
      />
    ) : null;
  if (delivered)
    return (
      <DeliveredView
        conv={conv.id}
        machine={conv.machineId}
        path={tab.path.startsWith('/') ? tab.path : `${conv.cwd.replace(/\/+$/, '')}/${tab.path}`}
        view={delivered}
        crumbs={crumbsOf(conv.cwd, tab.path, undefined, true)}
        onCrumb={(path) =>
          guard([tab.id], () => onChange({ ...tab, path, file: false, against: undefined, at: undefined }))
        }
        tools={tools}
      />
    );
  return (
    <FilesView
      root={conv.cwd}
      path={tab.path}
      listing={now?.listing ?? null}
      file={now?.file ?? null}
      editor={editor}
      tools={tools}
      error={now?.error ?? null}
      opening={tab.file ? 'file' : 'folder'}
      onRetry={() => setEpoch((n) => n + 1)}
      onOpen={(path, kind) =>
        guard([tab.id], () =>
          onChange({ ...tab, path, file: kind === 'file', against: undefined, at: undefined }),
        )
      }
      onCompare={(a, b) => guard([tab.id], () => onChange({ ...tab, path: a, file: true, against: b }))}
      onDownload={(paths) => saveFrom(downloadHref(conv.machineId, conv.id, paths))}
      take={uploads.ready ? uploads : null}
      find={find}
      findText={findLines}
      onOpenAt={(hit, size) => {
        const at = { line: hit.line, col: hit.col, size };
        const show = () => onChange({ ...tab, path: hit.path, file: true, against: undefined, at });
        if (tab.file && tab.path === hit.path) show();
        else guard([tab.id], show);
      }}
      below={<UploadsStrip uploads={uploads.uploads} queue={uploads.queue} />}
      entryOps={entryOps}
      onOpenBeside={onOpenBeside}
      menuOf={(path, kind, trigger, acts) => (
        <FileMenu
          trigger={trigger}
          machine={conv.machineId}
          conv={conv.id}
          path={path}
          abs={absIn(conv.cwd, path)}
          kind={kind}
          onOpenBeside={() => onOpenBeside(path, kind)}
          onRename={acts?.onRename}
          onTrash={acts?.onTrash}
        />
      )}
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
  /** When each tool was last opened in this chat. */
  used: Readonly<Record<string, number>>;
  /** Tools were opened in this chat now. */
  onUsed: (ids: readonly string[]) => void;
  /** A thing the chat made is picked in the tools home. */
  onOpenMade: (item: Pick<MadeItem, 'kind' | 'ref'>) => void;
  /** The pane stands beside an open design: its Designly tab and tool are left out (the design is the screen). */
  besideDesign: boolean;
}

const PaneBody = (props: IPaneBodyProps) => {
  const sideKey = useKeyHint('global.sideChat');
  const { conv, machineName, setTabs, active, setActive, edits, guard } = props;
  const tabs = props.besideDesign ? props.tabs.filter((x) => x.kind !== 'designly') : props.tabs;
  const [opening, setOpening] = useState(false);
  const subagents = useSubagents(conv.id);
  const running =
    subagents.filter((x) => x.status === 'running').length +
    useBackground(conv.id).filter((x) => x.kind === 'workflow' && x.status === 'running').length;
  const shells = hasFeature(conv.machineId, 'terminal');
  const machineTool = hasFeature(conv.machineId, 'machine');
  const { legacy } = props;
  const installed = useInstalledApps(conv.machineId);
  const legacyTab = isLegacyTab(active) ? legacy.tabs.find((x) => x.id === active) : undefined;
  // Neither is open: the tools, the permanent first tab (it is not among the pane's own).
  const tab = legacyTab ? undefined : tabs.find((x) => x.id === active);
  // A kept Legacy tab in front waits for Legacy's pane to come, rather than the tools showing for a moment.
  const launcher = !legacyTab && !tab && !(isLegacyTab(active) && !legacy.on);

  const made = useChatMade(conv, launcher);

  const groups: IToolGroup[] = [
    { id: 'chat', label: t('next.pane.chatGroup') },
    { id: 'vm', label: t('next.pane.machineGroup', { name: machineName }) },
    { id: 'apps', label: t('next.pane.appsGroup') },
    { id: 'links', label: t('next.pane.linksGroup') },
  ];
  // The apps on in this chat that have a view for the pane: a tile each, which opens (or brings forward) its tab.
  const paneApps = (installed.list?.apps ?? []).filter(
    (a) => conv.apps?.includes(a.id) && a.views.some((v) => v.targets.includes('pane')),
  );
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
            kbd: sideKey ?? undefined,
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
    ...(hasFeature(conv.machineId, 'agent-browser')
      ? [
          {
            id: 'agent-browser',
            group: 'vm',
            icon: 'agent-browser',
            name: t('next.agentBrowser.name'),
            hint: t('next.agentBrowser.hint'),
          },
        ]
      : []),
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
    ...paneApps.map((a) => ({
      id: `${APP_TOOL}${a.id}`,
      group: 'apps',
      icon: a.icon || 'layout-grid',
      name: a.name,
      hint: a.description,
      live: openCount(
        tabs.filter((x) => x.kind === 'app' && x.app === a.id),
        'app',
      ),
    })),
    ...legacy.tools.map((x) => ({ ...x, hint: LEGACY_HINTS[x.id] ? t(LEGACY_HINTS[x.id]) : '' })),
    ...(props.besideDesign
      ? []
      : [
          {
            id: 'designly',
            group: 'links',
            icon: 'palette',
            name: t('next.pane.designly'),
            hint: t('next.pane.designlyHint'),
          },
        ]),
  ];
  // The design's order inside a group: what the chat did first, then what it made, then what it cost.
  tools.sort((a, b) => rankOf(a.id) - rankOf(b.id));
  // A chat that used some tools keeps the others in a quiet row: what it opened, and what it holds content for.
  const held = heldTools({
    subagents: subagents.length > 0,
    side: props.side.messages.length > 0,
    tabKinds: tabs.map((x) => x.kind),
    pictures: made.holds?.pictures ?? false,
    documents: made.holds?.documents ?? false,
    designs: made.holds?.designs ?? false,
  });
  const quiet = quietTools(tools, new Set([...held, ...Object.keys(props.used)]));
  // An app on in the chat is never quiet: the person turned it on to use it.
  const marked = tools.map((x) =>
    quiet.has(x.id) && !x.id.startsWith(APP_TOOL) ? { ...x, quiet: true } : x,
  );

  /** What was picked in the tools opens as a tab beside them, which the tools stay. */
  const add = (next: TTab) => {
    setTabs((list) => [...list, next]);
    setActive(next.id);
  };

  const openTool = (id: string) => {
    props.onUsed([id]);
    if (id.startsWith(APP_TOOL)) {
      openAppTab(id.slice(APP_TOOL.length));
      return;
    }
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
    if (id === 'side' || id === 'plan' || id === 'files' || id === 'machine' || id === 'agent-browser') {
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
  // The tabs of dev apps whose inspector is open (a section under the view, not a tab of its own).
  const [inspecting, setInspecting] = useState<ReadonlySet<string>>(new Set());
  const debugMode = useAppsDebug();
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
    if (tab?.kind === 'app' && (debugMode || installed.list?.apps.find((a) => a.id === tab.app)?.dev)) {
      const on = inspecting.has(tab.id);
      return (
        <button
          type="button"
          className="hx-head-btn hx-tipped"
          aria-label={t('next.appmain.debug')}
          aria-pressed={on}
          onClick={() =>
            setInspecting((now) => {
              const next = new Set(now);
              if (!next.delete(tab.id)) next.add(tab.id);
              return next;
            })
          }
        >
          <Icon name="bug" size="sm" />
          <span className="hx-tip" data-pos="below" data-align="end">
            {t('next.appmain.debug')}
          </span>
        </button>
      );
    }
    // A file has its controls in its head row, beside the crumbs (fileTools).
    return null;
  };
  /** «…» of the open file; `compare`: «Сравнить с git» (a text file). */
  const fileMenu = (compare: boolean) =>
    tab?.kind === 'files' &&
    tab.file && (
      <FileMenu
        key={`menu:${tab.path}`}
        trigger={
          <button type="button" className="hx-head-btn" aria-label={t('next.fileMenu.label')}>
            <Icon name="ellipsis" size="sm" />
          </button>
        }
        machine={conv.machineId}
        conv={conv.id}
        path={tab.path}
        abs={absIn(conv.cwd, tab.path)}
        kind="file"
        onCompare={
          compare
            ? () =>
                setTabs((list) => list.map((x) => (x.id === tab.id ? { ...tab, against: 'git:HEAD' } : x)))
            : undefined
        }
      />
    );
  /** «Поделиться» of a file that can be a document. */
  const fileShare = () =>
    tab?.kind === 'files' &&
    tab.file &&
    isShareable(tab.path) && (
      <FileShareButton key={tab.path} machine={conv.machineId} conv={conv.id} path={tab.path} />
    );
  /** The open file's controls: download, share, «…», and «Сохранить» while a text file has something to save. A file
   *  shown its own way (a picture, a PDF) saves itself: its «…» has no «Сравнить с git». */
  const fileTools = () => {
    if (!tab || tab.kind !== 'files' || !tab.file) return null;
    const text = deliveredViewOf(tab.path) === null;
    const unsaved = edits.unsaved.has(tab.id) || (saveButton && edits.live?.saving === true);
    return (
      <>
        <a
          className="hx-head-btn hx-tipped"
          href={downloadHref(conv.machineId, conv.id, [tab.path])}
          download
          aria-label={t('next.files.downloadFile')}
        >
          <Icon name="download" size="sm" />
          <span className="hx-tip" data-pos="below" data-align="end">
            {t('next.files.downloadFile')}
          </span>
        </a>
        {fileShare()}
        {fileMenu(text)}
        {text && saveButton && unsaved && (
          <Button variant="primary" size="sm" busy={edits.live?.saving} onClick={() => void edits.save()}>
            {t('next.editor.save')}
          </Button>
        )}
      </>
    );
  };
  const actions = paneActions();

  return (
    <ToolPane
      tabs={[
        ...tabs.map((x) => ({
          ...tabInfo(
            x,
            machineName,
            conv.title,
            (id) => installed.list?.apps.find((a) => a.id === id)?.name ?? null,
          ),
          dirty: edits.unsaved.has(x.id),
          unread: props.unread.includes(x.id),
        })),
        ...legacy.tabs,
      ]}
      tab={legacyTab?.id ?? tab?.id ?? TOOLS_TAB}
      ownBar={tab?.kind === 'browser' || tab?.kind === 'agent-browser' || tab?.kind === 'designly'}
      maxed={props.maxed}
      onPick={(id) => {
        if (isLegacyTab(id)) legacy.pick(id);
        setActive(id);
      }}
      onCloseTab={closeTab}
      onToggleMax={() => {
        // A design opened beside the chat goes full screen as Sinopia Design, with this chat as its chat.
        if (tab?.kind === 'designly' && tab.design !== null && !props.maxed)
          void openDesignFull(conv.machineId, tab.design, conv.id);
        // An app goes full screen as the app's own page, with this chat beside it.
        else if (tab?.kind === 'app' && !props.maxed) goTo({ kind: ROUTE.app, id: tab.app, chat: conv.id });
        else props.onToggleMax();
      }}
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
          tools={marked}
          groups={groups}
          ready={made.holds !== null}
          head={
            <MadeBlock
              cards={made.cards}
              onOpen={(card) => {
                const item = made.open(card);
                if (item) props.onOpenMade(item);
              }}
            />
          }
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
      {tab?.kind === 'app' && (
        <AppTab
          key={tab.id}
          machine={conv.machineId}
          conversation={conv.id}
          app={tab.app}
          view={tab.view}
          inspect={inspecting.has(tab.id)}
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
          onOpenBeside={(path, kind) => add({ id: nextId(), kind: 'files', path, file: kind === 'file' })}
          tools={fileTools()}
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
      {tab?.kind === 'agent-browser' && (
        <AgentBrowserView
          key={`${conv.machineId}:${conv.id}:${tab.id}`}
          machine={conv.machineId}
          conv={conv.id}
          url={runtimeSocket(`/api/agent-browser?conv=${encodeURIComponent(conv.id)}`, conv.machineId)}
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
  /** The pane stands beside an open design, not a chat. */
  besideDesign = false,
) => {
  useMachines();
  const side = useSideChat(conv?.machineId ?? null, conv?.id ?? null);
  const convId = conv?.id ?? null;
  const machineId = conv?.machineId ?? null;
  // The chat's pane is read as the chat is: the first frame of another chat already draws its own pane.
  const state = useChatPane(convId);
  // Beside a design the pane opens at its side, never over it: the chat's own «wide» is for the chat's page (a pane
  // widened there must not cover the design opened from it). Widened here, it stays so only while the design is open.
  const [wideBeside, setWideBeside] = useState(false);
  const maxed = besideDesign ? wideBeside : state.maxed;
  const setState = (change: (pane: IChatPane) => IChatPane) => {
    if (convId) chatPanes.update(convId, machineId, change);
  };
  // Beside a design the pane is out only once it is opened there (pane-beside).
  const [open, setOpen] = usePaneOpenBeside(
    state.open,
    (next) => setState((s) => (s.open === next ? s : { ...s, open: next })),
    besideDesign,
    convId,
  );
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
  const setMaxed = (next: boolean) => {
    if (besideDesign) setWideBeside(next);
    else setState((s) => (s.maxed === next ? s : { ...s, maxed: next }));
  };
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
  const onSideKey = () => {
    if (docked) dockSide();
    else if (float === 'open') {
      side.stop();
      leaveFloat();
    } else openFloat();
  };
  // The pane's shortcuts (features/keys): the pane itself, and the side chat of a Claude chat.
  useKeys(
    KEY_SCOPE.global,
    {
      'global.pane': () => (open ? closePane() : show()),
      'global.sideChat': conv?.provider === 'claude' ? onSideKey : undefined,
    },
    conv !== null,
  );

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
  /** `asked`: the person asked for it themselves (a view's button), so it comes in front whatever tab they are in. */
  const applyRequests = (requests: readonly IPaneRequest[], asked = false) => {
    if (!conv) return;
    const busy = !asked && open && state.active !== '' && state.active !== TOOLS_TAB;
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
    const opened = requests.flatMap((request) => toolOfRequest(request) ?? []);
    setState((s) => ({
      ...s,
      tabs: tabs.tabs,
      used: opened.length > 0 ? markUsed(s.used, opened, Date.now()) : s.used,
      active,
      unread: tabs.unread.filter((x) => x !== active),
    }));
    if (show && !phone) setOpen(true);
  };
  // A plain function: the hook below calls the newest one from its own effect event.
  const applyIntents = (intents: IPaneIntent[]) => {
    for (const { command } of intents)
      if (command.kind === 'open' && command.target && command.place)
        requestFilePlace(command.target, command.place);
    applyRequests(intents.flatMap((intent) => requestOf(intent.command) ?? []));
  };
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

  // A view's hl.pane.open and a card's «Открыть в панели» ask for an app's tab like the agent's `open app` does.
  const showApp = useEffectEvent((target: string) => applyRequests([{ k: 'app', target, reload: false }]));
  useEffect(() => onOpenAppTab(showApp), []);
  // A view's hl.preview.open: its dev server in the pane's browser, as `open <port>` of the agent, in front: the
  // person pressed the view's button for it.
  const showAddress = useEffectEvent((address: string) =>
    applyRequests([{ k: 'browser', target: address, reload: false }], true),
  );
  useEffect(() => onOpenAddressTab(showAddress), []);

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

  // A thing the chat made opens where its kind lives: a design in Designly, a document or a board where the runtime says.
  const openMade = (item: Pick<MadeItem, 'kind' | 'ref'>) => {
    if (item.kind === 'design') openDesign(item.ref);
    else openRef(item.ref);
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
        used={state.used}
        onUsed={(ids) => setState((s) => ({ ...s, used: markUsed(s.used, ids, Date.now()) }))}
        onOpenMade={openMade}
        besideDesign={besideDesign}
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
