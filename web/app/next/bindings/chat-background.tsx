// The chat's right column: Claude's recap of the session, the pictures the agent looked at (ConvSnapshot.shots) and its
// background tasks (ConvSnapshot.background and its events, and the conversation's recap). It is open by itself while there is something in it, there is room for it beside
// the chat's column (the left sidebar takes its width off the stage, so it counts too) and the right pane is shut.
// The pane opening always takes it away, even when the person had opened it; the pane shutting gives back what the room
// says. The header's button shows it or folds it, and a fold stays until the chat is changed. A stop goes to the
// runtime as bg.stop and waits for it.
import { useState } from 'react';
import { settled } from '@/next/bindings/outcome';
import { stopBackground } from '@/next/commands';
import BackgroundPanel from '@/next/features/background/background-panel';
import { useRailRoom } from '@/next/features/background/rail-room';
import type { IHeaderBackground } from '@/next/features/header/chat-header';
import { machineApi } from '@/next/core/net/machine';
import type { BackgroundTask, ConvId, MachineId, Shot } from '@/next/protocol/v2';
import { useBackground, useShots } from '@/next/stores/conversation';
import { useConversation } from '@/next/stores/conversations';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

export const useChatBackground = (
  conv: ConvId | null,
  machine: MachineId | null,
  now: number,
): { header: IHeaderBackground | null; panel: React.ReactNode; show: () => void } => {
  const tasks = useBackground(conv);
  const recap = useConversation(conv)?.recap ?? null;
  const shots = useShots(conv);
  // What the person asked of the column in this chat: null leaves it to the room there is.
  const [asked, setAsked] = useState<{ conv: ConvId | null; shown: boolean } | null>(null);
  const [rail, setRail] = useState<HTMLElement | null>(null);
  const [stopping, setStopping] = useState<ReadonlySet<string>>(() => new Set());
  const { room, pane } = useRailRoom(rail);
  const has = tasks.length > 0 || recap !== null || (shots?.length ?? 0) > 0;
  // The pane opening takes back a column the person had opened (a fold they made stays); this runs in render, once per change.
  const [paneWas, setPaneWas] = useState(pane);
  if (pane !== paneWas) {
    setPaneWas(pane);
    if (pane && asked?.shown) setAsked(null);
  }
  const override = asked && asked.conv === conv ? asked.shown : null;
  // On its own the column is open when it has room and the pane is shut.
  const auto = room && !pane;
  const shown = has && (override ?? auto);

  // What the button asks for: what the room already says is no ask at all, so the column keeps following the room.
  const toggle = () => {
    const next = !shown;
    setAsked(next === auto ? null : { conv, shown: next });
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

  const shotSrc = (shot: Shot): string =>
    conv && machine
      ? machineApi(machine).http(`/conversations/${conv}/shot?id=${encodeURIComponent(shot.id)}`)
      : '';

  const header: IHeaderBackground | null =
    conv && has
      ? {
          running: tasks.filter((x) => x.status === 'running').length,
          total: tasks.length,
          recap: recap !== null,
          open: shown,
          onToggle: toggle,
        }
      : null;

  const panel = has ? (
    <BackgroundPanel
      ref={setRail}
      tasks={tasks}
      recap={recap}
      shots={shots}
      shotSrc={shotSrc}
      shown={shown}
      now={now}
      stopping={stopping}
      onStop={onStop}
      onHide={shown ? toggle : undefined}
    />
  ) : null;

  return { header, panel, show: () => setAsked({ conv, shown: true }) };
};
