import { useSyncExternalStore } from 'react';
import {
  MAX_EXCERPTS,
  MAX_HISTORY_TURNS,
  type ISideChatExcerpt,
  type ISideChatTurn,
  type TSideChatEvent,
  type TSideChatRole,
} from '@/lib/hl-side-chat';
import { streamSideChat, type TSideChatBody } from '@/lib/hl-side-chat-client';
import { t } from '@/lib/i18n';

export type TSideChatStreamer = (
  body: TSideChatBody,
  signal: AbortSignal,
  onEvent: (event: TSideChatEvent) => void,
) => Promise<void>;

export type TSideChatStatus = 'streaming' | 'done' | 'error' | 'stopped';

export interface ISideChatExcerptChip {
  id: string;
  quote: string;
  source: { entryId: string; role: TSideChatRole };
}

export interface ISideChatMessage {
  id: string;
  question: string;
  excerpts: ISideChatExcerptChip[];
  answer: string;
  status: TSideChatStatus;
  error?: string;
}

export interface ISideChatState {
  messages: readonly ISideChatMessage[];
  excerpts: readonly ISideChatExcerptChip[];
}

export const OPEN_PANE_TAB_EVENT = 'hl:open-pane-tab';

const EMPTY: ISideChatState = { messages: [], excerpts: [] };
const INTERRUPTED = t('lib.hlSideChatStore.interrupted');

const toExcerpt = ({ quote, source }: ISideChatExcerptChip): ISideChatExcerpt => ({ quote, source });

/**
 * What a side chat shows for a conversation tab, outside React and only in memory: it goes with the page, like
 * the chats «that disappear when you close the app». The excerpts wait above the field until a question takes them along.
 */
export const createSideChatStore = (stream: TSideChatStreamer = streamSideChat) => {
  const byTab = new Map<string, ISideChatState>();
  const running = new Map<string, { controller: AbortController; messageId: string }>();
  const listeners = new Set<() => void>();
  let ids = 0;

  const get = (tab: string): ISideChatState => byTab.get(tab) ?? EMPTY;
  const set = (tab: string, state: ISideChatState) => {
    if (state.messages.length === 0 && state.excerpts.length === 0) byTab.delete(tab);
    else byTab.set(tab, state);
    for (const listener of listeners) listener();
  };
  const patch = (tab: string, id: string, change: Partial<ISideChatMessage>) => {
    const state = get(tab);
    set(tab, {
      ...state,
      messages: state.messages.map((message) => (message.id === id ? { ...message, ...change } : message)),
    });
  };
  const append = (tab: string, id: string, text: string) => {
    const message = get(tab).messages.find((candidate) => candidate.id === id);
    if (message) patch(tab, id, { answer: message.answer + text });
  };

  const historyBefore = (tab: string, id: string): ISideChatTurn[] => {
    const { messages } = get(tab);
    const before = messages.slice(
      0,
      messages.findIndex((message) => message.id === id),
    );
    return before
      .filter((message) => message.status === 'done')
      .slice(-MAX_HISTORY_TURNS)
      .map(({ question, answer }) => ({ question, answer }));
  };

  const start = (tab: string, id: string) => {
    const message = get(tab).messages.find((candidate) => candidate.id === id);
    if (!message) return;
    const controller = new AbortController();
    const run = { controller, messageId: id };
    running.set(tab, run);
    const current = () => running.get(tab) === run;
    const onEvent = (event: TSideChatEvent) => {
      if (!current()) return;
      if (event.type === 'delta') {
        append(tab, id, event.text);
        return;
      }
      running.delete(tab);
      if (event.type === 'done')
        patch(tab, id, {
          status: 'done',
          answer: event.text || get(tab).messages.find((m) => m.id === id)?.answer || '',
        });
      else
        patch(
          tab,
          id,
          event.code === 'cancelled' ? { status: 'stopped' } : { status: 'error', error: event.message },
        );
    };
    const body: TSideChatBody = {
      tabId: tab,
      question: message.question,
      excerpts: message.excerpts.map(toExcerpt),
      history: historyBefore(tab, id),
    };
    void stream(body, controller.signal, onEvent).then(() => {
      if (!current()) return;
      running.delete(tab);
      const left = get(tab).messages.find((candidate) => candidate.id === id);
      if (left?.status === 'streaming') patch(tab, id, { status: 'error', error: INTERRUPTED });
    });
  };

  const stopRun = (tab: string) => {
    running.get(tab)?.controller.abort();
    running.delete(tab);
  };

  return {
    get,
    isBusy: (tab: string): boolean => running.has(tab),
    addExcerpt: (
      tab: string,
      { quote, source }: { quote: string; source: ISideChatExcerptChip['source'] },
    ): void => {
      const state = get(tab);
      if (
        state.excerpts.some((excerpt) => excerpt.quote === quote && excerpt.source.entryId === source.entryId)
      )
        return;
      const excerpts = [...state.excerpts, { id: `x${++ids}`, quote, source }].slice(-MAX_EXCERPTS);
      set(tab, { ...state, excerpts });
    },
    removeExcerpt: (tab: string, id: string): void => {
      const state = get(tab);
      set(tab, { ...state, excerpts: state.excerpts.filter((excerpt) => excerpt.id !== id) });
    },
    ask: (tab: string, text: string): boolean => {
      const question = text.trim();
      if (!question || running.has(tab)) return false;
      const state = get(tab);
      const message: ISideChatMessage = {
        id: `m${++ids}`,
        question,
        excerpts: [...state.excerpts],
        answer: '',
        status: 'streaming',
      };
      set(tab, { messages: [...state.messages, message], excerpts: [] });
      start(tab, message.id);
      return true;
    },
    retry: (tab: string, id: string): boolean => {
      const message = get(tab).messages.find((candidate) => candidate.id === id);
      if (!message || running.has(tab) || (message.status !== 'error' && message.status !== 'stopped'))
        return false;
      patch(tab, id, { status: 'streaming', answer: '', error: undefined });
      start(tab, id);
      return true;
    },
    cancel: (tab: string): void => {
      const run = running.get(tab);
      if (!run) return;
      stopRun(tab);
      patch(tab, run.messageId, { status: 'stopped' });
    },
    clear: (tab: string): void => {
      stopRun(tab);
      set(tab, EMPTY);
    },
    subscribe: (listener: () => void): (() => void) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
};

const store = createSideChatStore();

/** The fragment selected in the timeline goes above the field of the side chat of that tab. */
export const addSideChatExcerpt = (
  tabId: string,
  excerpt: { quote: string; source: ISideChatExcerptChip['source'] },
): void => store.addExcerpt(tabId, excerpt);
export const removeSideChatExcerpt = (tabId: string, id: string): void => store.removeExcerpt(tabId, id);
export const askSideChat = (tabId: string, question: string): boolean => store.ask(tabId, question);
export const retrySideChat = (tabId: string, id: string): boolean => store.retry(tabId, id);
export const cancelSideChat = (tabId: string): void => store.cancel(tabId);
export const clearSideChat = (tabId: string): void => store.clear(tabId);

export const useSideChat = (tabId: string | null): ISideChatState =>
  useSyncExternalStore(
    store.subscribe,
    () => (tabId ? store.get(tabId) : EMPTY),
    () => EMPTY,
  );
