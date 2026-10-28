// Settings → Notifications → Telegram on the machine the person has open. With a shared bot the person's
// Telegram is connected with one button and the bots of their own are offered only if the machine allows them; with
// none, the bots of their own as before. Everything is read when the page opens, every change taken only from the
// machine's answer. While a chat is being bound the state is read again every two seconds, so the chat shows the moment
// the person presses Start.
import { useCallback, useEffect, useState } from 'react';
import { pageText } from '@/next/bindings/outcome';
import {
  addBot,
  editBot,
  isRefusal,
  linkShared,
  loadBots,
  loadShared,
  removeBot,
  saveSharedInstruction,
  startBinding,
  stopBinding,
  testBot,
  testShared,
  unlinkShared,
  type INotifyBot,
  type ISharedView,
  type TNotifyOutcome,
} from '@/next/features/notify-bots/api';
import NotifyBotsSection, { reasonText } from '@/next/features/settings/notify-bots-section';
import NotifySharedSection from '@/next/features/settings/notify-shared-section';
import type { MachineId } from '@/next/protocol/v2';
import { toast } from '@/next/ui/toast';

const BINDING_POLL_MS = 2000;

type TLoad = { k: 'loading' } | { k: 'failed'; text: string } | { k: 'ready'; bots: readonly INotifyBot[] };

/** An answer as the words of why it was not taken, or the value. */
const outcome = <T,>(res: TNotifyOutcome<T>): { value: T } | { error: string } => {
  if (!('ok' in res)) return { error: pageText(res) };
  if (isRefusal(res.value)) return { error: reasonText(res.value.refused) };
  return { value: res.value };
};

type TSharedLoad = { k: 'loading' } | { k: 'failed' } | { k: 'ready'; view: ISharedView };

export const NotifyBotsPage = ({ machine }: { machine: MachineId }) => {
  const [state, setState] = useState<TSharedLoad>({ k: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const waiting =
    state.k === 'ready' && state.view.shared.chat === null && state.view.shared.binding !== null;

  useEffect(() => {
    let live = true;
    void loadShared(machine).then((res) => {
      if (!live) return;
      const got = outcome(res);
      if ('value' in got) setState({ k: 'ready', view: got.value });
      else setState((now) => (now.k === 'ready' ? now : { k: 'failed' }));
    });
    return () => {
      live = false;
    };
  }, [machine, attempt]);

  useEffect(() => {
    if (!waiting) return;
    const timer = setTimeout(() => setAttempt((n) => n + 1), BINDING_POLL_MS);
    return () => clearTimeout(timer);
  }, [waiting, attempt]);

  /** Runs a change and keeps the state the machine answered with. */
  const change = async (call: Promise<TNotifyOutcome<ISharedView>>): Promise<string | null> => {
    const got = outcome(await call);
    if ('error' in got) return got.error;
    setState({ k: 'ready', view: got.value });
    return null;
  };

  // The machine could not say: the bots of the person's own, as before the shared bot.
  if (state.k === 'failed') return <OwnBotsPage machine={machine} />;
  if (state.k === 'ready' && !state.view.shared.enabled) return <OwnBotsPage machine={machine} />;
  return (
    <>
      <NotifySharedSection
        state={state.k === 'ready' ? state.view.shared : null}
        problem={state.k === 'ready' ? state.view.problem : ''}
        onLink={async () => {
          const got = outcome(await linkShared(machine));
          if ('error' in got) return { error: got.error };
          setState({ k: 'ready', view: got.value });
          const link = got.value.shared.binding?.link;
          return link ? { link } : { error: reasonText('failed') };
        }}
        onUnlink={() => change(unlinkShared(machine))}
        onSave={(instruction) => change(saveSharedInstruction(machine, instruction))}
        onTest={() => change(testShared(machine))}
      />
      {state.k === 'ready' && state.view.ownBots && <OwnBotsPage machine={machine} />}
    </>
  );
};

/** The bots of the person's own, linked by their tokens. */
const OwnBotsPage = ({ machine }: { machine: MachineId }) => {
  const [state, setState] = useState<TLoad>({ k: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const [open, setOpen] = useState<string | null>(null);
  const waiting = state.k === 'ready' && state.bots.some((bot) => bot.binding?.state === 'waiting');

  useEffect(() => {
    let live = true;
    void loadBots(machine).then((res) => {
      if (!live) return;
      const got = outcome(res);
      if ('value' in got) setState({ k: 'ready', bots: got.value });
      else setState((now) => (now.k === 'ready' ? now : { k: 'failed', text: got.error }));
    });
    return () => {
      live = false;
    };
  }, [machine, attempt]);

  // While a binding waits, the list is read again until the chat is there (or the wait ends).
  useEffect(() => {
    if (!waiting) return;
    const timer = setTimeout(() => setAttempt((n) => n + 1), BINDING_POLL_MS);
    return () => clearTimeout(timer);
  }, [waiting, attempt]);

  const retry = useCallback(() => {
    setState({ k: 'loading' });
    setAttempt((n) => n + 1);
  }, []);

  const put = (bot: INotifyBot) =>
    setState((now) => {
      if (now.k !== 'ready') return now;
      const has = now.bots.some((b) => b.id === bot.id);
      return {
        k: 'ready',
        bots: has ? now.bots.map((b) => (b.id === bot.id ? bot : b)) : [...now.bots, bot],
      };
    });

  /** Runs a change of one bot and keeps the bot the machine answered with. */
  const change = async (call: Promise<TNotifyOutcome<INotifyBot>>): Promise<string | null> => {
    const got = outcome(await call);
    if ('error' in got) return got.error;
    put(got.value);
    return null;
  };

  return (
    <NotifyBotsSection
      bots={state.k === 'ready' ? state.bots : null}
      failed={state.k === 'failed' ? { reason: state.text, onRetry: retry } : null}
      open={open}
      onOpen={setOpen}
      onAdd={async (token) => {
        const got = outcome(await addBot(machine, token));
        if ('error' in got) return got.error;
        put(got.value);
        setOpen(got.value.id);
        // A new bot has no chat: its binding starts at once, the link is the next thing the person needs. The bot is
        // linked either way; a binding that did not start says why and the card keeps its «Привязать чат».
        const refused = await change(startBinding(machine, got.value.id));
        if (refused !== null) toast(refused);
        return null;
      }}
      onSave={(id, edit) => change(editBot(machine, id, edit))}
      onToken={(id, token) => change(editBot(machine, id, { token }))}
      onBind={(id) => change(startBinding(machine, id))}
      onStopBind={(id) => change(stopBinding(machine, id))}
      onTest={(id) => change(testBot(machine, id))}
      onRemove={async (id) => {
        const got = outcome(await removeBot(machine, id));
        if ('error' in got) return got.error;
        setState((now) =>
          now.k === 'ready' ? { k: 'ready', bots: now.bots.filter((b) => b.id !== id) } : now,
        );
        setOpen((now) => (now === id ? null : now));
        return null;
      }}
    />
  );
};
