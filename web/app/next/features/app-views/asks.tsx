// app.ask(question, options): the agent asks the person something inside the app. The runtime holds the ask; the page
// reads the chat's pending asks while an app of the chat is on screen and shows each as the host's own dialog, titled
// with the app's real name, then sends the pick back. Closing the dialog is «cancelled».
import { useCallback, useEffect, useState } from 'react';
import { answerAppAsk, loadAppAsks, type IAppAsk } from '@/next/features/app-views/api';
import Modal from '@/next/features/shell/modal';
import { Button } from '@/next/ui/button';
import { usePresence } from '@/next/ui/presence';
import { toast } from '@/next/ui/toast';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import '@/next/styles/app-main.css';

/** How often the chat's asks are read while an app of it is on screen. */
export const ASK_POLL_MS = 1500;
const EXIT_MS = 260;

/** The oldest ask nobody has answered here yet. */
export const firstAsk = (asks: readonly IAppAsk[], answered: ReadonlySet<string>): IAppAsk | null =>
  [...asks].sort((a, b) => a.createdAt - b.createdAt).find((a) => !answered.has(a.id)) ?? null;

const useAppAsks = (machine: MachineId, conversation: ConvId | null) => {
  const [asks, setAsks] = useState<readonly IAppAsk[]>([]);
  // Asks answered here and not yet gone from the runtime's list: not shown twice.
  const [answered, setAnswered] = useState<ReadonlySet<string>>(new Set());

  useEffect(() => {
    if (conversation === null) return;
    let live = true;
    let timer: number | undefined;
    const read = () => {
      if (document.hidden) {
        timer = window.setTimeout(read, ASK_POLL_MS);
        return;
      }
      loadAppAsks(machine, conversation).then(
        (list) => {
          if (!live) return;
          setAsks(list);
          const ids = new Set(list.map((a) => a.id));
          setAnswered((have) => {
            const kept = [...have].filter((id) => ids.has(id));
            return kept.length === have.size ? have : new Set(kept);
          });
          timer = window.setTimeout(read, ASK_POLL_MS);
        },
        () => {
          // A stumble of the link: the next read tells.
          if (live) timer = window.setTimeout(read, ASK_POLL_MS * 2);
        },
      );
    };
    read();
    return () => {
      live = false;
      window.clearTimeout(timer);
    };
  }, [machine, conversation]);

  const answer = useCallback(
    async (ask: IAppAsk, pick: string | null) => {
      try {
        await answerAppAsk(machine, ask.id, pick);
        setAnswered((have) => new Set(have).add(ask.id));
      } catch (e) {
        toast(t('next.appview.askFailed'), {
          tone: 'error',
          description: e instanceof Error ? e.message : undefined,
        });
      }
    },
    [machine],
  );
  return { asks, answered, answer };
};

/** Mounted where an app of the chat is on screen. */
const AppAsks = ({ machine, conversation }: { machine: MachineId; conversation: ConvId | null }) => {
  const { asks, answered, answer } = useAppAsks(machine, conversation);
  const ask = firstAsk(asks, answered);
  const shown = usePresence(ask, EXIT_MS);
  const [busy, setBusy] = useState<string | null>(null);
  const a = shown.value;
  if (!a) return null;
  const pick = async (choice: string | null) => {
    setBusy(choice ?? '');
    await answer(a, choice);
    setBusy(null);
  };
  return (
    <Modal
      open={shown.state === 'open'}
      onClose={() => void pick(null)}
      title={a.appName}
      lead={a.question}
      boxClass="hx-appconfirm"
    >
      <div className="hx-appconfirm__actions" data-choices="true">
        {a.options.map((option) => (
          <Button
            key={option}
            busy={busy === option}
            disabled={busy !== null}
            onClick={() => void pick(option)}
          >
            {option}
          </Button>
        ))}
      </div>
    </Modal>
  );
};

export default AppAsks;
