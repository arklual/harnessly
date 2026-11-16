// The chat's right column: Claude's recap of the session, the pictures the agent looked at (ConvSnapshot.shots) and its
// background tasks (ConvSnapshot.background and its events, and the conversation's recap). It is open by itself while there is something in it, there is room for it beside
// the chat's column (the left sidebar takes its width off the stage, so it counts too) and the right pane is shut.
// The pane opening always takes it away, even when the person had opened it; the pane shutting gives back what the room
// says. The header's button shows it or folds it, and a fold stays until the chat is changed. A stop goes to the
// runtime as bg.stop and waits for it.
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useCodeAsks } from '@/next/bindings/code-asks';
import { settled } from '@/next/bindings/outcome';
import { stopBackground } from '@/next/commands';
import BackgroundPanel from '@/next/features/background/background-panel';
import { cancelCode, isSafeRefusal, putCode, type ICodeAsk } from '@/next/features/safe/api';
import CodeAsks, { CODE_EXIT_MS, CODE_SENT, type TCodeSent } from '@/next/features/safe/code-asks';
import { useRailRoom } from '@/next/features/background/rail-room';
import type { IHeaderBackground } from '@/next/features/header/chat-header';
import { machineApi } from '@/next/core/net/machine';
import type { BackgroundTask, ConvId, Conversation, MachineId, Shot } from '@/next/protocol/v2';
import { useBackground, useShots } from '@/next/stores/conversation';
import { useConversation } from '@/next/stores/conversations';
import { usePresentFlag } from '@/next/ui/presence';
import { LEAVE_MS } from '@/next/features/notifications/motion';
import { toast } from '@/next/ui/toast';
import { usePhone } from '@/next/ui/use-media';
import NoticeStack from '@/next/features/smart-notice/stack';
import { SMART_KIND } from '@/next/features/smart-notice/smart-notice';
import { NOTIFICATION_KIND } from '@/next/features/notifications/kinds';
import MachineDrop from '@/next/bindings/machine-drop';
import SideWidgetsBinding from '@/next/bindings/side-widgets';
import { WaitBinding } from '@/next/bindings/notification-center';
import { askDropBack, dismissDrops, useDrops, type IDrop } from '@/next/stores/smart-drops';
import { useConversationList } from '@/next/stores/conversations';
import { t } from '@/next/i18n';

/** What is answered right in its card: a question's options, a permission's buttons (the rest opens its chat or the
 *  smart notice). */
/** The column's own exit (styles/background.css, .hx-side). */
const SIDE_EXIT_MS = 200;

const IN_PLACE: ReadonlySet<string> = new Set([SMART_KIND.question, SMART_KIND.permission]);

export const useChatBackground = (
  conv: ConvId | null,
  machine: MachineId | null,
  now: number,
  onOpenChat: (id: string) => void,
): { header: IHeaderBackground | null; panel: React.ReactNode; show: () => void } => {
  const tasks = useBackground(conv);
  const recap = useConversation(conv)?.recap ?? null;
  const shots = useShots(conv);
  // What the person asked of the column in this chat: null leaves it to the room there is.
  const [asked, setAsked] = useState<{ conv: ConvId | null; shown: boolean } | null>(null);
  const [rail, setRail] = useState<HTMLElement | null>(null);
  const [stopping, setStopping] = useState<ReadonlySet<string>>(() => new Set());
  const { room, pane } = useRailRoom(rail);
  const codeAsks = useCodeAsks(machine);
  // Only this chat's commands: a code is typed where its agent works.
  const codes = conv ? codeAsks.asks.filter((a) => a.conv === conv) : [];
  // The column stays while the last card folds away, then leaves with it.
  const codesMounted = usePresentFlag(codes.length > 0, CODE_EXIT_MS * 2).mounted;
  // What waits and was put away from the smart notice stands here as a stack, whichever chat it is of; the machine's
  // card stands apart, under the column, not among the chats.
  const all = useDrops();
  const drops = useMemo(() => all.filter((d) => d.item.kind !== SMART_KIND.machine), [all]);
  const machineDrops = useMemo(() => all.filter((d) => d.item.kind === SMART_KIND.machine), [all]);
  // The last card answered elsewhere flies off before the stack (and the column it keeps) goes.
  const dropsMounted = usePresentFlag(drops.length > 0, LEAVE_MS).mounted;
  const machineMounted = usePresentFlag(machineDrops.length > 0, LEAVE_MS).mounted;
  const convs = useConversationList(null);
  const byId = useMemo(() => new Map<string, Conversation>((convs ?? []).map((c) => [c.id, c])), [convs]);
  const renderAnswer = useCallback(
    (drop: IDrop, open: boolean, toggle: () => void) => {
      const { item } = drop;
      const chat = item.chatId ? byId.get(item.chatId) : undefined;
      if (!chat || !IN_PLACE.has(item.kind)) return null;
      return (
        <WaitBinding
          conv={chat}
          item={{
            id: item.key,
            kind: NOTIFICATION_KIND.waiting,
            icon: item.icon,
            title: item.title,
            text: item.text,
            at: item.at,
            unread: true,
            chatId: item.chatId,
          }}
          open={open}
          onToggle={toggle}
          onOpenChat={() => onOpenChat(chat.id)}
        />
      );
    },
    [byId, onOpenChat],
  );
  // The machine's card says what is wrong right where it stands: no flight into the smart notice for the details.
  const renderMachine = useCallback(
    (drop: IDrop) => (drop.item.machine ? <MachineDrop machine={drop.item.machine} /> : null),
    [],
  );
  const phone = usePhone();
  const has = tasks.length > 0 || recap !== null || (shots?.length ?? 0) > 0 || codesMounted || dropsMounted;
  // The pane opening takes back a column the person had opened (a fold they made stays); this runs in render, once per change.
  const [paneWas, setPaneWas] = useState(pane);
  if (pane !== paneWas) {
    setPaneWas(pane);
    if (pane && asked?.shown) setAsked(null);
  }
  const override = asked && asked.conv === conv ? asked.shown : null;
  // On its own the column is open when it has room and the pane is shut, and whenever a command waits for a code.
  // A drop opens it too, but not on a phone, where the column is a sheet over the chat: there the header's button says it.
  const auto = (room && !pane) || codes.length > 0 || (drops.length > 0 && !phone);
  const shown = has && (override ?? auto);
  // A chat with nothing for the column takes it away with its slide and fade (data-shown), not from one frame to the next.
  const hasMounted = usePresentFlag(has, SIDE_EXIT_MS).mounted;
  // The person's widgets stand under the column on a computer, where it has room beside the chat and the pane
  // is shut, or where the person opened them by the header's button; on a phone there is no room beside the chat (the limits are on the model's pill there). Away, they are not
  // drawn at all: no frame, no ask.
  const widgetsWanted = !phone && conv !== null && machine !== null;
  const widgetsShown = widgetsWanted && !pane && (override ?? room);
  // A chat with nothing for the column but the widgets: the header's button folds and unfolds them.
  const open = has ? shown : widgetsShown;
  const widgetsMounted = usePresentFlag(widgetsShown, SIDE_EXIT_MS).mounted;
  // Where the column has no room beside the chat, the widgets the button opened are a popover at the button over the
  // feed's edge, not a column: nothing of the feed moves, and a click away or Esc shuts them.
  const widgetsPop = widgetsShown && !room;
  useEffect(() => {
    if (!widgetsPop) return;
    const shut = () => setAsked(null);
    const away = (event: PointerEvent) => {
      const el = event.target instanceof Element ? event.target : null;
      // The cards' menus and the gallery stand in portals: a click there is a click in the popover.
      if (el?.closest('.hx-wgs-wrap, .hx-bg-btn, [role="menu"], [role="dialog"]')) return;
      shut();
    };
    const esc = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !event.defaultPrevented) shut();
    };
    document.addEventListener('pointerdown', away, true);
    document.addEventListener('keydown', esc);
    return () => {
      document.removeEventListener('pointerdown', away, true);
      document.removeEventListener('keydown', esc);
    };
  }, [widgetsPop]);

  // What the button asks for: what the room already says is no ask at all, so the column keeps following the room.
  const toggle = () => {
    const next = !open;
    setAsked(next === (has ? auto : room && !pane) ? null : { conv, shown: next });
  };

  const onStop = (task: BackgroundTask) => {
    if (!conv || !machine) return;
    setStopping((s) => new Set(s).add(task.id));
    void settled(stopBackground(machine, conv, task.id))
      .catch((error: Error) => toast(t('next.bg.stopFailed'), { tone: 'error', description: error.message }))
      .finally(() =>
        setStopping((s) => {
          const next = new Set(s);
          next.delete(task.id);
          return next;
        }),
      );
  };

  // One function per chat: the column keeps the last chat's pictures with their own addresses while they fade out.
  const shotSrc = useCallback(
    (shot: Shot): string =>
      conv && machine
        ? machineApi(machine).http(`/conversations/${conv}/shot?id=${encodeURIComponent(shot.id)}`)
        : '',
    [conv, machine],
  );

  const header: IHeaderBackground | null =
    conv && (has || widgetsWanted)
      ? {
          running: tasks.filter((x) => x.status === 'running').length,
          total: tasks.length,
          recap: recap !== null,
          waits: drops.length > 0,
          widgets: !has,
          open,
          onToggle: toggle,
        }
      : null;

  const putOne = async (ask: ICodeAsk, code: string): Promise<TCodeSent> => {
    if (!machine) return CODE_SENT.failed;
    const res = await putCode(machine, ask.id, code);
    if (!('ok' in res)) return CODE_SENT.failed;
    if (!isSafeRefusal(res.value)) return CODE_SENT.ok;
    if (res.value.refused === 'not-found') return CODE_SENT.gone;
    return res.value.refused === 'bad-code' ? CODE_SENT.bad : CODE_SENT.failed;
  };

  const panel =
    hasMounted || machineMounted || widgetsWanted ? (
      <BackgroundPanel
        ref={setRail}
        chat={conv}
        tasks={tasks}
        recap={recap}
        shots={shots}
        shotSrc={shotSrc}
        shown={shown}
        now={now}
        stopping={stopping}
        onStop={onStop}
        onHide={shown ? toggle : undefined}
        below={
          machineMounted || widgetsMounted ? (
            <>
              {machineMounted && (
                <NoticeStack
                  drops={machineDrops}
                  now={now}
                  label={t('next.smart.stack.machine')}
                  onBack={askDropBack}
                  onDismiss={dismissDrops}
                  renderAnswer={renderMachine}
                />
              )}
              {widgetsMounted && machine && (
                <div className="hx-wgs-wrap" data-shown={widgetsShown} data-pop={widgetsPop || undefined}>
                  <SideWidgetsBinding machine={machine} onOpenChat={onOpenChat} />
                </div>
              )}
            </>
          ) : null
        }
        codes={
          <>
            <NoticeStack
              drops={drops}
              now={now}
              label={t('next.smart.drops.title')}
              onBack={askDropBack}
              onOpenChat={onOpenChat}
              onDismiss={dismissDrops}
              renderAnswer={renderAnswer}
            />
            <CodeAsks
              asks={codes}
              now={now}
              skew={codeAsks.skew}
              onPut={putOne}
              onCancel={async (ask) => {
                if (machine) await cancelCode(machine, ask.id);
              }}
              onDone={(ask) => codeAsks.drop(ask.id)}
            />
          </>
        }
      />
    ) : null;

  return { header, panel, show: () => setAsked({ conv, shown: true }) };
};
