// What a new chat is: «Задача» — an agent that works on the machine, or «Разговор» — a plain talk with a
// model (the ChatGPT chat). A small segment over the field; Shift+Tab in an empty field turns it, as the mode
// key of the agents' own terminals. The choice is remembered for the next new chat (this browser only: a convenience).
import { useEffect, useRef, useSyncExternalStore, type RefObject } from 'react';
import { t } from '@/next/i18n';
import '@/next/styles/kind.css';

export const CHAT_KIND = { task: 'task', talk: 'talk' } as const;
export type ChatKind = (typeof CHAT_KIND)[keyof typeof CHAT_KIND];

const KEY = 'hx-new-chat-kind';
const listeners = new Set<() => void>();

const read = (): ChatKind => {
  try {
    return localStorage.getItem(KEY) === CHAT_KIND.talk ? CHAT_KIND.talk : CHAT_KIND.task;
  } catch {
    return CHAT_KIND.task;
  }
};

let current: ChatKind | null = null;
const get = (): ChatKind => {
  current ??= read();
  return current;
};

export const setChatKind = (kind: ChatKind): void => {
  current = kind;
  try {
    localStorage.setItem(KEY, kind);
  } catch {
    // A private window keeps the choice for this page only.
  }
  listeners.forEach((l) => l());
};

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
};

export const useChatKind = (): ChatKind => useSyncExternalStore(subscribe, get, () => CHAT_KIND.task);

const other = (kind: ChatKind): ChatKind => (kind === CHAT_KIND.task ? CHAT_KIND.talk : CHAT_KIND.task);

/** Shift+Tab in the empty field turns the kind; with words in it the key moves the focus as usual. The field is the
 *  window's one (one-field.tsx) and moves between screens, so the key is read on the document for that node. */
export const useKindKey = (
  field: RefObject<HTMLTextAreaElement | null>,
  kind: ChatKind,
  onKind: (k: ChatKind) => void,
): void => {
  const latest = useRef({ kind, onKind });
  useEffect(() => {
    latest.current = { kind, onKind };
  });
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Tab' || !event.shiftKey || event.altKey || event.ctrlKey || event.metaKey) return;
      const el = field.current;
      if (!el || event.target !== el || el.value.trim() !== '') return;
      event.preventDefault();
      latest.current.onKind(other(latest.current.kind));
    };
    document.addEventListener('keydown', onKey, true);
    return () => document.removeEventListener('keydown', onKey, true);
  }, [field]);
};

const ORDER: readonly ChatKind[] = [CHAT_KIND.task, CHAT_KIND.talk];

interface IKindSwitchProps {
  value: ChatKind;
  onChange: (kind: ChatKind) => void;
}

const KindSwitch = ({ value, onChange }: IKindSwitchProps) => (
  <div
    className="hx-kind"
    role="radiogroup"
    aria-label={t('next.kind.aria')}
    title={t('next.kind.keyHint')}
    data-at={value}
  >
    {ORDER.map((kind) => (
      <button
        key={kind}
        type="button"
        role="radio"
        aria-checked={value === kind}
        className="hx-kind__opt"
        onClick={() => onChange(kind)}
      >
        {t(`next.kind.${kind}`)}
      </button>
    ))}
  </div>
);

export default KindSwitch;
