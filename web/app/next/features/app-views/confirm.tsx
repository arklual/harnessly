// hl.ui.confirm(text): the host's own question, titled with the app's real name, so a view cannot dress it as anything
// else. One question at a time, the rest wait their turn; closing the dialog any way but «ОК» is «no».
import { useSyncExternalStore } from 'react';
import Modal from '@/next/features/shell/modal';
import { Button } from '@/next/ui/button';
import { usePresence } from '@/next/ui/presence';
import { t } from '@/next/i18n';
import '@/next/styles/app-main.css';

interface IQuestion {
  appName: string;
  text: string;
  done: (ok: boolean) => void;
}

const queue: IQuestion[] = [];
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());
let head: IQuestion | null = null;

const next = () => {
  head = queue.shift() ?? null;
  emit();
};

/** Asks the person; resolves true only for «ОК». */
export const askConfirm = (appName: string, text: string): Promise<boolean> =>
  new Promise((resolve) => {
    queue.push({ appName, text, done: resolve });
    if (head === null) next();
  });

/** Answers the question on screen (the dialog, or a test). */
export const answerConfirm = (ok: boolean) => {
  const q = head;
  if (!q) return;
  q.done(ok);
  next();
};

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => listeners.delete(l);
};

/** The dialog's exit, as long as .hx-modal's. */
const EXIT_MS = 260;

/** Mounted once in the shell. */
export const AppConfirmHost = () => {
  const question = useSyncExternalStore(subscribe, () => head);
  const shown = usePresence(question, EXIT_MS);
  const q = shown.value;
  if (!q) return null;
  return (
    <Modal
      open={shown.state === 'open'}
      onClose={() => answerConfirm(false)}
      title={q.appName}
      lead={q.text}
      boxClass="hx-appconfirm"
    >
      <div className="hx-appconfirm__actions">
        <Button variant="ghost" onClick={() => answerConfirm(false)}>
          {t('next.appview.confirmNo')}
        </Button>
        <Button variant="primary" onClick={() => answerConfirm(true)}>
          {t('next.appview.confirmYes')}
        </Button>
      </div>
    </Modal>
  );
};
