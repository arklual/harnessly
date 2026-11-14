// The search of everything (⌘K) as the shell hands it to the dialog: the chats and their messages, the
// settings (changed in the row), the designs, the team's documents, the apps and what the connected systems find, in
// one list; and, for a question in words, the answer of the model picked for «Ответы в поиске» with its sources.
// The sources are read when the dialog opens, so a page that never searches asks for nothing.
import { useCallback, useEffect, useMemo, useState } from 'react';
import { detailSource } from '@/next/bindings/search-detail';
import { useMessageSearch } from '@/next/bindings/use-message-search';
import { CONN_STATE, loadConnections, type IConn } from '@/next/features/connections/api';
import { fetchLibrary } from '@/next/features/designly/api';
import type { IDetailSource, ISearchDetail } from '@/next/features/search/detail';
import { messageGroups, recentChats, searchChats } from '@/next/features/search/search-chats';
import {
  ANSWER,
  type ISearchGroup,
  type ISearchScope,
  type TSearchAnswer,
} from '@/next/features/search/search-dialog';
import {
  SCOPE,
  connScope,
  connTabs,
  countIn,
  designsGroup,
  docsGroup,
  foundGroup,
  idleGroups,
  inScope,
  scopeEmpty,
  settingsGroup,
  type IScopeEmpty,
} from '@/next/features/search/smart-rows';
import {
  ANSWER_EVENT,
  ANSWER_NO_MODEL,
  FOUND_KIND,
  askSearchStream,
  listConnection,
  loadCatalogDocs,
  loadSettingsCatalog,
  searchConnection,
  withoutMarks,
  type IAnswerSource,
  type ICatalogDoc,
  type ICatalogSetting,
  type IFound,
  type TFoundKind,
} from '@/next/features/search/sources-api';
import { designsOfHits, mergeDesigns, searchIndex } from '@/next/features/search/index-search';
import { PERSON_KEY, personPatch } from '@/next/features/search/page-settings';
import { isQuestion, termsOf, type ITerms } from '@/next/features/search/terms';
import { putSetting } from '@/next/features/settings/changes-api';
import { isRefused } from '@/next/features/settings/platform-answer';
import type { ISidebarChat, ISidebarProject } from '@/next/features/sidebar/types';
import { t } from '@/next/i18n';
import type { DesignArtifact, MachineId, MessageHit } from '@/next/protocol/v2';
import { patchSettings, useMe } from '@/next/stores/me';
import { toast } from '@/next/ui/toast';

/** A connected system is asked this long after the last key, from this many letters. */
const FOUND_DEBOUNCE_MS = 400;
const FOUND_FROM = 3;
/** The model is asked this long after the question stopped changing. */
const ANSWER_DEBOUNCE_MS = 700;
/** How many of the found rows the model is shown. */
const ANSWER_SOURCES = 10;
/** A question's keywords the messages are looked for by, each on its own. */
const ASK_KEYWORDS = 3;

const CAPABILITY_KIND: Readonly<Record<string, TFoundKind>> = {
  code: FOUND_KIND.mr,
  issues: FOUND_KIND.issue,
  pages: FOUND_KIND.page,
};

interface ISources {
  machine: MachineId;
  catalog: readonly ICatalogSetting[];
  docs: readonly ICatalogDoc[];
  designs: readonly DesignArtifact[];
  conns: readonly IConn[];
}

/** A source that did not come has nothing to find: the search goes on with the others. */
const valueOr = <T>(res: { ok: true; value: T | { refused: string } } | object, none: T): T =>
  'ok' in res && !isRefused(res.value) ? (res.value as T) : none;

const readSources = async (machine: MachineId): Promise<ISources> => {
  const [catalog, docs, designs, conns] = await Promise.all([
    loadSettingsCatalog(machine),
    loadCatalogDocs(machine),
    fetchLibrary(machine).then(
      (library) => library.designs,
      () => [],
    ),
    loadConnections(machine),
  ]);
  return {
    machine,
    catalog: valueOr<readonly ICatalogSetting[]>(catalog, []),
    docs: valueOr<readonly ICatalogDoc[]>(docs, []),
    designs,
    conns: valueOr<readonly IConn[]>(conns, []).filter((conn) => conn.state === CONN_STATE.ok),
  };
};

interface IFoundState {
  key: string;
  byConn: Readonly<Record<string, readonly IFound[]>>;
}

interface IAnswerState {
  key: string;
  answer: TSearchAnswer;
}

/** What a connection's tab lists before anything is typed, for the machine and the connection it was asked of. */
interface IMineState {
  key: string;
  items: readonly IFound[];
}

const NO_TERMS: ITerms = { list: [], any: false };
/** A connection's own list, by what it can do: its open merge requests, the person's open issues. */
const MINE_KIND = [
  { capability: 'code', kind: FOUND_KIND.mr },
  { capability: 'issues', kind: FOUND_KIND.issue },
] as const;

export interface ISmartSearchInput {
  machine: MachineId | null;
  open: boolean;
  query: string;
  scope: string;
  /** null while the chat list is on its way. */
  chats: readonly ISidebarChat[] | null;
  projects: readonly ISidebarProject[];
  /** The apps' rows for the words, every app when nothing is typed in their own tab (features/search/app-rows.ts);
   *  undefined while the apps are on their way. */
  appsGroup: ISearchGroup | null | undefined;
  now: number;
}

export interface ISmartSearch {
  scopes: readonly ISearchScope[];
  /** null while the answer is on its way. */
  results: readonly ISearchGroup[] | null;
  /** What the tab shows before anything is typed; null while it is on its way. */
  idle: readonly ISearchGroup[] | null;
  /** What the tab says when it has nothing to list. */
  empty: IScopeEmpty;
  detail: IDetailSource;
  kept: Map<string, ISearchDetail>;
  indexing: boolean;
  /** The words lit in the rows. */
  marks: string;
  answer: TSearchAnswer | null;
  onAsk: () => void;
  onSetting: (key: string, value: string) => Promise<boolean>;
}

const mergeHits = (lists: readonly (readonly MessageHit[])[], keywords: readonly string[]): MessageHit[] => {
  const seen = new Map<string, MessageHit>();
  for (const hit of lists.flat()) seen.set(`${hit.convId}:${hit.entryId}`, hit);
  const score = (hit: MessageHit) => {
    const text = hit.snippet.toLowerCase();
    return keywords.filter((word) => text.includes(word)).length;
  };
  return [...seen.values()].sort((a, b) => score(b) - score(a) || b.at - a.at);
};

export const useSmartSearch = ({
  machine,
  open,
  query,
  scope,
  chats,
  projects,
  appsGroup,
  now,
}: ISmartSearchInput): ISmartSearch => {
  const [sources, setSources] = useState<ISources | null>(null);
  /** The words the person asked the model about by hand. */
  const [asked, setAsked] = useState<string | null>(null);
  const [found, setFound] = useState<IFoundState | null>(null);
  const [answered, setAnswered] = useState<IAnswerState | null>(null);
  const [mineOf, setMineOf] = useState<IMineState | null>(null);
  /** What the side asked about, kept while the search is open. */
  const [kept] = useState(() => new Map<string, ISearchDetail>());
  useEffect(() => {
    if (!open) kept.clear();
  }, [open, kept]);

  useEffect(() => {
    if (!open || machine === null) return;
    let stale = false;
    void readSources(machine).then((read) => {
      if (!stale) setSources(read);
    });
    return () => {
      stale = true;
    };
  }, [open, machine]);

  const mine = sources !== null && sources.machine === machine ? sources : null;
  const words = query.trim();
  // The database index: designs the library of a machine that is off or slow does not list; null = no index.
  const [indexed, setIndexed] = useState<{ key: string; designs: ReturnType<typeof designsOfHits> } | null>(
    null,
  );
  useEffect(() => {
    if (!open || machine === null || words === '') return;
    const ctl = new AbortController();
    const timer = setTimeout(() => {
      void searchIndex(machine, words, ctl.signal).then((hits) => {
        if (hits !== null && !ctl.signal.aborted)
          setIndexed({ key: `${machine}|${words}`, designs: designsOfHits(hits) });
      });
    }, 200);
    return () => {
      clearTimeout(timer);
      ctl.abort();
    };
  }, [open, machine, words]);
  const indexedDesigns = indexed !== null && indexed.key === `${machine}|${words}` ? indexed.designs : [];
  const ask = words !== '' && (isQuestion(words) || asked === words);
  const terms = useMemo(() => termsOf(words, ask), [words, ask]);
  const me = useMe();
  const person = me.k === 'ready' ? me.me.settings : null;
  const keywords = ask ? terms.list : [];

  // A plain query asks for the messages that hold every word; a question asks for each of its keywords.
  const plain = useMessageSearch(machine, words, open && !ask);
  const first = useMessageSearch(machine, keywords[0] ?? '', open && ask && keywords.length > 0);
  const second = useMessageSearch(machine, keywords[1] ?? '', open && ask && keywords.length > 1);
  const third = useMessageSearch(machine, keywords[2] ?? '', open && ask && keywords.length > 2);
  const byKeyword = [first, second, third].slice(0, Math.min(keywords.length, ASK_KEYWORDS));
  const messagesAsked = ask ? byKeyword.some((m) => m.asked) : plain.asked;
  const messagesPending = ask
    ? byKeyword.some((m) => m.asked && m.hits === null)
    : plain.asked && plain.hits === null;
  const firstHits = first.hits;
  const secondHits = second.hits;
  const thirdHits = third.hits;
  const hits = useMemo((): readonly MessageHit[] | null => {
    if (!ask) return plain.hits;
    const lists = [firstHits, secondHits, thirdHits].filter((list) => list !== null);
    return lists.length === 0 ? null : mergeHits(lists, terms.list);
  }, [ask, plain.hits, firstHits, secondHits, thirdHits, terms]);

  // The connected systems: asked for the words (a question's first keyword), each answer shown as it comes.
  const conns = mine?.conns;
  const foundText = ask ? (keywords[0] ?? '') : words;
  const foundKey = `${machine}|${foundText}`;
  useEffect(() => {
    if (!open || machine === null || !conns || [...foundText].length < FOUND_FROM) return;
    let stale = false;
    const timer = setTimeout(() => {
      setFound({ key: foundKey, byConn: {} });
      for (const conn of conns) {
        const kinds = conn.capabilities.flatMap((capability) => CAPABILITY_KIND[capability] ?? []);
        void Promise.all(kinds.map((kind) => searchConnection(machine, conn.id, kind, foundText))).then(
          (answers) => {
            if (stale) return;
            const items = answers.flatMap((res) => valueOr<readonly IFound[]>(res, []));
            setFound((current) =>
              current?.key === foundKey
                ? { key: foundKey, byConn: { ...current.byConn, [conn.id]: items } }
                : current,
            );
          },
        );
      }
    }, FOUND_DEBOUNCE_MS);
    return () => {
      stale = true;
      clearTimeout(timer);
    };
  }, [open, machine, conns, foundText, foundKey]);

  const tabs = useMemo(() => connTabs(conns ?? []), [conns]);

  // A connection's own tab, nothing typed: the person's open issues and the open merge requests, asked when the tab
  // is opened.
  const connOpen = scope.startsWith('conn:')
    ? (conns ?? []).find((conn) => connScope(conn.id) === scope)
    : undefined;
  const mineKey = `${machine}|${connOpen?.id ?? ''}`;
  useEffect(() => {
    if (!open || machine === null || !connOpen || words !== '') return;
    let stale = false;
    const kinds = MINE_KIND.filter((item) => connOpen.capabilities.includes(item.capability));
    void Promise.all(kinds.map((item) => listConnection(machine, connOpen.id, item.kind))).then((answers) => {
      if (stale) return;
      setMineOf({ key: mineKey, items: answers.flatMap((res) => valueOr<readonly IFound[]>(res, [])) });
    });
    return () => {
      stale = true;
    };
  }, [open, machine, connOpen, words, mineKey]);

  const groups = useMemo((): readonly ISearchGroup[] | null => {
    if (chats === null) return null;
    const foundNow = found?.key === foundKey ? found.byConn : {};
    const all = [
      settingsGroup(mine?.catalog ?? null, terms, true, person),
      ...searchChats(chats, projects, words, terms),
      ...(hits ? messageGroups(hits, chats, now) : []),
      designsGroup(mergeDesigns(mine?.designs ?? null, indexedDesigns), terms, true),
      docsGroup(mine?.docs ?? null, terms, true),
      appsGroup ?? null,
      ...tabs.map((tab) => foundGroup(tab, foundNow[tab.id] ?? [])),
    ].filter((group) => group !== null);
    return all;
  }, [
    chats,
    projects,
    words,
    terms,
    hits,
    now,
    mine,
    appsGroup,
    found,
    foundKey,
    tabs,
    person,
    indexedDesigns,
  ]);

  const results = useMemo(() => {
    if (groups === null) return null;
    const shown = inScope(groups, scope);
    // The messages are still being asked and nothing else answers yet: «searching», not «nothing».
    if (messagesAsked && messagesPending && shown.length === 0) return null;
    return shown;
  }, [groups, scope, messagesAsked, messagesPending]);

  // The answer: asked once the question stops changing and what was found is here.
  const answerKey = `${machine}|${words}`;
  const ready = ask && groups !== null && !messagesPending;
  useEffect(() => {
    if (!open || machine === null || !ready || groups === null) return;
    const picked = groups
      .flatMap((group) =>
        group.rows.map((row) => ({ row, label: row.kind === 'message' ? group.label : row.title })),
      )
      .slice(0, ANSWER_SOURCES);
    if (picked.length === 0) return;
    const ctl = new AbortController();
    const timer = setTimeout(() => {
      const sent: IAnswerSource[] = picked.map(({ row, label }) => ({
        kind: row.preview?.kind ?? t('next.search.kindChat'),
        title: label,
        text: row.snippet ?? row.preview?.text ?? '',
      }));
      let written = '';
      void askSearchStream(machine, words, sent, ctl.signal, (event) => {
        if (ctl.signal.aborted) return;
        if (event.type === ANSWER_EVENT.delta) {
          written += event.text;
          setAnswered({ key: answerKey, answer: { k: ANSWER.stream, text: withoutMarks(written) } });
          return;
        }
        if (event.type === ANSWER_EVENT.error) {
          setAnswered({
            key: answerKey,
            answer:
              event.code === ANSWER_NO_MODEL
                ? { k: ANSWER.noModel }
                : { k: ANSWER.failed, message: event.message || t('next.search.answerFailed') },
          });
          return;
        }
        setAnswered({
          key: answerKey,
          answer: {
            k: ANSWER.ready,
            text: event.answer.text,
            sources: event.answer.cited.flatMap((index) => picked[index] ?? []),
          },
        });
      });
    }, ANSWER_DEBOUNCE_MS);
    return () => {
      ctl.abort();
      clearTimeout(timer);
    };
    // The rows the model is shown are those found when the question settled: what a connected system adds later does
    // not ask again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, machine, ready, answerKey]);

  const hasRows = groups !== null && groups.some((group) => group.rows.length > 0);
  const answer = useMemo((): TSearchAnswer | null => {
    if (!ask) return null;
    if (answered?.key === answerKey) return answered.answer;
    // Nothing found and nothing more on its way: there is nothing to answer from.
    if (ready && !hasRows) return null;
    return { k: ANSWER.wait };
  }, [ask, answered, answerKey, ready, hasRows]);

  const scopes = useMemo((): readonly ISearchScope[] => {
    const count = (id: string) => (groups === null || words === '' ? undefined : countIn(groups, id));
    return [
      { id: SCOPE.all, label: t('next.search.scopeAll'), count: count(SCOPE.all) },
      { id: SCOPE.chats, label: t('next.search.groupChats'), count: count(SCOPE.chats) },
      { id: SCOPE.designs, label: t('next.search.groupDesigns'), count: count(SCOPE.designs) },
      { id: SCOPE.docs, label: t('next.search.groupDocs'), count: count(SCOPE.docs) },
      { id: SCOPE.apps, label: t('next.search.groupApps'), count: count(SCOPE.apps) },
      { id: SCOPE.settings, label: t('next.search.groupSettings'), count: count(SCOPE.settings) },
      ...tabs.map((tab) => ({
        id: connScope(tab.id),
        label: tab.label,
        hint: tab.hint,
        count: count(connScope(tab.id)),
      })),
    ];
  }, [tabs, groups, words]);

  // Before anything is typed each tab lists its own: what to go on with, the latest chats, the designs, the
  // documents, the apps, every setting, the person's own in a connected system.
  const idle = useMemo((): readonly ISearchGroup[] | null => {
    const one = (group: ISearchGroup | null): readonly ISearchGroup[] => (group ? [group] : []);
    if (scope === SCOPE.all)
      return idleGroups({
        chats: chats === null ? [] : recentChats(chats, projects),
        designs: mine?.designs ?? null,
        docs: mine?.docs ?? null,
      });
    if (scope === SCOPE.chats) {
      if (chats === null) return null;
      const rows = recentChats(chats, projects, true);
      return one(
        rows.length > 0 ? { id: SCOPE.chats, label: t('next.search.groupRecentChats'), rows } : null,
      );
    }
    if (scope === SCOPE.apps) return appsGroup === undefined ? null : one(appsGroup);
    if (mine === null) return null;
    if (scope === SCOPE.designs) return one(designsGroup(mine.designs, NO_TERMS, true));
    if (scope === SCOPE.docs) return one(docsGroup(mine.docs, NO_TERMS, true));
    if (scope === SCOPE.settings) return one(settingsGroup(mine.catalog, NO_TERMS, true, person));
    const tab = tabs.find((item) => connScope(item.id) === scope);
    if (!tab) return [];
    if (mineOf?.key !== mineKey) return null;
    return one(foundGroup({ id: tab.id, label: t('next.search.groupMine') }, mineOf.items));
  }, [scope, chats, projects, mine, appsGroup, tabs, mineOf, mineKey, person]);

  const empty = useMemo(() => scopeEmpty(scope), [scope]);
  const detail = useMemo(() => detailSource({ machine, chats, now }), [machine, chats, now]);

  const onAsk = useCallback(() => setAsked(words), [words]);

  const onSetting = useCallback(
    async (key: string, value: string): Promise<boolean> => {
      // The person's own setting is kept with the account, not on the machine.
      if (key.startsWith(PERSON_KEY)) {
        const patch = personPatch(key.slice(PERSON_KEY.length), value);
        const res = patch === null ? null : await patchSettings(patch);
        if (res?.r === 'accepted') return true;
        toast(t('next.search.settingFailed'), { tone: 'error' });
        return false;
      }
      if (machine === null) return false;
      const res = await putSetting(machine, key, value);
      if (!('ok' in res) || isRefused(res.value)) {
        const message = 'ok' in res && isRefused(res.value) ? res.value.message : '';
        toast(t('next.search.settingFailed'), { tone: 'error', description: message || undefined });
        return false;
      }
      const change = res.value;
      setSources((current) =>
        current === null
          ? current
          : {
              ...current,
              catalog: current.catalog.map((setting) =>
                setting.key === key
                  ? { ...setting, value: change.new, valueLabel: change.newLabel }
                  : setting,
              ),
            },
      );
      return true;
    },
    [machine],
  );

  return {
    scopes,
    results,
    idle,
    empty,
    detail,
    kept,
    indexing: ask ? byKeyword.some((m) => m.indexing) : plain.indexing,
    marks: ask ? terms.list.join(' ') : words,
    answer,
    onAsk,
    onSetting,
  };
};
