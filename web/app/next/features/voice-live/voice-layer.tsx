// The voice belongs to the chat it was started in («Голос — в другом чате»): the talk does not end when the
// person goes to another chat, the settings or a design. The layer, mounted once for the app, owns every chat's voice;
// a chat's field only lends it a place (VoiceSlot). While the chat is on screen the voice's elements stand in its field;
// when it leaves they move to a hidden place of the layer and the talk goes on — the DOM node moves, React keeps the
// same instance (a portal into a node that never changes), so nothing restarts.
import { useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react';
import { createPortal } from 'react-dom';
import type { IDockExtra } from '@/next/bindings/chat-dock';
import VoiceLiveAgent from '@/next/bindings/voice-live-agent';
import VoiceLiveGpt from '@/next/bindings/voice-live-gpt';
import { gptIdOf } from '@/next/features/chatgpt/voice-bridge';
import { askVoiceStart, useVoiceNow, VOICE_MOVED_EVENT } from '@/next/features/voice-live/field';
import { dropCutTalk, keepTalk, useCutTalk } from '@/next/features/voice-live/cut-talk';
import { t } from '@/next/i18n';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import '@/next/styles/voice-layer.css';

interface IDock {
  id: number;
  machine: MachineId;
  conv: ConvId;
  extra: IDockExtra | null;
  slot: HTMLElement;
}

/** The places the chats' fields lend, the latest last: two fields of one chat (the float and the chat) — the latest. */
let docks: readonly IDock[] = [];
let seq = 0;
const listeners = new Set<() => void>();
const emit = () => {
  for (const listen of [...listeners]) listen();
};
const subscribe = (listen: () => void) => {
  listeners.add(listen);
  return () => listeners.delete(listen);
};
const useDocks = () =>
  useSyncExternalStore(
    subscribe,
    () => docks,
    () => docks,
  );

/** The place of a chat's voice in its field. The voice itself lives in the layer. */
export const VoiceSlot = ({
  machine,
  conv,
  extra = null,
}: {
  machine: MachineId;
  conv: ConvId;
  extra?: IDockExtra | null;
}) => {
  const ref = useRef<HTMLSpanElement>(null);
  const id = useRef(0);
  useLayoutEffect(() => {
    const slot = ref.current;
    if (!slot) return;
    seq += 1;
    id.current = seq;
    docks = [...docks, { id: seq, machine, conv, extra: null, slot }];
    emit();
    return () => {
      docks = docks.filter((d) => d.id !== id.current);
      emit();
    };
  }, [machine, conv]);
  // The host's additions change with what is picked on a board: the voice takes the fresh ones.
  useEffect(() => {
    docks = docks.map((d) => (d.id === id.current ? { ...d, extra } : d));
    emit();
  }, [extra]);
  const cut = useCutTalk(conv);
  return (
    <>
      {cut && (
        <button
          type="button"
          className="hx-voice-cut"
          onClick={() => {
            dropCutTalk();
            askVoiceStart();
          }}
        >
          {t('next.chatgpt.voice.cut.text')} · <b>{t('next.chatgpt.voice.cut.go')}</b>
        </button>
      )}
      <span ref={ref} className="hx-voice-slot" />
    </>
  );
};

/** The chat's field on screen now, if one is: the last place it lent that is shown. */
const shown = (slot: HTMLElement) => slot.getClientRects().length > 0;
/** A kept screen (a design behind another tab) keeps its field, hidden: a field on screen comes first. */
const shownDock = (list: readonly IDock[], conv: ConvId): IDock | null => {
  const mine = list.filter((d) => d.conv === conv && d.slot.isConnected);
  return mine.findLast((d) => shown(d.slot)) ?? mine.at(-1) ?? null;
};

/** True while the chat's field is on screen (the voice of that chat needs no bar). `at` is where the person is (the
 *  route): a kept screen hides without leaving the list, so the check runs again when it changes, after paint. */
export const useVoiceDocked = (conv: ConvId | null, at: string): boolean => {
  const list = useDocks();
  // Read again a frame later: a kept screen is hidden in the same commit that changes the route.
  const [, setFrame] = useState(0);
  useEffect(() => {
    const frame = window.requestAnimationFrame(() => setFrame((n) => n + 1));
    return () => window.cancelAnimationFrame(frame);
  }, [list, conv, at]);
  const dock = conv ? shownDock(list, conv) : null;
  return dock !== null && shown(dock.slot);
};

interface IVoiceEntry {
  machine: MachineId;
  conv: ConvId;
  extra: IDockExtra | null;
}

/** One chat's voice: a node of its own, moved into the chat's field or into the layer's hidden place. */
const VoiceHome = ({
  entry,
  slot,
  parking,
}: {
  entry: IVoiceEntry;
  slot: HTMLElement | null;
  parking: HTMLElement | null;
}) => {
  const node = useNodeOnce();
  useLayoutEffect(() => {
    const into = slot ?? parking;
    if (!into || node.parentElement === into) return;
    into.appendChild(node);
    window.dispatchEvent(new Event(VOICE_MOVED_EVENT));
  }, [slot, parking, node]);
  useEffect(() => () => node.remove(), [node]);
  // A ChatGPT chat's talk (voice-bridge.ts) has its own voice; an agent chat's goes to its agent.
  const gpt = gptIdOf(entry.conv);
  return createPortal(
    gpt === null ? (
      <VoiceLiveAgent machine={entry.machine} conv={entry.conv} extra={entry.extra} />
    ) : (
      <VoiceLiveGpt machine={entry.machine} conv={entry.conv} id={gpt} />
    ),
    node,
  );
};

const useNodeOnce = (): HTMLElement =>
  useState(() => {
    const el = document.createElement('span');
    el.className = 'hx-voice-home';
    return el;
  })[0];

/** The app's voice layer: every chat field's voice, and the talk that goes on away from its chat. */
const VoiceLayer = () => {
  const list = useDocks();
  const now = useVoiceNow();
  const [parking, setParking] = useState<HTMLDivElement | null>(null);
  // The talk going on is kept for the page after a reload, which then says it was cut (cut-talk.ts).
  const talk = now?.conv ?? null;
  const was = useRef<string | null>(null);
  useEffect(() => {
    if (talk === was.current) return;
    if (talk !== null || was.current !== null) keepTalk(talk);
    was.current = talk;
  }, [talk]);

  const entries = new Map<ConvId, IVoiceEntry>();
  for (const d of list) entries.set(d.conv, { machine: d.machine, conv: d.conv, extra: d.extra });
  if (now?.conv && !entries.has(now.conv))
    entries.set(now.conv, { machine: now.machine, conv: now.conv, extra: null });

  return (
    <div ref={setParking} className="hx-voice-parking" aria-hidden="true">
      {[...entries.values()].map((entry) => (
        <VoiceHome
          key={`${entry.machine}/${entry.conv}`}
          entry={entry}
          slot={shownDock(list, entry.conv)?.slot ?? null}
          parking={parking}
        />
      ))}
    </div>
  );
};

export default VoiceLayer;
