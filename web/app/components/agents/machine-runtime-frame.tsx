import { useCallback, useEffect, useRef, useState } from 'react';
import useFleetStatus from '@/hooks/use-fleet-status';
import { pokeAgentList } from '@/hooks/use-hl-conversations';
import {
  isNetworkQuality,
  NETWORK_MESSAGE_TYPE,
  useNetworkHealthStore,
  type TNetworkQuality,
} from '@/lib/network-health';
import { PROVIDER_STATUSES, type TProviderStatus } from '@/components/agents/provider-icon';
import type { Conversation } from '@/lib/hl-conversations';
import {
  PANE_SUMMARY_MESSAGE,
  frameViewParams,
  isPaneWidthMessage,
  isShortcutMessage,
  isSwipeMessage,
  type IShellShortcut,
  type TShellShortcutId,
  type TSwipeDirection,
} from '@/lib/hl-frame-bridge';
import { OPEN_PANE_TAB_EVENT, isPaneTabRequest } from '@/lib/hl-pane-tabs';
import type { IFrameSplit } from '@/lib/hl-frame-bridge';
import { markOpenPhase } from '@/lib/hl-speed-client';
import { t } from '@/lib/i18n';

interface IMachineRuntimeFrameProps {
  machineId: string;
  conversation: Conversation;
  visible: boolean;
  inspectorOpen: boolean;
  navigatorOpen: boolean;
  onNavigatorChange: (open: boolean) => void;
  onInspectorChange: (open: boolean) => void;
  /** The frame's right panel was dragged to this width: the shell keeps its column for it. */
  onPaneWidth: (width: number) => void;
  /** Shortcuts the shell handles; the frame forwards exactly these while it has focus. */
  shellShortcuts: readonly TShellShortcutId[];
  onShortcut: (shortcut: IShellShortcut) => void;
  onSwipe: (direction: TSwipeDirection) => void;
  /** Wide screen with the right panel open: where the conversation starts under the shell header and how wide the panel is. */
  split: IFrameSplit | null;
  /** The frame's page came up and reported its agents: from then on it rides out an outage of its machine. */
  onReady?: (machineId: string) => void;
}

/** A visited machine's frame keeps its terminals and in-progress replies while it stays mounted (see `retainMachineFrames`). */
const MachineRuntimeFrame = ({
  machineId,
  conversation,
  visible,
  inspectorOpen,
  navigatorOpen,
  onNavigatorChange,
  onInspectorChange,
  onPaneWidth,
  shellShortcuts,
  onShortcut,
  onSwipe,
  split,
  onReady,
}: IMachineRuntimeFrameProps) => {
  const frame = useRef<HTMLIFrameElement>(null);
  const lastStatuses = useRef<string | null>(null);
  const [ready, setReady] = useState(0);
  const [networkQuality, setNetworkQuality] = useState<TNetworkQuality>('ok');
  // The address is read once: a later view goes by message, a changed `src` would reload the frame.
  const [src] = useState(
    () =>
      `/agents/workspace?runtime=1&machine=${encodeURIComponent(machineId)}${frameViewParams({ inspectorOpen, split: visible ? split : null })}`,
  );
  const sendSelection = useCallback(() => {
    frame.current?.contentWindow?.postMessage(
      {
        type: 'hl:open-agent',
        workspaceId: conversation.workspaceId,
        tabId: conversation.id,
        provider: conversation.provider,
      },
      location.origin,
    );
  }, [conversation]);

  useEffect(() => {
    const handle = (event: MessageEvent) => {
      if (event.origin !== location.origin || event.source !== frame.current?.contentWindow) return;
      if (event.data?.type === 'hl:agents-ready') {
        if (visible) markOpenPhase('tab');
        setReady((value) => value + 1);
        onReady?.(machineId);
      }
      if (event.data?.type === NETWORK_MESSAGE_TYPE && isNetworkQuality(event.data.quality))
        setNetworkQuality(event.data.quality);
      if (
        event.data?.type === 'hl:agent-status' &&
        event.data.statuses &&
        typeof event.data.statuses === 'object'
      ) {
        const statuses = Object.fromEntries(
          Object.entries(event.data.statuses).filter(
            ([, status]) =>
              typeof status === 'string' && (PROVIDER_STATUSES as readonly string[]).includes(status),
          ),
        ) as Record<string, TProviderStatus>;
        const text = JSON.stringify(statuses);
        // The first report only describes the frame's start; later changes mean new prompts, titles or tabs there.
        if (lastStatuses.current !== null && lastStatuses.current !== text) pokeAgentList();
        lastStatuses.current = text;
        useFleetStatus.getState().setMachine(machineId, statuses);
      }
      if (visible && event.data?.type === 'hl:navigator-state' && typeof event.data.open === 'boolean')
        onNavigatorChange(event.data.open);
      if (visible && event.data?.type === 'hl:inspector-state' && typeof event.data.open === 'boolean')
        onInspectorChange(event.data.open);
      if (visible && isPaneWidthMessage(event.data)) onPaneWidth(event.data.width);
      if (visible && isShortcutMessage(event.data))
        onShortcut({ key: event.data.key, shift: event.data.shift });
      if (visible && isSwipeMessage(event.data)) onSwipe(event.data.direction);
    };
    window.addEventListener('message', handle);
    return () => window.removeEventListener('message', handle);
  }, [machineId, visible, onNavigatorChange, onInspectorChange, onPaneWidth, onShortcut, onSwipe, onReady]);

  // The conversation on screen keeps its right panel inside this frame, so tab and summary requests from the shell go there.
  useEffect(() => {
    if (!visible || !ready) return;
    const post = (message: object) => frame.current?.contentWindow?.postMessage(message, location.origin);
    const forwardTab = (event: Event) => {
      const request = (event as CustomEvent<unknown>).detail;
      if (isPaneTabRequest(request)) post({ ...request, type: OPEN_PANE_TAB_EVENT });
    };
    const forwardSummary = () => post({ type: PANE_SUMMARY_MESSAGE });
    window.addEventListener(OPEN_PANE_TAB_EVENT, forwardTab);
    window.addEventListener(PANE_SUMMARY_MESSAGE, forwardSummary);
    return () => {
      window.removeEventListener(OPEN_PANE_TAB_EVENT, forwardTab);
      window.removeEventListener(PANE_SUMMARY_MESSAGE, forwardSummary);
    };
  }, [visible, ready]);

  // Only the frame on screen speaks for the link: a hidden machine reconnecting is not what the user is looking at.
  useEffect(() => {
    const key = `machine:${machineId}`;
    useNetworkHealthStore.getState().setRemote(key, visible ? networkQuality : 'ok');
    return () => useNetworkHealthStore.getState().setRemote(key, 'ok');
  }, [machineId, visible, networkQuality]);

  useEffect(() => {
    if (ready) sendSelection();
  }, [ready, sendSelection]);

  useEffect(() => {
    if (!ready) return;
    frame.current?.contentWindow?.postMessage(
      {
        type: 'hl:runtime-view',
        visible,
        inspectorOpen,
        navigatorOpen: visible && navigatorOpen,
        shortcuts: shellShortcuts,
        split: visible ? split : null,
      },
      location.origin,
    );
  }, [ready, inspectorOpen, navigatorOpen, visible, shellShortcuts, split]);

  return (
    <iframe
      ref={frame}
      title={t('agents.machineRuntimeFrame.title', { machine: conversation.machineName || machineId })}
      src={src}
      className={`tw-pane tw-frame${visible ? '' : ' tw-pane-off'}`}
      aria-hidden={!visible}
      allow="clipboard-read; clipboard-write"
    />
  );
};

export default MachineRuntimeFrame;
