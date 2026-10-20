import { appConfig } from '@/platform/config';
import { useEffect, useRef } from 'react';
import { useRouter } from 'next/router';
import { toast } from 'sonner';
import { X } from 'lucide-react';
import ProviderIcon from '@/components/agents/provider-icon';
import useTabStore from '@/hooks/use-tab-store';
import useFleetStatus from '@/hooks/use-fleet-status';
import { PUSH_MESSAGE_EVENT } from '@/hooks/use-web-push';
import { cleanConversationTitle, type Conversation } from '@/lib/hl-conversations';
import { OPEN_BOARD_EVENT, parseBoardOpenUrl, type IBoardOpenRequest } from '@/lib/hl-design-library/phone';
import { designLocationOfUrl } from '@/lib/hl-design-link';
import { openDesignLocation } from '@/lib/hl-open-design';
import { PUSH_KIND_LABELS, clip, type IPushMessage, type TPushKind } from '@/lib/push-message';
import { APP_BASE_PATH, stripAppPath } from '@/lib/app-path';
import type { TProviderStatus } from '@/components/agents/provider-icon';
import type { ITabState } from '@/hooks/use-tab-store';
import { t } from '@/lib/i18n';

/**
 * A doc-thread push carries `url` instead of a tab to open; only trust one that is our own
 * `/agents/docs/...` path (an attacker who reaches the push channel shouldn't get an open
 * redirect out of a click), and hand `router.push` the basePath-free form it expects.
 */
const trustedDocPath = (url: string | undefined): string | null => {
  if (!url || !url.startsWith(`${APP_BASE_PATH}/docs/`)) return null;
  return stripAppPath(url);
};

type TNotifyKind = Exclude<TPushKind, 'test'>;

interface INotice {
  kind: TPushKind;
  tabId: string;
  machineId?: string;
  provider?: string;
  title?: string;
  detail?: string;
  url?: string;
}

interface IAgentNotifierProps {
  conversations: Conversation[];
  selected: Conversation | null;
  defaultMachineId?: string;
  onOpen: (item: Conversation) => void;
}

/** One transition reaches the page three ways (push, local status socket, a machine's frame); show it once. */
const DEDUPE_MS = 20_000;
const DURATION: Record<TPushKind, number> = {
  done: 8_000,
  question: 20_000,
  test: 6_000,
  doc: 10_000,
  design: 15_000,
  release: 10_000,
};

const OPEN_LABEL: Record<TPushKind, string> = {
  done: t('agents.agentNotifier.openConversation'),
  question: t('agents.agentNotifier.reply'),
  test: t('agents.agentNotifier.openConversation'),
  doc: t('agents.agentNotifier.openConversation'),
  design: t('agents.agentNotifier.openDesign'),
  release: t('agents.agentNotifier.refresh'),
};

const kindOf = (status: TProviderStatus | undefined): TNotifyKind | null =>
  status === 'ready' || status === 'failed' ? 'done' : status === 'needs-input' ? 'question' : null;

const cliKind = (tab: ITabState): TNotifyKind | null =>
  tab.cliState === 'ready-for-review' ? 'done' : tab.cliState === 'needs-input' ? 'question' : null;

const localDetail = (tab: ITabState, kind: TNotifyKind): string | undefined => {
  if (kind === 'done') return tab.lastAssistantMessage ?? undefined;
  const request = tab.permissionRequest;
  if (request?.type === 'ExecApprovalRequest')
    return t('agents.agentNotifier.allowCommand', { command: request.command });
  return tab.currentAction?.summary
    ? t('agents.agentNotifier.allow', { action: tab.currentAction.summary })
    : undefined;
};

interface IAgentToastProps {
  kind: TPushKind;
  provider: string;
  title: string;
  detail?: string;
  machine?: string;
  onOpen?: () => void;
  onClose: () => void;
}

const openBoard = (request: IBoardOpenRequest) =>
  window.dispatchEvent(new CustomEvent<IBoardOpenRequest>(OPEN_BOARD_EVENT, { detail: request }));

const AgentToast = ({ kind, provider, title, detail, machine, onOpen, onClose }: IAgentToastProps) => (
  <div className="tw-toast" data-kind={kind} role={kind === 'question' ? 'alert' : 'status'}>
    <ProviderIcon provider={provider} status={kind === 'question' ? 'needs-input' : 'ready'} />
    <div className="tw-toast-body">
      <div className="tw-toast-head">
        <span className="tw-toast-kind">{PUSH_KIND_LABELS[kind]}</span>
        {machine && <span className="tw-toast-machine">{machine}</span>}
      </div>
      <p className="tw-toast-title">{title}</p>
      {detail && <p className="tw-toast-detail">{detail}</p>}
      {onOpen && (
        <button type="button" className="tw-toast-open" onClick={onOpen}>
          {OPEN_LABEL[kind]}
        </button>
      )}
    </div>
    <button
      type="button"
      className="tw-toast-close"
      aria-label={t('agents.agentNotifier.close')}
      onClick={onClose}
    >
      <X />
    </button>
  </div>
);

/**
 * In-app notifications of the workspace: an agent finished or is waiting for an answer.
 * The conversation on screen never notifies about itself.
 */
const AgentNotifier = ({ conversations, selected, defaultMachineId, onOpen }: IAgentNotifierProps) => {
  const router = useRouter();
  const context = useRef({ conversations, selected, defaultMachineId, onOpen, router });
  useEffect(() => {
    context.current = { conversations, selected, defaultMachineId, onOpen, router };
  }, [conversations, selected, defaultMachineId, onOpen, router]);

  useEffect(() => {
    const shown = new Map<string, number>();

    const show = (notice: INotice) => {
      if (document.visibilityState !== 'visible') return;
      const {
        conversations: list,
        selected: current,
        defaultMachineId: native,
        onOpen: open,
        router: nav,
      } = context.current;
      if (notice.kind !== 'test' && current?.kind === 'agent' && current.id === notice.tabId) return;
      const key = `${notice.tabId}:${notice.kind}${notice.url ? `:${notice.url}` : ''}`;
      const now = Date.now();
      if (notice.kind !== 'test' && now - (shown.get(key) ?? 0) < DEDUPE_MS) return;
      shown.set(key, now);

      const item =
        notice.kind === 'doc' || notice.kind === 'design'
          ? undefined
          : (list.find(
              (c) =>
                c.kind === 'agent' &&
                c.id === notice.tabId &&
                (!notice.machineId || (c.machineId ?? native) === notice.machineId),
            ) ?? list.find((c) => c.kind === 'agent' && c.id === notice.tabId));
      if (notice.kind !== 'test' && notice.kind !== 'doc' && notice.kind !== 'design' && !item) return;
      const machines = new Set(list.filter((c) => c.kind === 'agent').map((c) => c.machineId ?? native));
      const machine = machines.size > 1 ? item?.machineName : undefined;
      const title = notice.title || (item ? cleanConversationTitle(item.title) : appConfig().productName);
      const docPath = notice.kind === 'doc' ? trustedDocPath(notice.url) : null;
      const board = notice.kind === 'design' ? parseBoardOpenUrl(notice.url) : null;
      const place = notice.kind === 'design' ? designLocationOfUrl(notice.url) : null;

      const actionOf = (): (() => void) | undefined => {
        if (item) return () => open(item);
        if (docPath)
          return () => {
            void nav.push(docPath);
          };
        if (board) return () => openBoard(board);
        if (place) return () => openDesignLocation(place);
        return undefined;
      };
      const action = actionOf();

      toast.custom(
        (id) => (
          <AgentToast
            kind={notice.kind}
            provider={item?.provider ?? notice.provider ?? 'claude'}
            title={clip(title, 90)}
            detail={notice.detail ? clip(notice.detail, 160) : undefined}
            machine={machine}
            onOpen={
              action
                ? () => {
                    toast.dismiss(id);
                    action();
                  }
                : undefined
            }
            onClose={() => toast.dismiss(id)}
          />
        ),
        { id: `agent:${key}`, duration: DURATION[notice.kind], unstyled: true, className: 'tw-toast-host' },
      );
    };

    const fromPush = (event: Event) => {
      const message = (event as CustomEvent<IPushMessage>).detail;
      // The update banner answers a release push on an open page.
      if (message.kind === 'release') return;
      show({
        kind: message.kind,
        tabId: message.tabId,
        provider: message.provider,
        title: message.kind === 'test' ? message.title : message.conversationTitle,
        detail: message.detail,
        url: message.url,
      });
    };
    window.addEventListener(PUSH_MESSAGE_EVENT, fromPush);

    const unsubTabs = useTabStore.subscribe((state, prev) => {
      for (const [tabId, tab] of Object.entries(state.tabs)) {
        const before = prev.tabs[tabId];
        if (!before || before.cliState === tab.cliState) continue;
        const kind = cliKind(tab);
        if (kind)
          show({ kind, tabId, machineId: context.current.defaultMachineId, detail: localDetail(tab, kind) });
      }
    });

    const unsubFleet = useFleetStatus.subscribe((state, prev) => {
      for (const [machineId, statuses] of Object.entries(state.machines)) {
        const before = prev.machines[machineId];
        if (!before) continue;
        for (const [tabId, status] of Object.entries(statuses)) {
          if (before[tabId] === status) continue;
          const kind = kindOf(status);
          if (kind) show({ kind, tabId, machineId });
        }
      }
    });

    return () => {
      window.removeEventListener(PUSH_MESSAGE_EVENT, fromPush);
      unsubTabs();
      unsubFleet();
    };
  }, []);

  useEffect(() => {
    if (selected?.kind !== 'agent') return;
    toast.dismiss(`agent:${selected.id}:done`);
    toast.dismiss(`agent:${selected.id}:question`);
  }, [selected]);

  return null;
};

export default AgentNotifier;
