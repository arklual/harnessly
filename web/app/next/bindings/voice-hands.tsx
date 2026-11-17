// The voice's hands, one for both kinds of talk — a chat with an agent («Задача») and a ChatGPT talk
// («Разговор»): what the voice hands over goes through the router (voice-router.ts) and the plan is carried out on the
// whole page; chats that wait for the person are told by voice; a long text is read out loud part by part.
import { useCallback, useEffect, useRef, useState } from 'react';
import { ItemsBuilder } from '@/next/bindings/feed-items';
import { chatLines } from '@/next/features/app-views/host-handlers';
import {
  routeRequest,
  routeVoice,
  runPlan,
  watchWaiting,
  type IRouteAction,
} from '@/next/bindings/voice-router';
import { permissionWhat, sayable } from '@/next/features/voice-live/permission';
import {
  nextPart,
  readInstruction,
  reading,
  pauseReading,
  resumeReading,
  startReading,
  stopReading,
} from '@/next/features/voice-live/reader';
import { ChatsCard, ReadingCard, TextCard } from '@/next/features/voice-live/stage-cards';
import { showOnVoiceStage } from '@/next/features/voice-live/voice-stage';
import type { IVoiceLiveControls } from '@/next/features/voice-live/voice-live';
import type { IVoiceLine, IVoiceTask } from '@/next/features/voice-live/use-voice-call';
import { useThemeMaybe } from '@/platform/theme';
import { useRail } from '@/next/bindings/rail';
import { goTo, ROUTE } from '@/next/routes/routes';
import { conversationsByActivity, conversationsStore } from '@/next/stores/conversations';
import { gatesStore, openGates } from '@/next/stores/conversation';
import { startFolder } from '@/next/bindings/home-composer';
import { t } from '@/next/i18n';
import type { ConvId, MachineId } from '@/next/protocol/v2';

interface IVoiceHandsArgs {
  machine: MachineId;
  /** The agent's chat the talk is in; null in a ChatGPT talk. */
  conv: ConvId | null;
  /** A ChatGPT talk: its title (the router's «agent» is that talk writing). */
  chatHere?: string | null;
  controls: { current: IVoiceLiveControls | null };
}

export const useVoiceHands = ({ machine, conv, chatHere = null, controls }: IVoiceHandsArgs) => {
  const theme = useThemeMaybe();
  const resolvedTheme = theme?.resolvedTheme ?? 'dark';
  const setTheme = (next: string) => theme?.setTheme(next as Parameters<typeof theme.setTheme>[0]);
  const railFolded = useRail();
  // The call's last lines, for the router (what «второй», «да», «продолжи» answer).
  const history = useRef<string[]>([]);
  const [live, setLive] = useState(false);
  // The part of a long reading the voice was given and has not finished saying (reader.ts).
  const readingSent = useRef(false);

  // During a call every chat that starts waiting for the person (a question, a permission) is told by voice: the
  // person answers out loud and the router answers the chat. Its channel is opened so its words are known.
  useEffect(() => {
    if (!live) return;
    const release = watchWaiting();
    const told = new Set<string>();
    const waitingNow = () =>
      conversationsByActivity().filter(
        (c) => !c.test && (c.state.k === 'waiting-question' || c.state.k === 'waiting-permission'),
      );
    // What waits already when the call starts is told too, once its words arrive.
    const check = () => {
      for (const c of waitingNow()) {
        const g = openGates(gatesStore.get(c.id) ?? { questions: [], permissions: [] });
        const q = g.questions[0]?.items[0];
        const p = g.permissions[0];
        const key = `${c.id}:${q ? g.questions[0]?.id : p?.id}`;
        if (told.has(key) || (!q && !p)) continue;
        // The voice's own chat's short permission is asked by the old way (permission.ts); a long or chained one is
        // told here too, in gist, with the whole command on the screen («почему просит до сих пор»).
        if (c.id === conv && p && !q && sayable(p)) continue;
        told.add(key);
        const what = q
          ? `asks: «${q.question || q.header}»; options: ${q.options.map((o, i) => `${i + 1}) ${o.label}`).join('; ')}`
          : `wants permission to run: «${(p ? permissionWhat(p) : '').slice(0, 400)}» (the whole of it is on the ` +
            'screen; tell its gist in a few words, not the command itself)';
        void controls.current?.say(
          `The chat «${c.title || 'untitled'}» ${what}. Tell the person in one or two short sentences (options by ` +
            'their short names) and ask what to answer; when they answer, hand it over.',
        );
      }
    };
    const timer = window.setInterval(check, 2000);
    const unsub = conversationsStore.subscribeAll(check);
    return () => {
      window.clearInterval(timer);
      unsub();
      release();
    };
  }, [live, conv, controls]);

  /** Says the next part of a long reading, or ends it. */
  const readOn = useCallback((say: IVoiceLiveControls['say']) => {
    const r = reading();
    if (!r || r.paused) return;
    const part = readingSent.current ? nextPart() : (r.parts[r.at] ?? null);
    const now = reading();
    if (part === null || !now) {
      readingSent.current = false;
      return;
    }
    readingSent.current = true;
    void say(readInstruction(part, now.at, now.parts.length));
  }, []);

  /** The text a read_aloud action asks for: given, or the chat's last answer. */
  const readSource = (a: IRouteAction): string => {
    if (a.text && a.source !== 'chat' && a.source !== 'answer') return a.text;
    const target = (a.chat && conversationsStore.get(a.chat as ConvId)) || null;
    const id = target?.id ?? conv;
    if (!id) return a.text ?? '';
    const m = target?.machineId ?? machine;
    const last = chatLines(new ItemsBuilder(id, m).snapshot(), 6)
      .filter((l) => l.role === 'assistant')
      .pop();
    return last?.text ?? a.text ?? '';
  };

  /** A task through the router (voice-router.ts): the plan done here; false when the old way should take it. */
  const routed = async (
    task: IVoiceTask,
    say: IVoiceLiveControls['say'],
    toAgent: (text?: string) => void,
  ): Promise<boolean> => {
    const ctx = {
      machine,
      conv,
      screen: chatHere
        ? `route: ChatGPT talk «${chatHere}» (a plain talk: it answers and writes texts; it has no machine)`
        : '',
      history: history.current,
      theme: resolvedTheme,
      sidebarFolded: railFolded,
    };
    let plan;
    try {
      plan = await routeVoice(machine, routeRequest(task.text, task.said, ctx));
    } catch {
      return false;
    }
    const onlyAgent = plan.actions.length > 0 && plan.actions.every((a) => a.do === 'agent');
    if (onlyAgent) {
      toAgent(plan.actions.map((a) => a.text).join('\n'));
      return true;
    }
    const cur = conv ? conversationsStore.get(conv) : undefined;
    const ok = await runPlan(plan, {
      machine,
      conv,
      chatHere,
      folder: cur?.cwd ?? startFolder([...conversationsByActivity()], machine),
      setTheme,
      sidebarFolded: railFolded,
      toAgent: (text) => toAgent(text),
      show: (what, a) => {
        if (what === 'chats') {
          showOnVoiceStage(
            <ChatsCard
              chats={conversationsByActivity()
                .filter((c) => !c.test)
                .slice(0, 30)}
              onOpen={(c) => goTo({ kind: ROUTE.chat, id: c.id })}
            />,
            t('next.voice.cards.chats'),
          );
          return;
        }
        const text = what === 'chat' ? readSource({ ...a, source: 'chat' }) : (a.text ?? '');
        if (text) showOnVoiceStage(<TextCard text={text} />, t('next.voice.cards.text'));
      },
      readAloud: (a) => {
        if (a.source === 'resume') {
          // The plan's line («продолжаю») is said first; the part follows when it is over (onLine).
          resumeReading();
          readingSent.current = false;
          return;
        }
        const text = readSource(a);
        if (!text.trim() || !startReading(t('next.voice.cards.reading'), text)) return;
        readingSent.current = false;
        showOnVoiceStage(
          <ReadingCard
            onResume={() => {
              readingSent.current = false;
              readOn(say);
            }}
          />,
          t('next.voice.cards.reading'),
        );
        // The plan's line first («читаю»), then the text: the part goes when that line is over (onLine).
      },
      stop: () => {
        stopReading();
        // The goodbye is said first; then the call ends.
        window.setTimeout(() => controls.current?.end?.(), 3500);
      },
    });
    if (!ok) return false;
    void say(`Say just this to the person, in a few words: «${plan.say}»`, task.id);
    return true;
  };

  const onLine = (line: IVoiceLine) => {
    if (!line.done || !line.text.trim()) return;
    history.current = [
      ...history.current,
      `${line.role === 'user' ? 'person' : 'voice'}: ${line.text}`,
    ].slice(-12);
    const r = reading();
    if (!r || !controls.current) return;
    // The person spoke: the reading waits for them («продолжи» goes on); the voice finished a part: the next one.
    if (line.role === 'user') {
      if (!r.paused) pauseReading();
      readingSent.current = false;
      return;
    }
    if (!r.paused) readOn(controls.current.say);
  };
  const onBegin = () => setLive(true);
  const onEnd = () => {
    setLive(false);
    stopReading();
    showOnVoiceStage(null);
    history.current = [];
  };
  return { route: routed, onLine, onBegin, onEnd };
};
