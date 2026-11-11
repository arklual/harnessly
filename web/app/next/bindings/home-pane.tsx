// The right pane of the start page, where no chat is open (the chat's own is bindings/right-pane.tsx). It has every
// tool that needs no chat, in the same tiles as a chat's pane (features/pane/tools-launcher.tsx): the machine's —
// «Терминал», «Браузер», «Файлы» in its projects' folder, «Машина» with its load — on the machine picked at the
// «Машина …» label when the person has several; the services — GitLab (my merge requests and my reviews in every
// project), «Сейф», Sinopia Design; and «Ревью» — a project of the deployment's GitLab, then a merge request, a branch
// or a commit. What only a chat has (its plan, changes, subagents, side chat, correspondence, spend) is named in one
// quiet line. A shell and the files go to the machine's own place (features/pane/place.ts: HOME_CONV) instead of a
// chat's folder. «Спросить агента» on a change starts a new chat about it. Its state is kept as a chat's pane is
// (stores/home-pane-state.ts).
import { Suspense, lazy, useEffect, useEffectEvent, useState, type ComponentType } from 'react';
import PaneDrawer from '@/next/features/shell/pane-drawer';
import ToolPane, { TOOLS_TAB } from '@/next/features/pane/tool-pane';
import ToolsLauncher, { type ITool, type IToolGroup } from '@/next/features/pane/tools-launcher';
import MachinePick from '@/next/features/pane/machine-pick';
import { HOME_CONV, type IPlace } from '@/next/features/pane/place';
import { browserAt, type TTab } from '@/next/features/pane/pane-tabs';
import type { IAddress } from '@/next/features/pane/address';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { Skeleton } from '@/next/ui/skeleton';
import LazyView from '@/next/ui/lazy-view';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';
import { KEY_SCOPE } from '@/next/features/keys/commands';
import { useKeys } from '@/next/features/keys/use-keys';
import { setPaneOpen } from '@/next/bindings/pane-open';
import { createConversation } from '@/next/commands';
import { closeTerminal, openTerminal } from '@/next/commands/pages';
import { failureText, pageText } from '@/next/bindings/outcome';
import { CLIENT_ID } from '@/next/bindings/agent-terminal';
import BrowserTab from '@/next/bindings/browser-tab';
import { FilesTab, openDesignFull } from '@/next/bindings/right-pane';
import { useFileEdits } from '@/next/bindings/file-edits';
import { LegacyVault } from '@/next/legacy/pane';
import { launchModel } from '@/next/features/composer/agents';
import { useStartChoice } from '@/next/bindings/use-start-choice';
import { useDefaultCwd } from '@/next/stores/projects';
import { hasFeature, useMachines } from '@/next/stores/machines';
import { machineApi } from '@/next/core/net/machine';
import { startFolder } from '@/next/bindings/home-composer';
import { goTo, ROUTE } from '@/next/routes/routes';
import {
  HOME_TAB,
  updateHomePane,
  useHomePaneState,
  type IHomePane,
  type THomeTab,
  type TMachineTab,
} from '@/next/stores/home-pane-state';
import {
  askMessage,
  askRef,
  changeLinkOf,
  changeTitle,
  type IReviewContext,
} from '@/next/features/review/gitlab-pick';
import type { IPicked } from '@/next/features/review/review-picker';
import type { IMachineChoice } from '@/next/features/sidebar/types';
import type { Conversation, ConvId, MachineId } from '@/next/protocol/v2';
import '@/next/styles/review-picker.css';

const ReviewPicker = lazy(() => import('@/next/features/review/review-picker'));
const MyChanges = lazy(() =>
  import('@/next/features/review/review-picker').then((m) => ({ default: m.MyChanges })),
);
const ReviewChange = lazy(() => import('@/next/legacy/change').then((m) => ({ default: m.ReviewChange })));
const later =
  <P extends object>(load: () => Promise<{ default: ComponentType<P> }>) =>
  (props: P) => <LazyView load={load} props={props} fallback={null} title={t('next.load.screen')} />;
const ShellView = later(() => import('@/next/features/pane/shell-view'));
const MachineTab = later(() => import('@/next/bindings/machine-tab'));
const DesignlyLibrary = later<{ machine: MachineId; onOpen: (id: string) => void }>(() =>
  import('@/next/bindings/designly').then((m) => ({ default: m.DesignlyLibrary })),
);

/** The tools of the start page's pane. */
export const HOME_TOOL = {
  shell: 'shell',
  browser: 'browser',
  files: 'files',
  machine: 'machine',
  gitlab: 'gitlab',
  vault: 'vault',
  designly: 'designly',
  review: 'review',
} as const;

/** The groups of the tiles, in the design's order: the machine's, the services, the code. */
export const HOME_GROUP = { vm: 'vm', links: 'links', review: 'review' } as const;

export interface IHomeToolsInput {
  /** The machine the tools open on, by name; null while there is none: its tools are not there. */
  machineName: string | null;
  /** The machine has these tools (hello's features): its own shells, its load. */
  shells: boolean;
  machineTool: boolean;
}

/** What the start page's pane offers: every tool that needs no chat (pure: the tests count the tiles). */
export const homeTools = ({ machineName, shells, machineTool }: IHomeToolsInput): ITool[] => {
  const vm: ITool[] =
    machineName === null
      ? []
      : [
          ...(shells
            ? [
                {
                  id: HOME_TOOL.shell,
                  group: HOME_GROUP.vm,
                  icon: 'terminal',
                  name: t('next.pane.shell'),
                  hint: t('next.pane.shellHint', { name: machineName }),
                },
              ]
            : []),
          {
            id: HOME_TOOL.browser,
            group: HOME_GROUP.vm,
            icon: 'globe',
            name: t('next.pane.browser'),
            hint: t('next.pane.browserHint'),
          },
          {
            id: HOME_TOOL.files,
            group: HOME_GROUP.vm,
            icon: 'files',
            name: t('next.pane.files'),
            hint: t('next.review.homeFilesHint'),
          },
          ...(machineTool
            ? [
                {
                  id: HOME_TOOL.machine,
                  group: HOME_GROUP.vm,
                  icon: 'cpu',
                  name: t('next.pane.machine'),
                  hint: t('next.pane.machineHint'),
                },
              ]
            : []),
        ];
  return [
    ...vm,
    {
      id: HOME_TOOL.gitlab,
      group: HOME_GROUP.links,
      icon: 'git-merge',
      name: t('next.review.gitlab'),
      hint: t('next.review.gitlabHint'),
    },
    {
      id: HOME_TOOL.vault,
      group: HOME_GROUP.links,
      icon: 'key-round',
      name: t('next.review.vault'),
      hint: t('next.pane.hint.vault'),
    },
    {
      id: HOME_TOOL.designly,
      group: HOME_GROUP.links,
      icon: 'palette',
      name: t('next.pane.designly'),
      hint: t('next.pane.designlyHint'),
    },
    {
      id: HOME_TOOL.review,
      group: HOME_GROUP.review,
      icon: 'git-pull-request',
      name: t('next.review.tool'),
      hint: t('next.review.toolHint'),
    },
  ];
};

let seq = 0;
const nextId = () => `h${Date.now().toString(36)}${(++seq).toString(36)}`;

// ── Changing the pane's state (pure: the tests walk these) ──

/** The review picker in front: the one there is, or a new one. */
export const withReview = (pane: IHomePane, id: string = nextId()): IHomePane => {
  const have = pane.tabs.find((x) => x.kind === HOME_TAB.review);
  if (have) return { ...pane, open: true, active: have.id };
  return { ...pane, open: true, tabs: [...pane.tabs, { id, kind: HOME_TAB.review }], active: id };
};

/** GitLab or the vault in front: the one there is, or a new one. */
export const withService = (
  pane: IHomePane,
  kind: typeof HOME_TAB.gitlab | typeof HOME_TAB.vault,
  id: string = nextId(),
): IHomePane => {
  const have = pane.tabs.find((x) => x.kind === kind);
  if (have) return { ...pane, open: true, active: have.id };
  return { ...pane, open: true, tabs: [...pane.tabs, { id, kind }], active: id };
};

/** A tool of the machine in front: the tab of it on this machine there is (a browser without a page, the files at
 *  their root), or a new one; a shell is always new (it comes with its terminal: withShell). */
export const withMachineTool = (
  pane: IHomePane,
  machine: MachineId,
  kind: typeof HOME_TAB.browser | typeof HOME_TAB.files | typeof HOME_TAB.machine | typeof HOME_TAB.designly,
  id: string = nextId(),
): IHomePane => {
  const have = pane.tabs.find((x) => {
    if (x.kind !== kind || !('machine' in x) || x.machine !== machine) return false;
    if (x.kind === HOME_TAB.browser) return x.hi < 0;
    if (x.kind === HOME_TAB.files) return x.path === '' && !x.file;
    return true;
  });
  if (have) return { ...pane, open: true, active: have.id };
  const tab = ((): TMachineTab => {
    switch (kind) {
      case HOME_TAB.browser:
        return { id, kind, machine, hist: [], hi: -1 };
      case HOME_TAB.files:
        return { id, kind, machine, path: '', file: false };
      case HOME_TAB.designly:
        return { id, kind, machine, design: null };
      case HOME_TAB.machine:
        return { id, kind, machine };
    }
  })();
  return { ...pane, open: true, tabs: [...pane.tabs, tab], active: id };
};

/** A page opened from the tools' field: the browser tab on that address there is, or a new one. */
export const withAddress = (
  pane: IHomePane,
  machine: MachineId,
  address: IAddress,
  id: string = nextId(),
): IHomePane => {
  const have = pane.tabs.find(
    (x) => x.kind === HOME_TAB.browser && x.machine === machine && browserAt(x, address.text),
  );
  if (have) return { ...pane, open: true, active: have.id };
  const tab: TMachineTab = { id, kind: HOME_TAB.browser, machine, hist: [address], hi: 0 };
  return { ...pane, open: true, tabs: [...pane.tabs, tab], active: id };
};

/** A shell the machine just made, in front. */
export const withShell = (pane: IHomePane, machine: MachineId, terminal: string, id: string = nextId()) => ({
  ...pane,
  open: true,
  tabs: [...pane.tabs, { id, kind: HOME_TAB.shell, machine, terminal } satisfies TMachineTab],
  active: id,
});

/** The tab, changed in place (a browser went to another page, the files to another folder). */
export const withTab = (pane: IHomePane, tab: THomeTab): IHomePane => ({
  ...pane,
  tabs: pane.tabs.map((x) => (x.id === tab.id ? tab : x)),
});

/** The machine the tools open on: the person's pick while it is one of theirs, else the start page's own. */
export const paneMachine = (
  picked: string | null,
  choices: readonly IMachineChoice[],
  fallback: MachineId | null,
): MachineId | null => (picked && choices.some((m) => m.id === picked) ? picked : fallback);

/** A change in front: the tab already showing it, or a new one. */
export const withChange = (pane: IHomePane, picked: IPicked, id: string = nextId()): IHomePane => {
  const have = pane.tabs.find((x) => x.kind === HOME_TAB.change && x.url === picked.link.url);
  if (have) return { ...pane, open: true, active: have.id };
  const tab: THomeTab = {
    id,
    kind: HOME_TAB.change,
    url: picked.link.url,
    title: picked.title,
    name: picked.name,
    source: picked.source,
    target: picked.target,
  };
  return { ...pane, open: true, tabs: [...pane.tabs, tab], active: id };
};

/** Without the tab; its neighbour comes forward, the tools when none is left. */
export const withoutTab = (pane: IHomePane, id: string): IHomePane => {
  const at = pane.tabs.findIndex((x) => x.id === id);
  if (at < 0) return pane;
  const tabs = pane.tabs.filter((x) => x.id !== id);
  const active = pane.active === id ? (tabs[Math.max(0, at - 1)]?.id ?? '') : pane.active;
  return { ...pane, tabs, active };
};

/** What the agent is told about a change tab. */
export const contextOf = (tab: Extract<THomeTab, { kind: 'change' }>): IReviewContext | null => {
  const link = changeLinkOf(tab.url);
  return link ? { link, title: tab.name, source: tab.source, target: tab.target } : null;
};

const tabLook = (tab: THomeTab) => {
  switch (tab.kind) {
    case HOME_TAB.review:
      return { id: tab.id, icon: 'git-pull-request', name: t('next.review.tab') };
    case HOME_TAB.change:
      return {
        id: tab.id,
        icon: tab.url.includes('/-/commit/') ? 'git-compare' : 'git-pull-request',
        name: tab.title,
      };
    case HOME_TAB.gitlab:
      return { id: tab.id, icon: 'git-merge', name: t('next.review.gitlab') };
    case HOME_TAB.vault:
      return { id: tab.id, icon: 'key-round', name: t('next.review.vault') };
    case HOME_TAB.shell:
      return { id: tab.id, icon: 'terminal', name: t('next.pane.shell') };
    case HOME_TAB.browser:
      return {
        id: tab.id,
        icon: 'globe',
        name: tab.hist[tab.hi]?.text ?? t('next.pane.browser'),
      };
    case HOME_TAB.files:
      return {
        id: tab.id,
        icon: tab.file ? 'file-code' : 'files',
        name: tab.path.split('/').filter(Boolean).at(-1) ?? t('next.pane.files'),
      };
    case HOME_TAB.machine:
      return { id: tab.id, icon: 'cpu', name: t('next.pane.machine') };
    case HOME_TAB.designly:
      return { id: tab.id, icon: 'palette', name: t('next.pane.designly') };
  }
};

/** «Спросить агента»: a new chat whose first message names the change, opened as soon as the machine makes it. */
const useAskAgent = (machine: MachineId | null, conversations: readonly Conversation[] | null) => {
  const { choice } = useStartChoice(machine);
  const defaultCwd = useDefaultCwd(machine);
  const [busy, setBusy] = useState(false);
  const ask = (ctx: IReviewContext) => {
    if (!machine || busy) return;
    const cwd = startFolder(conversations, machine, defaultCwd ?? null);
    if (!cwd) {
      toast(t('next.review.askNoFolder'), { tone: 'error' });
      return;
    }
    setBusy(true);
    const created = createConversation(machine, {
      where: { cwd },
      provider: choice?.provider ?? 'claude',
      ...(choice?.profile ? { profile: choice.profile } : {}),
      model: choice?.model ? launchModel({ ...choice, model: choice.model }) : null,
      first: { text: askMessage(ctx, t('next.review.askDefault')), refs: [askRef(ctx)] },
    });
    void created.done.then((st) => {
      if (st.k !== 'failed') return;
      setBusy(false);
      toast(t('next.newChat.failed'), { tone: 'error', description: failureText(st.failure) });
    });
    void created.conversation.then((id: ConvId | null) => {
      setBusy(false);
      if (id) goTo({ kind: ROUTE.chat, id });
    });
  };
  return { ask, busy, can: machine !== null };
};

export interface IHomePaneOptions {
  /** The machine the lists and a new chat go to; null while there is none. */
  machine: MachineId | null;
  /** The person's machines: with more than one, the tools' machine is picked at its label. */
  machines: readonly IMachineChoice[];
  conversations: readonly Conversation[] | null;
  /** The start page is on screen: the pane, its button and its key are there only then. */
  enabled: boolean;
}

/** The files of the machine's own place: its unsaved text asks to be saved before the tab goes or moves away. */
const useHomeFiles = () => {
  const edits = useFileEdits();
  const guard = (ids: readonly string[], run: () => void) => {
    if (ids.some((id) => edits.unsaved.has(id))) toast(t('next.review.unsaved'), { tone: 'warning' });
    else run();
  };
  return { edits, guard };
};

export const useHomePane = ({
  machine: startMachine,
  machines,
  conversations,
  enabled,
}: IHomePaneOptions) => {
  const state = useHomePaneState();
  const open = enabled && state.open;
  const asker = useAskAgent(startMachine, conversations);
  const { edits, guard } = useHomeFiles();
  const [opening, setOpening] = useState(false);
  // Read so the tiles follow a machine's hello (its features come with it).
  useMachines();
  const machine = paneMachine(state.machine, machines, startMachine);
  const nameOf = (id: string) => machines.find((m) => m.id === id)?.name ?? id;
  const tab = state.tabs.find((x) => x.id === state.active);
  // The place of the tab on screen: the machine's own place, its projects' folder as the machine names it.
  const placeMachine = tab && 'machine' in tab ? tab.machine : machine;
  const defaultCwd = useDefaultCwd(placeMachine);
  const place: IPlace | null = placeMachine
    ? { id: HOME_CONV, machineId: placeMachine, cwd: defaultCwd ?? '' }
    : null;

  const setOpen = (next: boolean) => updateHomePane((s) => (s.open === next ? s : { ...s, open: next }));
  useEffect(() => {
    if (!enabled) return;
    setPaneOpen(open);
    return () => setPaneOpen(false);
  }, [enabled, open]);

  useKeys(KEY_SCOPE.global, { 'global.pane': () => setOpen(!state.open) }, enabled);
  const onEscape = useEffectEvent(() => setOpen(false));
  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !event.defaultPrevented) onEscape();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open]);

  const groups: IToolGroup[] = [
    {
      id: HOME_GROUP.vm,
      label: t('next.pane.machineGroup', { name: machine ? nameOf(machine) : '' }),
      aside:
        machine && machines.length > 1 ? (
          <MachinePick
            current={machine}
            choices={machines}
            onPick={(id) => updateHomePane((s) => (s.machine === id ? s : { ...s, machine: id }))}
          />
        ) : null,
    },
    { id: HOME_GROUP.links, label: t('next.pane.linksGroup') },
    { id: HOME_GROUP.review, label: t('next.review.group') },
  ];
  const tools = homeTools({
    machineName: machine ? nameOf(machine) : null,
    shells: machine !== null && hasFeature(machine, 'terminal'),
    machineTool: machine !== null && hasFeature(machine, 'machine'),
  });

  const openShell = (on: MachineId) => {
    if (opening) return;
    setOpening(true);
    void openTerminal(on, HOME_CONV).then((res) => {
      setOpening(false);
      if ('ok' in res) {
        updateHomePane((s) => withShell(s, on, res.value.id));
        return;
      }
      const limit = res.r === 'refused' && res.code === 'conflict';
      toast(limit ? t('next.pane.shellLimit') : t('next.pane.shellFailed'), {
        tone: 'error',
        description: limit ? undefined : pageText(res),
      });
    });
  };
  const openTool = (id: string) => {
    if (id === HOME_TOOL.review) updateHomePane((s) => withReview(s));
    else if (id === HOME_TOOL.gitlab) updateHomePane((s) => withService(s, HOME_TAB.gitlab));
    else if (id === HOME_TOOL.vault) updateHomePane((s) => withService(s, HOME_TAB.vault));
    else if (!machine) return;
    else if (id === HOME_TOOL.shell) openShell(machine);
    else if (
      id === HOME_TOOL.browser ||
      id === HOME_TOOL.files ||
      id === HOME_TOOL.machine ||
      id === HOME_TOOL.designly
    )
      updateHomePane((s) => withMachineTool(s, machine, id));
  };
  const openPicked = (picked: IPicked) => updateHomePane((s) => withChange(s, picked));
  const removeTab = (id: string) => updateHomePane((s) => withoutTab(s, id));
  const closeTab = (id: string) => {
    const closing = state.tabs.find((x) => x.id === id);
    if (closing?.kind !== HOME_TAB.shell) {
      guard([id], () => {
        edits.discard([id]);
        removeTab(id);
      });
      return;
    }
    // The shell ends on the machine first: a tab that went while its shell stayed would leave it running unseen.
    void closeTerminal(closing.machine, HOME_CONV, closing.terminal).then((res) => {
      if ('ok' in res || res.r === 'refused') {
        removeTab(id);
        return;
      }
      toast(t('next.pane.shellCloseFailed'), { tone: 'error', description: pageText(res) });
    });
  };

  const askCtx = tab?.kind === HOME_TAB.change ? contextOf(tab) : null;
  const saving = tab?.kind === HOME_TAB.files && tab.file && edits.live?.tab === tab.id;
  const actions =
    (askCtx && (
      <Button
        size="sm"
        icon="message-square-plus"
        busy={asker.busy}
        disabled={!asker.can}
        onClick={() => asker.ask(askCtx)}
      >
        {t('next.review.ask')}
      </Button>
    )) ||
    (saving && edits.unsaved.has(tab.id) && (
      <Button variant="primary" size="sm" busy={edits.live?.saving} onClick={() => void edits.save()}>
        {t('next.editor.save')}
      </Button>
    ));

  const onMachineTab = (next: TTab) => {
    if (tab && 'machine' in tab)
      updateHomePane((s) => withTab(s, { ...next, machine: tab.machine } as TMachineTab));
  };

  const header = enabled ? { open, onToggle: () => setOpen(!open) } : null;
  const drawer = enabled && (
    <PaneDrawer
      open={open}
      onClose={() => setOpen(false)}
      wide={state.maxed}
      width={state.width}
      onWidth={(width) => updateHomePane((s) => (s.width === width ? s : { ...s, width }))}
    >
      <ToolPane
        tabs={state.tabs.map(tabLook).map((x) => (edits.unsaved.has(x.id) ? { ...x, dirty: true } : x))}
        tab={tab?.id ?? TOOLS_TAB}
        ownBar={tab?.kind === HOME_TAB.browser}
        maxed={state.maxed}
        onPick={(id) => updateHomePane((s) => ({ ...s, active: id === TOOLS_TAB ? '' : id }))}
        onCloseTab={closeTab}
        onToggleMax={() => updateHomePane((s) => ({ ...s, maxed: !s.maxed }))}
        onClose={() => setOpen(false)}
        actions={actions || undefined}
      >
        {!tab && (
          <ToolsLauncher
            tools={tools}
            groups={groups}
            head={<p className="hx-rp-chatonly">{t('next.review.chatOnlyQuiet')}</p>}
            onOpen={openTool}
            onAddress={(address) => {
              // A merge request or a commit opens as a change; any other address in the machine's browser.
              const link =
                address.target.kind === 'url'
                  ? changeLinkOf(`${address.target.origin}${address.path}`)
                  : null;
              if (link)
                openPicked({ link, title: changeTitle(link), name: null, source: null, target: null });
              else if (machine) updateHomePane((s) => withAddress(s, machine, address));
            }}
          />
        )}
        <Suspense fallback={<Skeleton className="hx-rp__skel" />}>
          {tab?.kind === HOME_TAB.review && machine && (
            <ReviewPicker
              key={tab.id}
              machine={machine}
              onOpen={openPicked}
              onSettings={() => goTo({ kind: ROUTE.settings, section: 'connections' })}
            />
          )}
          {tab?.kind === HOME_TAB.gitlab && machine && (
            <MyChanges
              key={tab.id}
              machine={machine}
              onOpen={openPicked}
              onSettings={() => goTo({ kind: ROUTE.settings, section: 'connections' })}
            />
          )}
          {(tab?.kind === HOME_TAB.review || tab?.kind === HOME_TAB.gitlab) && !machine && (
            <p className="hx-rp__empty">
              <Icon name="cpu" size="sm" /> {t('next.review.noMachine')}
            </p>
          )}
          {tab?.kind === HOME_TAB.change && <ReviewChange key={tab.id} url={tab.url} />}
          {tab?.kind === HOME_TAB.vault && <LegacyVault key={tab.id} />}
        </Suspense>
        {tab?.kind === HOME_TAB.machine && (
          <MachineTab key={tab.id} machineId={tab.machine} machineName={nameOf(tab.machine)} />
        )}
        {tab?.kind === HOME_TAB.designly && (
          <DesignlyLibrary
            key={tab.id}
            machine={tab.machine}
            onOpen={(design) => void openDesignFull(tab.machine, design)}
          />
        )}
        {tab?.kind === HOME_TAB.shell && (
          <ShellView
            key={tab.id}
            url={machineApi(tab.machine).ws(
              `/terminal?conversation=${encodeURIComponent(HOME_CONV)}&terminal=${encodeURIComponent(tab.terminal)}&clientId=${CLIENT_ID}`,
            )}
          />
        )}
        {tab?.kind === HOME_TAB.files && place && (
          <FilesTab
            key={tab.id}
            conv={place}
            tab={tab}
            edits={edits}
            guard={guard}
            onChange={onMachineTab}
            onOpenBeside={(path, kind) => {
              const id = nextId();
              const beside: TMachineTab = {
                id,
                kind: HOME_TAB.files,
                machine: tab.machine,
                path,
                file: kind === 'file',
              };
              updateHomePane((s) => ({ ...s, tabs: [...s.tabs, beside], active: id }));
            }}
            tools={null}
          />
        )}
        {tab?.kind === HOME_TAB.browser && place && (
          <BrowserTab
            key={tab.id}
            conv={place}
            tab={tab}
            onChange={onMachineTab}
            onNewTab={() => updateHomePane((s) => withMachineTool(s, tab.machine, HOME_TAB.browser))}
          />
        )}
      </ToolPane>
    </PaneDrawer>
  );
  return { header, drawer, open };
};
