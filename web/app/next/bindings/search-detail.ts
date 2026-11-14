// What the side of the search (⌘K) says about the selected row, off what the page and the machine already have:
// a chat's last words and when (GET …/glance; HRN-1463 keeps the side of a chat to that); a design's cover and boards
// from the library; a document's versions; a task whole from its tracker.
// No model is asked: running over the list costs nothing but these reads, each kept while the search is open.
import { loadChatGlance } from '@/next/commands/pages';
import { agoText } from '@/next/features/apps/model';
import { thumbPath } from '@/next/features/designly/api';
import { peerFrom } from '@/next/features/chat/peer';
import {
  DETAIL_ACT,
  NO_DETAIL,
  type IDetailFact,
  type IDetailItem,
  type IDetailQuote,
  type IDetailSource,
  type ISearchDetail,
} from '@/next/features/search/detail';
import { ROW_GO, type ISearchRow } from '@/next/features/search/search-dialog';
import {
  FOUND_KIND,
  loadDocVersions,
  loadIssue,
  type ICatalogDoc,
  type ICatalogSetting,
  type IFound,
} from '@/next/features/search/sources-api';
import { SUBJECT } from '@/next/features/search/subject';
import { isRefused } from '@/next/features/settings/platform-answer';
import { CHAT_STATUS, type ISidebarChat } from '@/next/features/sidebar/types';
import type { IInstalledApp } from '@/next/features/app-views/api';
import { t } from '@/next/i18n';
import type { ChatGlance, Conversation, ConvId, DesignArtifact, MachineId } from '@/next/protocol/v2';
import { ROUTE, routePath } from '@/next/routes/routes';
import { conversationsStore } from '@/next/stores/conversations';

const isConvId = (id: string): id is ConvId => id.startsWith('tab-');

const LIST_MAX = 6;

const fact = (label: string, value: string, title?: string): IDetailFact[] =>
  value === '' ? [] : [{ label, value, ...(title ? { title } : {}) }];

const whenOf = (at: number | string | null, now: number): string => {
  const ms = typeof at === 'string' ? Date.parse(at) : at;
  return ms === null || Number.isNaN(ms) || ms <= 0 ? '' : agoText(ms, now);
};

const absolute = (path: string): string => new URL(path, window.location.origin).toString();

interface IChatInput {
  machine: MachineId;
  chats: readonly ISidebarChat[];
  now: number;
}

/** A chat's last words as the side quotes them: the newest of the person's (or another chat's) and the agent's. */
const lastQuote = (glance: ChatGlance, now: number): IDetailQuote | null => {
  const { lastUser: user, lastAgent: agent } = glance;
  if (agent && (!user || agent.at >= user.at))
    return { who: t('next.search.agent'), text: agent.text, when: whenOf(agent.at, now) };
  if (!user) return null;
  const said = peerFrom(user.text);
  const who = said.from === null ? t('next.search.you') : t('next.search.from', { chat: said.from });
  return { who, text: said.text, when: whenOf(user.at, now) };
};

/**
 * A chat on the side: its name, the last thing said in it and when. No states, no lists: what can be done
 * with it is in the menu.
 */
const chatDetail = (id: string, input: IChatInput, glance: ChatGlance | null): ISearchDetail => {
  const { chats, now } = input;
  const chat = chats.find((item) => item.id === id);
  const record: Conversation | undefined = isConvId(id) ? conversationsStore.get(id) : undefined;
  if (!chat) return NO_DETAIL;
  const said = glance ? lastQuote(glance, now) : null;
  // A machine that does not tell the last words yet: the line of the last turn is what there is.
  const turn =
    glance === null && record?.lastTurn?.snippet
      ? { who: t('next.search.agent'), text: record.lastTurn.snippet, when: whenOf(chat.lastActivityAt, now) }
      : null;
  const quote = said ?? turn;
  const working = chat.status === CHAT_STATUS.thinking;
  const asleep = !chat.terminal && chat.status !== CHAT_STATUS.waitingMachine;
  return {
    quotes: quote ? [quote] : [],
    facts: [],
    lists: [],
    ...(working ? { act: DETAIL_ACT.stop } : asleep ? { act: DETAIL_ACT.resume } : {}),
    link: absolute(routePath({ kind: ROUTE.chat, id })),
  };
};

/** The most names «Работали» shows; the rest are «и ещё N». */
export const PEOPLE_MAX = 3;

/** People by name, each once, at most PEOPLE_MAX and «и ещё N»; never an id. */
export const peopleLine = (names: readonly string[]): string => {
  const unique = [
    ...new Set(
      names
        .map((name) => name.trim())
        .filter((name) => name !== '' && !isConvId(name) && !name.includes('@')),
    ),
  ];
  const shown = unique.slice(0, PEOPLE_MAX).join(', ');
  const rest = unique.length - PEOPLE_MAX;
  return rest > 0 ? t('next.search.peopleMore', { names: shown, n: rest }) : shown;
};

const DESIGN_KIND_KEY: Readonly<Record<DesignArtifact['kind'], string>> = {
  site: 'site',
  slides: 'slides',
  ds: 'ds',
  video: 'video',
};

const designDetail = (
  design: DesignArtifact,
  board: string | null,
  machine: MachineId,
  chats: readonly ISidebarChat[],
  now: number,
): ISearchDetail => {
  const shownBoard = design.boards.find((item) => item.title === board) ?? design.boards[0];
  const boards = design.boards.slice(0, LIST_MAX).map((item): IDetailItem => ({
    id: `board:${design.id}:${item.path}`,
    icon: 'layout-dashboard',
    title: item.title,
    go: { k: ROW_GO.design, id: design.id, board: item.path },
  }));
  // touchedBy are chat ids (the chats that edited the project): they go into the list of chats by their titles, never
  // into «Работали»; a chat this page does not have is left out.
  const from = [...new Set([...design.convs, ...(design.touchedBy ?? [])])]
    .flatMap((id) => chats.find((chat) => chat.id === id) ?? [])
    .slice(0, LIST_MAX)
    .map((chat): IDetailItem => ({
      id: `chat:${chat.id}`,
      icon: 'message-square',
      title: chat.title,
      go: { k: ROW_GO.chat, id: chat.id },
    }));
  // The runtime names the people: the owner and whose chats worked on it, a chat agent folded into its person.
  const people = peopleLine([design.owner?.name ?? '', ...(design.people ?? [])]);
  return {
    ...(shownBoard
      ? {
          cover: {
            src: thumbPath(machine, design.id, shownBoard.path, design.updatedAt),
            alt: shownBoard.title,
          },
        }
      : {}),
    quotes: [],
    facts: [
      ...fact(
        t('next.search.factKind'),
        design.appKind?.title ?? t(`next.search.designKind.${DESIGN_KIND_KEY[design.kind]}`),
      ),
      ...fact(t('next.search.factBoards'), String(design.boards.length)),
      ...fact(t('next.search.factChanged'), whenOf(design.updatedAt, now)),
      ...fact(t('next.search.factPeople'), people),
    ],
    lists: [
      ...(boards.length > 0 ? [{ label: t('next.search.listBoards'), items: boards }] : []),
      ...(from.length > 0 ? [{ label: t('next.search.listChats'), items: from }] : []),
    ],
    link: absolute(routePath({ kind: ROUTE.site, id: design.id })),
  };
};

const docDetail = (doc: ICatalogDoc, now: number, versions: readonly IDetailItem[]): ISearchDetail => ({
  quotes: [],
  facts: [
    ...fact(t('next.search.factKind'), doc.kind === 'board' ? t('next.search.docBoard') : ''),
    ...fact(t('next.search.factOwner'), doc.owner),
    ...fact(t('next.search.factChanged'), doc.changedAt === '' ? '' : whenOf(doc.changedAt, now)),
    ...fact(t('next.search.factThreads'), doc.openThreads > 0 ? String(doc.openThreads) : ''),
  ],
  lists: versions.length > 0 ? [{ label: t('next.search.listVersions'), items: versions }] : [],
  link: doc.url,
});

const settingDetail = (setting: ICatalogSetting): ISearchDetail => ({
  quotes: [],
  text: setting.description,
  facts: [
    ...fact(t('next.search.factSection'), setting.section),
    ...fact(t('next.search.factValue'), setting.valueLabel),
    ...fact(t('next.search.factOptions'), setting.options.map((option) => option.label).join(', ')),
  ],
  lists: [],
});

const TEXT_MAX = 700;
const clip = (text: string): string => {
  const flat = text.trim();
  return [...flat].length > TEXT_MAX ? `${[...flat].slice(0, TEXT_MAX).join('')}…` : flat;
};

const foundDetail = (found: IFound, now: number): ISearchDetail => ({
  quotes: [],
  ...(found.text === '' ? {} : { text: clip(found.text) }),
  facts: [
    ...fact(
      t('next.search.factState'),
      found.draft ? `${found.state} · ${t('next.search.mrDraft')}` : found.state,
    ),
    ...fact(t(`next.search.place.${found.kind}`), found.place),
    ...fact(
      found.kind === FOUND_KIND.page ? t('next.search.factOwner') : t('next.search.factAssignee'),
      found.person,
    ),
    ...fact(t('next.search.factType'), [found.type, found.priority].filter(Boolean).join(' · ')),
    ...fact(t('next.search.factBranch'), found.branch),
    ...fact(t('next.search.factPipeline'), found.pipeline),
    ...fact(t('next.search.factChanged'), found.updated === '' ? '' : whenOf(found.updated, now)),
  ],
  lists: [],
  link: found.url,
});

const appDetail = (app: IInstalledApp): ISearchDetail => ({
  quotes: [],
  text: app.description,
  facts: fact(t('next.search.factVersion'), app.version),
  lists:
    app.commands && app.commands.length > 0
      ? [
          {
            label: t('next.search.listCommands'),
            items: app.commands.slice(0, LIST_MAX).map((command) => ({
              id: `cmd:${app.id}:${command.name}`,
              icon: 'square-slash',
              title: `/${command.name}`,
              meta: command.description,
            })),
          },
        ]
      : [],
});

const valueOf = <T>(res: { ok: true; value: T } | object): T | null =>
  'ok' in res ? (res as { value: T }).value : null;

export interface IDetailInput {
  machine: MachineId | null;
  /** null while the chat list is on its way. */
  chats: readonly ISidebarChat[] | null;
  now: number;
}

/** The side's source for the search of one machine. */
export const detailSource = ({ machine, chats, now }: IDetailInput): IDetailSource => {
  const list = chats ?? [];
  const now_ = (row: ISearchRow): ISearchDetail => {
    const subject = row.subject;
    if (!subject || machine === null) return NO_DETAIL;
    if (subject.k === SUBJECT.chat) return chatDetail(subject.id, { machine, chats: list, now }, null);
    if (subject.k === SUBJECT.design) return designDetail(subject.design, subject.board, machine, list, now);
    if (subject.k === SUBJECT.doc) return docDetail(subject.doc, now, []);
    if (subject.k === SUBJECT.setting) return settingDetail(subject.setting);
    if (subject.k === SUBJECT.found) return foundDetail(subject.found, now);
    return appDetail(subject.app);
  };
  return {
    now: now_,
    loads: (row) => {
      const subject = row.subject;
      if (!subject || machine === null) return false;
      if (subject.k === SUBJECT.chat) return isConvId(subject.id);
      if (subject.k === SUBJECT.doc) return true;
      return (
        subject.k === SUBJECT.found && subject.found.kind === FOUND_KIND.issue && subject.found.place !== ''
      );
    },
    more: async (row) => {
      const subject = row.subject;
      if (!subject || machine === null) return null;
      if (subject.k === SUBJECT.chat && isConvId(subject.id)) {
        const said = valueOf<ChatGlance>(await loadChatGlance(machine, subject.id));
        // A machine that knows no last words answers with none: the last turn's line stays instead.
        return said === null ? null : chatDetail(subject.id, { machine, chats: list, now }, said);
      }
      if (subject.k === SUBJECT.doc) {
        const res = await loadDocVersions(machine, subject.doc.slug);
        if (!('ok' in res) || isRefused(res.value)) return null;
        const versions = res.value.slice(0, LIST_MAX).map((version): IDetailItem => ({
          id: `version:${version.n}`,
          icon: 'history',
          title: version.note || t('next.search.versionOf', { n: version.n }),
          meta: [version.author, whenOf(version.at, now)].filter(Boolean).join(' · '),
        }));
        return docDetail(subject.doc, now, versions);
      }
      if (subject.k === SUBJECT.found && subject.found.kind === FOUND_KIND.issue) {
        const res = await loadIssue(machine, subject.conn, subject.found.place);
        if (!('ok' in res) || isRefused(res.value)) return null;
        return foundDetail({ ...subject.found, ...res.value }, now);
      }
      return null;
    },
  };
};
