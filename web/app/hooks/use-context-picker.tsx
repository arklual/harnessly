import { useEffect, useId, useState, type KeyboardEvent } from 'react';
import { File as FileIcon, GitPullRequest, MessageSquare, Ticket } from 'lucide-react';
import { LISTBOX_ACTION, useTwListboxNav, type ITwOption } from '@/components/agents/ui';
import { appFetch } from '@/lib/app-path';
import { jsonRequest } from '@/lib/conversation-loading';
import type { IIssueMeta, TMrState } from '@/lib/link-meta';
import { parseAtQuery, removeAtMention } from '@/lib/hl-context-query';
import type { TContextChip } from '@/lib/hl-context-chips';
import {
  agentConversationKey,
  cleanConversationTitle,
  type AgentConversation,
  type Conversation,
} from '@/lib/hl-conversations';
import { conversationUrl } from '@/lib/hl-conversation-url';
import type { IGitLabMr } from '@/types/server/hl-gitlab';
import useTranscriptSearch, { type ITranscriptSearchHit } from '@/hooks/use-transcript-search';
import { t } from '@/lib/i18n';

const DEBOUNCE_MS = 200;
const TASK_KEY_RE = /^[A-Za-z][A-Za-z0-9]{0,9}-\d{1,7}$/;
const SEARCH_MIN_LENGTH = 2;
const GROUP_LIMIT = 6;
const RECENT_LIMIT = 8;
const MR_STATES: readonly TMrState[] = ['opened', 'merged', 'closed', 'locked'];

const newChipId = (): string =>
  typeof crypto.randomUUID === 'function' ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`;

const conversationLink = (key: string): string | null =>
  typeof window === 'undefined' ? null : conversationUrl(window.location.origin, key);

const stripSnippetMarkers = (snippet: string) => snippet.replace(/[\u0001\u0002]/g, '');

interface IGitLabIsland {
  mrs: IGitLabMr[];
}

export interface IUseContextPickerOptions {
  /** Composer text, unchanged by this hook except when a mention is picked. */
  value: string;
  setValue: (value: string) => void;
  /** Folder the agent works in: files are searched under it, its repo's MRs are offered. */
  cwd: string | null;
  /** Welcome composer already has the recent list loaded; the conversation composer fetches its own. */
  recentConversations?: Conversation[];
}

export interface IUseContextPicker {
  listId: string;
  open: boolean;
  options: ITwOption[] | null;
  activeValue: string | null;
  setActiveValue: (value: string | null) => void;
  activeDescendant: string | undefined;
  chips: TContextChip[];
  removeChip: (id: string) => void;
  clearChips: () => void;
  handleKeyDown: (event: KeyboardEvent<HTMLElement>) => boolean;
  pick: (optionValue: string) => void;
  close: () => void;
}

/**
 * The «@»-context popover: typing `@word` (same end-of-text convention as `/` and `$skill`,
 * see parseCommandQuery) opens a list of conversations, tasks, merge requests and files; picking one
 * removes the typed text and attaches a chip instead. Chips expand into a compact block appended to the
 * prompt on send (see buildContextBlock) — the caller owns that step, this hook only owns the picker.
 */
const useContextPicker = ({
  value,
  setValue,
  cwd,
  recentConversations,
}: IUseContextPickerOptions): IUseContextPicker => {
  const listId = useId();
  const [dismissedAt, setDismissedAt] = useState<string | null>(null);
  const [chips, setChips] = useState<TContextChip[]>([]);
  const [taskHit, setTaskHit] = useState<{ key: string; meta: IIssueMeta } | null>(null);
  const [island, setIsland] = useState<IGitLabIsland | null>(null);
  const [fileHits, setFileHits] = useState<string[]>([]);
  const [recent, setRecent] = useState<Conversation[] | null>(null);

  const query = parseAtQuery(value);
  const open = query !== null && dismissedAt !== value;
  const q = query?.query.trim() ?? '';
  const conversationList = recentConversations ?? recent ?? [];

  const search = useTranscriptSearch(open && q.length >= SEARCH_MIN_LENGTH ? q : '');

  useEffect(() => {
    // Stale hits stay in state but unused: taskItems below only reads one that still matches `q`.
    if (!open || !TASK_KEY_RE.test(q)) return;
    let cancelled = false;
    const timer = setTimeout(() => {
      appFetch(`/api/platform/link-meta?key=${encodeURIComponent(q)}`)
        .then((res) => (res.ok ? (res.json() as Promise<IIssueMeta>) : null))
        .then((meta) => {
          if (!cancelled) setTaskHit(meta ? { key: meta.key, meta } : null);
        })
        .catch(() => {
          if (!cancelled) setTaskHit(null);
        });
    }, DEBOUNCE_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [open, q]);

  useEffect(() => {
    if (!open || !cwd || island) return;
    let cancelled = false;
    appFetch('/api/platform/gitlab', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ paths: [cwd], jsonlPath: null, mrs: [] }),
    })
      .then((res) => (res.ok ? (res.json() as Promise<IGitLabIsland>) : null))
      .then((body) => {
        if (!cancelled && body) setIsland(body);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [open, cwd, island]);

  useEffect(() => {
    // Stale hits stay in state but unused: fileItems below is only read while `cwd` is set.
    if (!open || !cwd) return;
    let cancelled = false;
    const timer = setTimeout(() => {
      appFetch(`/api/platform/context-files?cwd=${encodeURIComponent(cwd)}&q=${encodeURIComponent(q)}`)
        .then((res) => (res.ok ? (res.json() as Promise<{ files: string[] }>) : null))
        .then((body) => {
          if (!cancelled) setFileHits(body?.files ?? []);
        })
        .catch(() => {
          if (!cancelled) setFileHits([]);
        });
    }, DEBOUNCE_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [open, cwd, q]);

  useEffect(() => {
    if (!open || recentConversations || recent !== null || q.length > 0) return;
    let cancelled = false;
    jsonRequest<{ conversations: AgentConversation[] }>('/agents/api/platform/conversations')
      .then((body) => {
        if (cancelled) return;
        const items = body.conversations
          .map((agent) => ({
            ...agent,
            key: agentConversationKey(agent.id, agent.machineId),
            kind: 'agent' as const,
          }))
          .sort((a, b) => b.updatedAt - a.updatedAt)
          .slice(0, RECENT_LIMIT);
        setRecent(items);
      })
      .catch(() => {
        if (!cancelled) setRecent([]);
      });
    return () => {
      cancelled = true;
    };
  }, [open, recentConversations, recent, q]);

  const conversationItems: ITwOption[] =
    q.length >= SEARCH_MIN_LENGTH
      ? search.hits.slice(0, GROUP_LIMIT).map((hit) => ({
          value: `conv-hit:${hit.fileId}:${hit.byteOffset}`,
          label: hit.aiTitle || hit.path.split('/').pop() || t('hooks.useContextPicker.conversation'),
          description: stripSnippetMarkers(hit.snippet),
          icon: <MessageSquare size={14} />,
        }))
      : conversationList.slice(0, GROUP_LIMIT).map((item) => ({
          value: `conv-recent:${item.key}`,
          label: cleanConversationTitle(item.title),
          icon: <MessageSquare size={14} />,
        }));

  const taskItems: ITwOption[] =
    taskHit && taskHit.key.toLowerCase() === q.toLowerCase()
      ? [
          {
            value: `task:${taskHit.key}`,
            label: taskHit.key,
            description: [taskHit.meta.title, taskHit.meta.state].filter(Boolean).join(' · '),
            icon: <Ticket size={14} />,
          },
        ]
      : [];

  const mrItems: ITwOption[] = (island?.mrs ?? [])
    .filter((mr) => !q || `${mr.project} !${mr.iid} ${mr.title}`.toLowerCase().includes(q.toLowerCase()))
    .slice(0, GROUP_LIMIT)
    .map((mr) => ({
      value: `mr:${mr.host}/${mr.project}!${mr.iid}`,
      label: `${mr.project.split('/').at(-1)} !${mr.iid}`,
      description: mr.title,
      icon: <GitPullRequest size={14} />,
    }));

  const fileItems: ITwOption[] = (cwd ? fileHits : []).map((path) => ({
    value: `file:${path}`,
    label: path.split('/').at(-1) ?? path,
    description: path,
    icon: <FileIcon size={14} />,
  }));

  const groups: { label: string; items: ITwOption[] }[] = [
    { label: t('hooks.useContextPicker.conversations'), items: conversationItems },
    { label: t('hooks.useContextPicker.tasks'), items: taskItems },
    { label: 'MR', items: mrItems },
    { label: t('hooks.useContextPicker.files'), items: fileItems },
  ];
  const options: ITwOption[] | null = open
    ? groups.flatMap((group) =>
        group.items.length
          ? [{ value: `header:${group.label}`, label: group.label, disabled: true }, ...group.items]
          : [],
      )
    : null;

  const nav = useTwListboxNav({ listId, options });

  const close = () => setDismissedAt(value);

  const pick = (optionValue: string) => {
    if (!query) return;
    const sep = optionValue.indexOf(':');
    const kind = optionValue.slice(0, sep);
    const rest = optionValue.slice(sep + 1);
    const chip = buildChip(kind, rest, { search, conversationList, taskHit, island });
    if (!chip) return;
    setChips((current) => [...current, chip]);
    setValue(removeAtMention(value, query));
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLElement>): boolean => {
    if (!open || event.nativeEvent.isComposing) return false;
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      close();
      return true;
    }
    const result = nav.handleKeyDown(event);
    if (!result) return event.defaultPrevented;
    if (result.type === LISTBOX_ACTION.dismiss) {
      close();
      return true;
    }
    pick(result.value);
    return true;
  };

  return {
    listId,
    open,
    options,
    activeValue: nav.activeValue,
    setActiveValue: nav.setActiveValue,
    activeDescendant: open ? nav.activeDescendant : undefined,
    chips,
    removeChip: (id) => setChips((current) => current.filter((chip) => chip.id !== id)),
    clearChips: () => setChips([]),
    handleKeyDown,
    pick,
    close,
  };
};

const buildChip = (
  kind: string,
  rest: string,
  ctx: {
    search: { hits: ITranscriptSearchHit[] };
    conversationList: Conversation[];
    taskHit: { key: string; meta: IIssueMeta } | null;
    island: IGitLabIsland | null;
  },
): TContextChip | null => {
  if (kind === 'conv-hit') {
    const hit = ctx.search.hits.find((h) => `${h.fileId}:${h.byteOffset}` === rest);
    if (!hit) return null;
    const title = hit.aiTitle || hit.path.split('/').pop() || t('hooks.useContextPicker.conversation');
    const matched = ctx.conversationList.find(
      (c) => c.machineId === hit.machineId && c.jsonlPath === hit.path,
    );
    return {
      id: newChipId(),
      kind: 'conversation',
      label: title,
      data: {
        title,
        url: matched ? conversationLink(matched.key) : null,
        excerpts: [{ role: hit.role, text: stripSnippetMarkers(hit.snippet) }],
      },
    };
  }
  if (kind === 'conv-recent') {
    const item = ctx.conversationList.find((c) => c.key === rest);
    if (!item) return null;
    const title = cleanConversationTitle(item.title);
    return {
      id: newChipId(),
      kind: 'conversation',
      label: title,
      data: { title, url: conversationLink(item.key), excerpts: [] },
    };
  }
  if (kind === 'task') {
    if (!ctx.taskHit || ctx.taskHit.key !== rest) return null;
    return {
      id: newChipId(),
      kind: 'task',
      label: ctx.taskHit.key,
      data: { key: ctx.taskHit.key, title: ctx.taskHit.meta.title, state: ctx.taskHit.meta.state },
    };
  }
  if (kind === 'mr') {
    const mr = (ctx.island?.mrs ?? []).find((m) => `${m.host}/${m.project}!${m.iid}` === rest);
    if (!mr) return null;
    const state = (MR_STATES as readonly string[]).includes(mr.state) ? (mr.state as TMrState) : 'opened';
    return {
      id: newChipId(),
      kind: 'mr',
      label: `!${mr.iid}`,
      data: { host: mr.host, project: mr.project, iid: mr.iid, title: mr.title, state },
    };
  }
  if (kind === 'file') {
    return { id: newChipId(), kind: 'file', label: rest.split('/').at(-1) ?? rest, data: { path: rest } };
  }
  return null;
};

export default useContextPicker;
