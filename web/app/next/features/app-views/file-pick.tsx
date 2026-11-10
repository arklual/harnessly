// hl.files.pick (HRN-850): the person picks a file of the chat's folder in the host's own dialog, titled with the app's
// real name; the view gets that one file's path and text, once, with no mask. Closing the dialog any way but a pick is
// null. One request at a time, the rest wait their turn.
import { useEffect, useState, useSyncExternalStore } from 'react';
import Modal from '@/next/features/shell/modal';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';
import { findFiles, readFile } from '@/next/features/pane/chat-files-api';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { conversationsStore } from '@/next/stores/conversations';
import { t } from '@/next/i18n';
import '@/next/styles/app-main.css';

export interface IPicked {
  path: string;
  text: string;
}

interface IRequest {
  appName: string;
  machine: MachineId;
  conv: ConvId;
  /** Endings the file may have (.md, .json); empty is any. */
  accept: readonly string[];
  done: (file: IPicked | null) => void;
}

const queue: IRequest[] = [];
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());
let head: IRequest | null = null;

const next = () => {
  head = queue.shift() ?? null;
  emit();
};

/** Asks the person for a file of the chat's folder; null when they close the dialog. */
export const askFile = (
  appName: string,
  machine: MachineId,
  conv: ConvId,
  accept: readonly string[],
): Promise<IPicked | null> =>
  new Promise((resolve) => {
    queue.push({ appName, machine, conv, accept, done: resolve });
    if (head === null) next();
  });

/** Ends the request on screen with a file or with nothing. */
export const answerFile = (file: IPicked | null) => {
  const r = head;
  if (!r) return;
  r.done(file);
  next();
};

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => listeners.delete(l);
};

/** Whether a path has one of the endings asked for. */
export const accepts = (accept: readonly string[], path: string): boolean =>
  accept.length === 0 || accept.some((a) => path.toLowerCase().endsWith(a.toLowerCase()));

/** The dialog's exit, as long as .hx-modal's. */
const EXIT_MS = 260;

const Picker = ({ r }: { r: IRequest }) => {
  const [query, setQuery] = useState('');
  const [found, setFound] = useState<string[] | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const cwd = conversationsStore.get(r.conv)?.cwd ?? '';
  useEffect(() => {
    const ctl = new AbortController();
    const wait = window.setTimeout(() => {
      findFiles(r.machine, cwd, query, ctl.signal)
        .then((files) => setFound(files.filter((f) => accepts(r.accept, f))))
        .catch(() => {
          if (!ctl.signal.aborted) setFound([]);
        });
    }, 150);
    return () => {
      window.clearTimeout(wait);
      ctl.abort();
    };
  }, [r, cwd, query]);
  const pick = async (path: string) => {
    setBusy(true);
    setFailure(null);
    try {
      const got = await readFile(r.machine, r.conv, path);
      answerFile({ path, text: got.content });
    } catch (e) {
      setFailure(e instanceof Error && e.message !== '' ? e.message : t('next.appview.pickFailed'));
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <input
        className="hx-filepick__field"
        value={query}
        placeholder={t('next.appview.pickSearch')}
        onChange={(e) => setQuery(e.target.value)}
        autoFocus
      />
      <ul className="hx-filepick__list">
        {(found ?? []).map((f) => (
          <li key={f}>
            <button type="button" className="hx-filepick__item" disabled={busy} onClick={() => void pick(f)}>
              <Icon name="file" size="sm" />
              <span>{f}</span>
            </button>
          </li>
        ))}
        {found !== null && found.length === 0 && (
          <li className="hx-filepick__none">{t('next.appview.pickNone')}</li>
        )}
      </ul>
      {failure && (
        <p className="hx-filepick__fail" role="alert">
          {failure}
        </p>
      )}
    </>
  );
};

/** Mounted once in the shell. */
export const AppFilePickHost = () => {
  const request = useSyncExternalStore(subscribe, () => head);
  const shown = usePresence(request, EXIT_MS);
  const r = shown.value;
  if (!r) return null;
  return (
    <Modal
      open={shown.state === 'open'}
      onClose={() => answerFile(null)}
      title={t('next.appview.pickTitle', { app: r.appName })}
      lead={t('next.appview.pickLead')}
      boxClass="hx-appconfirm hx-filepick"
    >
      <Picker r={r} />
    </Modal>
  );
};
