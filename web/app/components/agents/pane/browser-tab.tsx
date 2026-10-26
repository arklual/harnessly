import { useEffect, useMemo, useRef, useState } from 'react';
import { MoreHorizontal, Smartphone } from 'lucide-react';
import {
  BUTTON_SIZE,
  BUTTON_VARIANT,
  CHIP_TONE,
  LAYER_ALIGN,
  TW_MENU_KIND,
  TwBadge,
  TwButton,
  TwMenuButton,
  TwTooltip,
  type TTwMenuEntry,
} from '@/components/agents/ui';
import { AddressField, BrowserBar } from '@/components/agents/pane/address-bar';
import useIsMobileDevice from '@/hooks/use-is-mobile-device';
import VaultLoginButton from '@/components/agents/vault/vault-login-button';
import { ShareButton } from '@/components/agents/design/library/share-button';
import {
  DevtoolsDrawer,
  DevtoolsToggle,
  devtoolsMenuEntry,
} from '@/components/agents/devtools/devtools-drawer';
import { useDevtoolsDrawer, usePreviewDevtools } from '@/hooks/use-preview-devtools';
import {
  FLOATING_BREAKPOINT,
  Island as DevServers,
  PreviewBody,
  asksForAnotherPage,
  forgetAutoOpen,
  neighboursIn,
  openedNeighbours,
  previewStorage,
  reopenTarget,
  shownGateKey,
  targetGateKey,
  usePreviewOpener,
} from '@/components/agents/inspector/preview-island';
import { useOverloadGate } from '@/components/agents/machine-load';
import useIsMobile from '@/hooks/use-is-mobile';
import useDesignLink, { openFullLabel } from '@/hooks/use-design-link';
import useSendBoardToPhone from '@/hooks/use-send-board-to-phone';
import { ensureLibraryPort, libraryPageLabel, useLibraryPort } from '@/hooks/use-design-library';
import { currentMachineId } from '@/lib/app-path';
import { parseLibraryBoardPath } from '@/lib/hl-design-library/phone';
import { parsePreviewTarget, targetAddress, targetLabel, type TPreviewTarget } from '@/lib/hl-preview-client';
import { extractLocalUrls, mergePortRows } from '@/lib/hl-preview-chat-urls';
import { readUrlHistory, writeUrlHistory } from '@/lib/hl-preview-history';
import { relatedPorts } from '@/lib/hl-preview/ports-shared';
import { readPreview, usePreviewState } from '@/lib/hl-preview-store';
import type { ITab } from '@/types/terminal';
import type { ITimelineEntry } from '@/types/timeline';
import { t } from '@/lib/i18n';
import { shownAddress } from '@/components/agents/pane/shown-address';

const failureMessage = (error: unknown): string =>
  error instanceof Error ? error.message : t('pane.browserTab.openFailed');

interface IBrowserTabProps {
  tab: ITab;
  entries: readonly ITimelineEntry[];
  workPaths: readonly string[];
  cwd: string | null;
  /** Where `openPaneTab('browser', target)` asked to go: a port, `localhost:4200/path` or a site. */
  target: string | null;
  /** The person typed another address: the tab remembers it as its target. */
  onTarget: (address: string) => void;
  /** Counts the requests for `target`: the same address asked for again opens again. */
  request?: number;
}

/**
 * The browser of the right panel, as in Codex: back, forward, reload and the address on top, the page below.
 * It drives the conversation's preview (slots, tickets, overload gate); without a page it lists the dev servers.
 */
const BrowserTab = ({ tab, entries, workPaths, cwd, target, onTarget, request = 0 }: IBrowserTabProps) => {
  const key = tab.id;
  const [{ state, ports, detached }, patch] = usePreviewState(key);
  const { open, openInTab, close, openingTab, tabError } = usePreviewOpener(key);
  const { guard, gate } = useOverloadGate(key);
  const canFloat = !useIsMobile(FLOATING_BREAKPOINT);
  const floating = detached && canFloat;
  const [machine] = useState(() => currentMachineId() ?? '');
  const [history, setHistory] = useState(() => readUrlHistory(previewStorage(), machine));
  const urls = useMemo(() => extractLocalUrls(entries), [entries]);
  const rows = useMemo(() => (ports === null ? null : mergePortRows(ports, urls)), [ports, urls]);

  const neighboursOf = (to: TPreviewTarget) => (to.kind === 'port' ? neighboursIn(rows, to.port) : []);
  const go = async (to: TPreviewTarget) => {
    forgetAutoOpen(key);
    await open(to, neighboursOf(to));
    const { state: after } = readPreview(key);
    if (
      to.kind === 'url' &&
      after.kind === 'open' &&
      after.target.kind === 'url' &&
      after.target.origin === to.origin
    ) {
      setHistory(writeUrlHistory(previewStorage(), machine, to));
    }
  };
  const goTo = (to: TPreviewTarget) =>
    guard(targetGateKey(to), shownGateKey(readPreview(key).state), () => void go(to));
  const parse = (address: string): TPreviewTarget | null => {
    try {
      const parsed = parsePreviewTarget(address);
      if (parsed) return parsed;
      patch({
        state: { kind: 'error', message: t('pane.browserTab.addressHint') },
      });
    } catch (error) {
      patch({ state: { kind: 'error', message: failureMessage(error) } });
    }
    return null;
  };
  const submit = (address: string) => {
    const to = parse(address);
    if (!to) return;
    onTarget(address);
    goTo(to);
  };

  // A target asked for from outside (the summary, an agent command, the design library) opens once per request; the
  // page already there stays as it is unless the request names another page of it.
  const applied = useRef<string | null>(null);
  useEffect(() => {
    const asked = target ? `${target}#${request}` : null;
    if (!target || applied.current === asked) return;
    applied.current = asked;
    let to: TPreviewTarget | null;
    try {
      to = parsePreviewTarget(target);
    } catch {
      return;
    }
    const shown = readPreview(key).state;
    if (to && (targetGateKey(to) !== shownGateKey(shown) || asksForAnotherPage(to, shown))) goTo(to);
  });

  const reload = (to: TPreviewTarget, neighbours: readonly number[]) => {
    forgetAutoOpen(key);
    void open(to, neighbours);
  };

  // A page of the design library: the bar names the board and marks it as Claude Design.
  const libraryPort = useLibraryPort();
  const showsPort = state.kind === 'open' && state.target.kind === 'port';
  const onLibrary =
    state.kind === 'open' && state.target.kind === 'port' && state.target.port === libraryPort;
  // A board of the library opens in a window of its own by its link, not through a one-time slot ticket.
  const { openFull } = useDesignLink();
  const board = onLibrary && state.kind === 'open' ? parseLibraryBoardPath(state.path) : null;
  const shown = state.kind === 'open' || state.kind === 'opening' || state.kind === 'taken';
  const tabEntry = (): TTwMenuEntry[] => {
    if (board)
      return [
        {
          kind: TW_MENU_KIND.action,
          id: 'tab',
          label: openFullLabel(),
          onSelect: () => openFull({ project: board.artifact, board: board.board, full: true }),
        },
      ];
    if (state.kind === 'open')
      return [
        {
          kind: TW_MENU_KIND.action,
          id: 'tab',
          label: t('pane.browserTab.openInBrowser'),
          disabled: openingTab,
          onSelect: () => void openInTab(reopenTarget(state), openedNeighbours(state)),
        },
      ];
    return [];
  };
  const framed = shown && !floating;
  // DevTools of the page in the frame; the floating window has its own while the page is there.
  const [drawer, patchDrawer] = useDevtoolsDrawer(key);
  const openPage = framed && state.kind === 'open' ? state : null;
  const devtools = usePreviewDevtools({
    slot: openPage?.preview.origin ?? null,
    machineId: openPage?.preview.machineId,
    open: drawer.open,
  });
  const toggleDevtools = () => patchDrawer({ open: !drawer.open });
  const menu: TTwMenuEntry[] = [
    ...tabEntry(),
    ...(openPage ? [devtoolsMenuEntry(drawer.open, toggleDevtools)] : []),
    ...(canFloat && shown && !floating
      ? [
          {
            kind: TW_MENU_KIND.action,
            id: 'float',
            label: t('pane.browserTab.detach'),
            onSelect: () => patch({ detached: true }),
          },
        ]
      : []),
    ...(shown
      ? [
          {
            kind: TW_MENU_KIND.action,
            id: 'servers',
            label: t('pane.browserTab.devServers'),
            onSelect: close,
          },
        ]
      : []),
    ...history.map((to) => ({
      kind: TW_MENU_KIND.action,
      id: `url:${targetAddress(to)}`,
      label: targetAddress(to),
      onSelect: () => goTo(to),
    })),
  ];
  const more = menu.length > 0 && (
    <TwMenuButton
      items={menu}
      menuLabel={t('pane.browserTab.more')}
      align={LAYER_ALIGN.end}
      className="tw-icon-button tw-rpane-browser-more"
      aria-label={t('pane.browserTab.more')}
      title={t('pane.browserTab.more')}
    >
      <MoreHorizontal />
    </TwMenuButton>
  );
  const field = (path?: string) => (
    <AddressField
      value={
        shown && path !== undefined && state.kind !== 'opening'
          ? `${targetLabel(state.target)}${path === '/' ? '' : path}`
          : shownAddress(state)
      }
      label={onLibrary && path !== undefined ? (libraryPageLabel(path) ?? undefined) : undefined}
      onSubmit={submit}
    />
  );
  const vault =
    state.kind === 'open' && state.target.kind === 'url' ? (
      <VaultLoginButton previewKey={key} origin={state.target.origin} />
    ) : null;

  useEffect(() => {
    if (showsPort) void ensureLibraryPort();
  }, [showsPort]);
  const design = onLibrary ? (
    <TwBadge tone={CHIP_TONE.accent} className="tw-rpane-design-badge">
      Claude Design
    </TwBadge>
  ) : null;
  // The board on screen can be sent to the phone; any other page has nothing to send.
  const { send: sendToPhone, sending } = useSendBoardToPhone();
  // On the phone itself there is nowhere to send the board.
  const isMobileDevice = useIsMobileDevice();
  const toPhone =
    board && !isMobileDevice ? (
      <TwTooltip content={t('pane.browserTab.sendToPhone')}>
        <TwButton
          type="button"
          size={BUTTON_SIZE.sm}
          variant={BUTTON_VARIANT.ghost}
          pending={sending}
          aria-label={t('pane.browserTab.sendToPhone')}
          onClick={() => void sendToPhone(board)}
        >
          <Smartphone />
        </TwButton>
      </TwTooltip>
    ) : null;
  const share =
    board && state.kind === 'open' ? (
      <ShareButton
        location={{ project: board.artifact, board: board.board, full: false }}
        title={libraryPageLabel(state.path) ?? board.board}
      />
    ) : null;

  return (
    <div className="tw-rpane-browser">
      {(!framed || state.kind !== 'open') && (
        <BrowserBar>
          {field()}
          {more}
        </BrowserBar>
      )}
      {gate}
      {tabError && (
        <p className="tw-preview-error" role="alert">
          {tabError}
        </p>
      )}
      {framed && (
        <div className="tw-rpane-browser-page">
          <PreviewBody
            previewKey={key}
            state={state}
            onReopenTaken={(taken) =>
              reload(
                reopenTarget(taken),
                taken.target.kind === 'port' ? relatedPorts(ports ?? [], taken.target.port) : [],
              )
            }
            onStale={(current) => void open(reopenTarget(current), openedNeighbours(current))}
            onEntered={(current) => patch({ state: { ...current, entered: true } })}
            onReload={(current) => reload(reopenTarget(current), openedNeighbours(current))}
            onOpenUrl={(to) => guard(targetGateKey(to), shownGateKey(state), () => reload(to, []))}
            address={field}
            actions={
              <>
                {design}
                {share}
                {toPhone}
                {vault}
                {openPage && (
                  <DevtoolsToggle devtools={devtools} open={drawer.open} onToggle={toggleDevtools} />
                )}
                {more}
              </>
            }
          />
          {openPage && drawer.open && (
            <DevtoolsDrawer
              devtools={devtools}
              slot={openPage.preview.origin}
              machineId={openPage.preview.machineId}
              tab={drawer.tab}
              onTab={(tab) => patchDrawer({ tab })}
              onClose={() => patchDrawer({ open: false })}
            />
          )}
        </div>
      )}
      <div className="tw-rpane-browser-home" hidden={framed}>
        <DevServers
          tab={tab}
          provider=""
          expanded={false}
          entries={entries}
          workPaths={workPaths}
          cwd={cwd}
          active
          bare
        />
      </div>
    </div>
  );
};

export default BrowserTab;
