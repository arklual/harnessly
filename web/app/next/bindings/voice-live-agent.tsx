// The voice mode in an agent's chat: what the person asks for out loud goes to the chat's agent as a message,
// and when the agent's turn is over its answer goes back to the voice, which tells it in a sentence or two.
import { useCallback, useEffect, useRef, useState } from 'react';
import { spokenResult, spokenTask } from '@/next/features/voice-live/spoken';
import { ItemsBuilder } from '@/next/bindings/feed-items';
import { decidePermission, sendMessage } from '@/next/commands';
import { useConversationChannel } from '@/next/core/realtime/hub';
import {
  permissionOption,
  permissionWhat,
  sayable,
  voicePermission,
} from '@/next/features/voice-live/permission';
import type { Permission } from '@/next/protocol/v2';
import { chatLines } from '@/next/features/app-views/host-handlers';
import {
  quickEdit,
  quickEditApp,
  undoQuickEdit,
  undoQuickEditApp,
  VOICE_MODE,
  warmQuickEdit,
  type IAppObject,
} from '@/next/features/voice-live/api';
import { RuntimeError } from '@/next/core/net/runtime-json';
import { ANSWER, answerOf, needsYes } from '@/next/features/voice-live/confirm';
import { flashLiveHint, LIVE_PHASE, setLiveMark } from '@/next/features/voice-live/live-mark';
import { refusalCode } from '@/next/features/chatgpt/api';
import { t } from '@/next/i18n';
import type { IDockExtra } from '@/next/bindings/chat-dock';
import VoiceLive, { type IVoiceLiveControls } from '@/next/features/voice-live/voice-live';
import type { IVoiceTask } from '@/next/features/voice-live/use-voice-call';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { useAgentState, sendModeNow } from '@/next/stores/chat-state';
import { openGates, useGates, useNow } from '@/next/stores/conversation';
import { progressPhrase, shouldSay } from '@/next/features/voice-live/progress';
import { useFileSelectionChip } from '@/next/stores/file-selection';
import { useChatPane } from '@/next/stores/pane-state';
import { recentLines, screenText } from '@/next/bindings/voice-screen';
import { callStyle, DELIVERY } from '@/next/features/voice-live/style';
import { isNoiseText } from '@/next/features/voice-live/noise';
import { useVoiceHands } from '@/next/bindings/voice-hands';

/** Words about the page, the chats, the model or the talk itself, not about the open board: the router takes them. */
const COMMAND_RE =
  /(открой|откро|закрой|покажи|переключ|тем[уаеы]\b|сайдбар|боков|панел|настройк|чат|модел|опус|сонет|хайку|уров[ен]|глубж|быстрее|разреш|вариант|вспомн|помнишь|прочитай|читай|продолж|новый|новую|создай|заверш|стоп|хватит|open|theme|sidebar|settings|chat|model|switch|read)/i;

/** «Верни», «отмени», «откати»: the last quick edit put back. */
const UNDO_RE = /^(верни|отмени|откати|undo|revert)/i;
/** Files beside the chat a quick edit may change: text the fast model can read whole. */
const TEXT_FILE_RE =
  /\.(html?|css|scss|less|m?[jt]sx?|json|md|mdx|txt|ya?ml|toml|svg|csv|go|py|rb|rs|java|kt|swift|sh|sql|xml)$/i;

/** The text file in front of the chat's pane, or null. */
const paneFileOf = (pane: ReturnType<typeof useChatPane>): string | null => {
  const tab = pane.open ? pane.tabs.find((x) => x.id === pane.active) : undefined;
  if (tab?.kind !== 'files' || !tab.file || tab.against) return null;
  return TEXT_FILE_RE.test(tab.path) ? tab.path : null;
};

/** «Меняет» follows «узнал» after this: a ring that only flashes by reads as noise. */
const FOUND_MS = 250;
/** The board takes the changed file in a moment: the flash waits for it. */
const SETTLE_EDIT_MS = 350;
/** Pieces of one phrase handed over this close are one task. */
const MERGE_MS = 1200;
/** Two pieces of one phrase as one task: the later id, the words joined. */
const joinTasks = (a: IVoiceTask, b: IVoiceTask): IVoiceTask => ({
  id: b.id,
  text: `${a.text.trim()} ${b.text.trim()}`.trim(),
  said: `${a.said?.trim() ?? ''} ${b.said?.trim() ?? ''}`.trim(),
});
/** The words a refusal carries (edit_needs_yes: what the planned removal does), '' without them. */
const refusedDetail = (failure: unknown): string => {
  if (!(failure instanceof RuntimeError)) return '';
  const body = failure.body;
  return typeof body === 'object' && body !== null && 'detail' in body ? String(body.detail) : '';
};
/** How long the agent stays stopped before its turn counts as over. */
const SETTLE_MS = 2500;

interface IVoiceLiveAgentProps {
  machine: MachineId;
  conv: ConvId;
  /** The host's additions (a design's picked blocks and open page): a spoken task carries them as a typed one does. */
  extra?: IDockExtra | null;
}

const VoiceLiveAgent = ({ machine, conv, extra = null }: IVoiceLiveAgentProps) => {
  const agent = useAgentState(conv);
  const working = agent?.k === 'working';
  // The agent waits for a permission (an action that goes outside, during a call): its task is not over.
  const waitsPermission = agent?.k === 'waiting-permission';
  const [tasks, setTasks] = useState<readonly string[]>([]);
  const controls = useRef<IVoiceLiveControls | null>(null);
  // A file open beside the chat takes the quick edits when the host (a design) offers none.
  const pane = useChatPane(conv);
  const paneFile = paneFileOf(pane);
  const fileChip = useFileSelectionChip(conv);
  const paneEdit: IDockExtra['voiceEdit'] = paneFile
    ? {
        file: paneFile,
        selection: () => (fileChip?.path === paneFile ? fileChip.selection.content : ''),
      }
    : null;
  const voiceEdit = extra?.voiceEdit ?? paneEdit;
  const hands = useVoiceHands({ machine, conv, overBoard: Boolean(voiceEdit), controls });
  const started = useRef(false);
  // A task that waits for «да» (confirm.ts): the next words decide.
  const held = useRef<IVoiceTask | null>(null);
  // A removal from an app's object planned and waiting for «да» (edit_needs_yes): the same edit, confirmed.
  const heldRemoval = useRef<{ app: IAppObject; ask: string; selection: string } | null>(null);
  // A task waiting MERGE_MS for the rest of its phrase.
  const pending = useRef<{ task: IVoiceTask; timer: number } | null>(null);

  // While the agent works on a spoken task, its steps are said in a few words (progress.ts): «смотрю доску», «правлю…».
  const now = useNow(conv);
  const phrase = tasks.length > 0 && working ? progressPhrase(now) : null;
  const saidStep = useRef<{ text: string; at: number } | null>(null);
  useEffect(() => {
    if (tasks.length === 0) saidStep.current = null;
    const at = Date.now();
    if (!shouldSay(phrase, saidStep.current, at) || phrase === null) return;
    // «Помолчи, скажи просто результат»: the steps are not told (style.ts).
    if (callStyle.get().delivery === DELIVERY.result) return;
    saidStep.current = { text: phrase, at };
    void controls.current?.say(`How the work goes; say just this, in a few words, nothing more: «${phrase}»`);
  }, [phrase, tasks.length]);

  // A task is over when the agent has worked since it was sent and stays stopped for a moment: the turn's last answer
  // lands in the feed a little after the state, and a turn may pause between steps.
  useEffect(() => {
    if (tasks.length === 0) return;
    if (working || waitsPermission) {
      started.current = true;
      return;
    }
    if (!started.current) return;
    const timer = window.setTimeout(() => {
      started.current = false;
      const last = chatLines(new ItemsBuilder(conv, machine).snapshot(), 1)[0];
      const text = last?.role === 'assistant' ? spokenResult(last.text) : '';
      const task = tasks[tasks.length - 1];
      setTasks([]);
      void controls.current?.say(text || 'The agent finished without an answer.', task);
    }, SETTLE_MS);
    return () => window.clearTimeout(timer);
  }, [working, waitsPermission, tasks, conv, machine]);

  // The permission the agent of a spoken task waits for is said and answered by voice («да» / «нет»), not on a card.
  useConversationChannel(waitsPermission ? machine : null, waitsPermission ? conv : null);
  const gates = useGates(waitsPermission ? conv : null);
  const asked = tasks.length > 0 && waitsPermission ? voicePermission(openGates(gates).permissions) : null;
  const heldPermission = useRef<Permission | null>(null);
  const answerPermission = useCallback(
    (allow: boolean, task?: string) => {
      const p = heldPermission.current;
      heldPermission.current = null;
      if (!p) return;
      const option = permissionOption(p, allow);
      if (option) void decidePermission(machine, conv, p.id, option);
      void controls.current?.say(
        t(allow ? 'next.chatgpt.voice.permission.allowed' : 'next.chatgpt.voice.confirm.dropped'),
        task,
      );
    },
    [machine, conv],
  );
  // The permission said last: each is told once.
  const toldPermission = useRef<string | null>(null);
  useEffect(() => {
    if (!asked) {
      heldPermission.current = null;
      return;
    }
    if (toldPermission.current === asked.id) return;
    toldPermission.current = asked.id;
    const c = controls.current;
    const task = tasks[tasks.length - 1];
    // A long or chained command is not allowed by voice: a «да» would cover what was not said (security review).
    if (!sayable(asked)) return void c?.say(t('next.chatgpt.voice.permission.onScreen'), task);
    heldPermission.current = asked;
    const what = permissionWhat(asked) || t('next.chatgpt.voice.permission.something');
    const ask = t('next.chatgpt.voice.permission.ask', { what });
    void c?.say(ask, task);
    c?.note(ask, {
      label: t('next.chatgpt.voice.confirm.yes'),
      run: async () => {
        if (heldPermission.current?.id === asked.id) answerPermission(true);
      },
    });
  }, [asked, tasks, answerPermission]);

  /** «Да» to a removal the app planned: the same edit, confirmed (the plan is kept by the machine for 2 min). */
  const applyRemoval = (taskId: string, say: IVoiceLiveControls['say'], note: IVoiceLiveControls['note']) => {
    const held = heldRemoval.current;
    heldRemoval.current = null;
    if (!held) return;
    quickEditApp(machine, { ...held.app, selection: held.selection, ask: held.ask, confirm: true }).then(
      (res) => {
        const said = res.said || t('next.chatgpt.voice.edited');
        void say(said, taskId);
        note(said, {
          label: t('next.chatgpt.voice.undo'),
          run: () => undoQuickEditApp(machine, held.app),
          after: t('next.chatgpt.voice.undoneNote', { text: said }),
        });
      },
      () => void say(t('next.chatgpt.voice.removalFailed'), taskId),
    );
  };

  /** A spoken task, let go: a quick edit of the open board, or a message to the chat's agent. */
  const run = (task: IVoiceTask, say: IVoiceLiveControls['say'], note: IVoiceLiveControls['note']) => {
    const toAgent = (routedText?: string) => {
      setTasks((was) => [...was, task.id]);
      const words = routedText?.trim() || spokenTask(task);
      // Nothing heard: no message with only the board's context around an empty line (15.11, three such).
      if (!words.trim()) return void say(t('next.chatgpt.voice.notHeard'), task.id);
      const text = extra ? extra.wrap(words) : words;
      sendMessage(machine, { conversationId: conv, text, attachments: [], mode: sendModeNow(conv) });
      extra?.onSent();
    };
    const edit = voiceEdit;
    // Through the router: what it cannot place goes on as `fallback` says (the agent, or «занято»).
    const viaRouter = (fallback: () => void) =>
      void hands.route(task, say, toAgent).then((done) => {
        if (!done) fallback();
      });
    // Over an open board or file a command about the page, the chats or the talk is not an edit (00:18 17.11: «открой
    // …» over a design went to the quick edit and came back «занято»): it goes through the router too.
    if (edit && COMMAND_RE.test(task.said?.trim() || task.text)) return viaRouter(toAgent);
    if (!edit) {
      // Everything but a quick edit goes through the router first; when it is not there, to the agent as before.
      void hands.route(task, say, toAgent).then((done) => {
        if (!done) toAgent();
      });
      return;
    }
    // An app's object (a board, a document; HRN-1469): the app plans and applies the change, a removal asks «да».
    const app = edit.app;
    if (app) {
      if (UNDO_RE.test(task.text.trim())) {
        undoQuickEditApp(machine, app).then(
          () => void say(t('next.chatgpt.voice.undone'), task.id),
          () => toAgent(),
        );
        return;
      }
      const ask = task.said?.trim() || task.text;
      const selection = edit.selection();
      quickEditApp(machine, { ...app, selection, ask }).then(
        (res) => {
          if (!res.changed) return toAgent();
          const said = res.said || t('next.chatgpt.voice.edited');
          void say(said, task.id);
          note(said, {
            label: t('next.chatgpt.voice.undo'),
            run: () => undoQuickEditApp(machine, app),
            after: t('next.chatgpt.voice.undoneNote', { text: said }),
          });
        },
        (refused: unknown) => {
          const code = refusalCode(refused);
          if (code === 'edit_needs_yes') {
            heldRemoval.current = { app, ask, selection };
            const plan = refusedDetail(refused) || ask;
            const question = t('next.chatgpt.voice.confirm.ask', { task: plan });
            void say(question, task.id);
            note(question, {
              label: t('next.chatgpt.voice.confirm.yes'),
              run: async () => {
                if (heldRemoval.current?.app === app) applyRemoval(task.id, say, note);
              },
            });
            return;
          }
          if (code === 'edit_taken' || code === 'edit_conflict')
            return viaRouter(() => void say(t('next.chatgpt.voice.taken'), task.id));
          toAgent();
        },
      );
      return;
    }
    // A board is open: the small edit goes fast to its file; what a quick edit cannot do goes to the agent.
    if (UNDO_RE.test(task.text.trim())) {
      undoQuickEdit(machine, edit.file).then(
        () => void say(t('next.chatgpt.voice.undone'), task.id),
        () => toAgent(),
      );
      return;
    }
    const tpl = edit.tpl?.() ?? null;
    let editing = 0;
    if (tpl !== null) {
      setLiveMark({ tpl, phase: LIVE_PHASE.found });
      editing = window.setTimeout(() => setLiveMark({ tpl, phase: LIVE_PHASE.editing }), FOUND_MS);
    }
    const unmark = (done: boolean) => {
      window.clearTimeout(editing);
      if (tpl === null) return;
      if (!done) return setLiveMark(null);
      window.setTimeout(() => setLiveMark({ tpl, phase: LIVE_PHASE.done }), SETTLE_EDIT_MS);
    };
    quickEdit(machine, {
      file: edit.file,
      selection: edit.selection(),
      ask: task.said?.trim() || task.text,
      chat: conv,
    }).then(
      (res) => {
        unmark(res.changed);
        if (!res.changed) return toAgent();
        // No picked block: the board finds the one that changed by its lines, once it has the new file.
        const hint = res.hint;
        if (tpl === null && hint) window.setTimeout(() => flashLiveHint(hint), SETTLE_EDIT_MS * 2);
        const said = res.said || t('next.chatgpt.voice.edited');
        void say(said, task.id);
        note(said, {
          label: t('next.chatgpt.voice.undo'),
          run: () => undoQuickEdit(machine, edit.file),
          after: t('next.chatgpt.voice.undoneNote', { text: said }),
        });
      },
      (refused: unknown) => {
        unmark(false);
        if (refusalCode(refused) === 'edit_taken') {
          // The agent holds the board: the words go through the router (the agent's chat, or what else they ask).
          viaRouter(() => void say(t('next.chatgpt.voice.taken'), task.id));
          return;
        }
        toAgent();
      },
    );
  };

  /** A whole spoken task: a «да»/«нет» to the held one, held itself for a «да», or let go. */
  const decide = (spoken: IVoiceTask, say: IVoiceLiveControls['say'], note: IVoiceLiveControls['note']) => {
    if (heldRemoval.current) {
      const answer = answerOf(spoken.text);
      if (answer === ANSWER.yes) return applyRemoval(spoken.id, say, note);
      heldRemoval.current = null;
      if (answer === ANSWER.no) return void say(t('next.chatgpt.voice.confirm.dropped'), spoken.id);
    }
    if (heldPermission.current) {
      const answer = answerOf(spoken.text);
      if (answer === ANSWER.yes) return answerPermission(true, spoken.id);
      if (answer === ANSWER.no) return answerPermission(false, spoken.id);
    }
    const waiting = held.current;
    held.current = null;
    if (waiting) {
      const answer = answerOf(spoken.text);
      if (answer === ANSWER.no) return void say(t('next.chatgpt.voice.confirm.dropped'), spoken.id);
      // The answer goes to the task the «да» was said as: the held one is over for the voice.
      if (answer === ANSWER.yes) return run({ ...waiting, id: spoken.id }, say, note);
    }
    if (needsYes(spoken.text)) {
      held.current = spoken;
      const ask = t('next.chatgpt.voice.confirm.ask', { task: spoken.text.trim() });
      void say(ask, spoken.id);
      note(ask, {
        label: t('next.chatgpt.voice.confirm.yes'),
        run: async () => {
          if (held.current !== spoken) return;
          held.current = null;
          run(spoken, say, note);
        },
      });
      return;
    }
    run(spoken, say, note);
  };

  return (
    <VoiceLive
      machine={machine}
      conv={conv}
      mode={VOICE_MODE.agent}
      context={() => ''}
      primer={() => ({
        conv,
        screen: screenText({
          tab: pane.open ? pane.tabs.find((x) => x.id === pane.active) : undefined,
          selection: fileChip?.selection.content,
          host: extra?.voiceContext,
          lines: recentLines(conv, machine),
        }),
      })}
      working={tasks.length > 0}
      controlsRef={controls}
      onTask={(piece, { say, note }) => {
        if (!piece.text.trim() && !piece.said?.trim()) return;
        // A noise the recognizer made words of (noise.ts) is not a task: nothing goes to a chat.
        if (isNoiseText(piece.said?.trim() || piece.text)) {
          void say('That was background noise, not the person: ignore it and say nothing.', piece.id, false);
          return;
        }
        // A phrase cut by a pause comes as two tasks (15.11: «и нарисуем краткую» + «нормальную интеграцию…»): the
        // pieces said within MERGE_MS are one task. A «да» or «нет» to a held task is not waited for.
        if (!held.current && !heldPermission.current && !heldRemoval.current) {
          const was = pending.current;
          window.clearTimeout(was?.timer);
          const spoken = was ? joinTasks(was.task, piece) : piece;
          if (was) void say('Joined with the next words.', was.task.id, false);
          pending.current = {
            task: spoken,
            timer: window.setTimeout(() => {
              pending.current = null;
              decide(spoken, say, note);
            }, MERGE_MS),
          };
          return;
        }
        decide(piece, say, note);
      }}
      onBegin={() => {
        hands.onBegin();
        if (voiceEdit) void warmQuickEdit(machine).catch(() => undefined);
      }}
      onLine={hands.onLine}
      onEnd={() => {
        hands.onEnd();
        window.clearTimeout(pending.current?.timer);
        pending.current = null;
        setTasks([]);
        started.current = false;
      }}
    />
  );
};

export default VoiceLiveAgent;
