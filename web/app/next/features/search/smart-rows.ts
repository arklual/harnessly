// The search's rows beyond the chats: settings (changed in the row itself), the sections of Settings, the team's
// documents, the designs of the library and what the connected systems found. Each builder answers a group, or null
// when it has nothing for the words.
import { formatWhen } from '@/next/i18n/format';
import { matching, type ITerms } from '@/next/features/search/terms';
import {
  ROW_GO,
  ROW_WIDGET,
  type ISearchGroup,
  type ISearchRow,
  type ISearchWidget,
} from '@/next/features/search/search-dialog';
import {
  FOUND_KIND,
  SETTING_KIND,
  SETTING_ON,
  type ICatalogDoc,
  type ICatalogSetting,
  type IFound,
  type TFoundKind,
} from '@/next/features/search/sources-api';
import {
  pageSettings,
  personWidget,
  sectionWords,
  type IPageSetting,
} from '@/next/features/search/page-settings';
import { designIcon } from '@/next/features/designly/design-icon';
import { SUBJECT } from '@/next/features/search/subject';
import { t } from '@/next/i18n';
import type { DesignArtifact } from '@/next/protocol/v2';
import type { SettingsSection } from '@/next/routes/routes';
import type { PersonSettings } from '@/next/stores/me';

export const SCOPE = {
  all: 'all',
  chats: 'chats',
  docs: 'docs',
  designs: 'designs',
  apps: 'apps',
  settings: 'settings',
} as const;

/** The scope of a connection's results. */
export const connScope = (id: string): string => `conn:${id}`;

const ROWS_MAX = 5;
/** A tab of its own shows more of its kind than «Всё» does. */
const ROWS_OWN = 40;

/** The section of Settings a setting of the catalog is changed in. */
export const sectionOfSetting = (key: string): SettingsSection => {
  if (key.startsWith('background.')) return 'smart';
  if (key === 'providers') return 'accounts';
  return 'general';
};

const widgetOf = (setting: ICatalogSetting): ISearchWidget | undefined => {
  if (setting.kind === SETTING_KIND.bool)
    return { kind: ROW_WIDGET.toggle, on: setting.value === SETTING_ON };
  if (setting.kind === SETTING_KIND.enum)
    return {
      kind: ROW_WIDGET.pick,
      value: setting.value,
      label: setting.valueLabel,
      options: setting.options,
    };
  return undefined;
};

const settingRow = (setting: ICatalogSetting): ISearchRow => ({
  id: `setting:${setting.key}`,
  kind: 'setting',
  icon: 'sliders-horizontal',
  title: setting.title,
  snippet: `${setting.section} · ${setting.description}`,
  // A number has no control in the row: its value is said, and the row opens it in Settings.
  meta: setting.kind === SETTING_KIND.number ? setting.valueLabel : '',
  widget: widgetOf(setting),
  go: { k: ROW_GO.setting, key: setting.key, section: sectionOfSetting(setting.key) },
  preview: { kind: t('next.search.kindSetting'), text: setting.description },
  subject: { k: SUBJECT.setting, setting },
});

/** A setting as its page names it: the row leads to the section it is changed in; the person's own simple ones are
 *  changed right in the row. */
const pageRow = (setting: IPageSetting, person: PersonSettings | null): ISearchRow => {
  const own = personWidget(setting.id, person);
  const where = t(`next.settings.section.${setting.section}`);
  return {
    id: `page-setting:${setting.id}`,
    kind: 'setting',
    icon: 'sliders-horizontal',
    title: setting.title,
    snippet: where,
    meta: '',
    widget: own?.widget,
    go: own
      ? { k: ROW_GO.setting, key: own.key, section: setting.section }
      : { k: ROW_GO.section, section: setting.section },
    preview: {
      kind: t('next.search.kindSetting'),
      text: t('next.search.pageSettingHint', { section: where }),
    },
  };
};

/** The sections of Settings the search leads to, with the words each is found by. */
const SECTIONS: readonly SettingsSection[] = [
  'general',
  'appearance',
  'keys',
  'notifications',
  'voice',
  'accounts',
  'smart',
  'claude',
  'codex',
  'skills',
  'safe',
  'browser',
  'machines',
  'apps',
  'connections',
];

const sectionRow = (section: SettingsSection): ISearchRow => ({
  id: `section:${section}`,
  kind: 'setting',
  icon: 'settings',
  title: t(`next.settings.section.${section}`),
  snippet: t('next.search.sectionOf'),
  meta: '',
  go: { k: ROW_GO.section, section },
  preview: { kind: t('next.search.kindSection'), text: t('next.search.sectionHint') },
});

/** The catalog's settings, the pages' own (those the catalog does not name already) and the sections, in that order. */
const settingRows = (
  catalog: readonly ICatalogSetting[],
  terms: ITerms | null,
  person: PersonSettings | null,
): ISearchRow[] => {
  const pages = pageSettings();
  const pageRowOf = (page: IPageSetting) => pageRow(page, person);
  if (terms === null)
    return [...SECTIONS.map(sectionRow), ...catalog.map(settingRow), ...pages.map(pageRowOf)];
  const settings = matching(catalog, terms, (s) => [s.title, s.section, s.description]);
  const taken = new Set(settings.map((setting) => setting.title.toLowerCase()));
  const labels = matching(pages, terms, (page) => [page.title, page.words]).filter(
    (page) => !taken.has(page.title.toLowerCase()),
  );
  const sections = matching(SECTIONS, terms, (section) => [
    t(`next.settings.section.${section}`),
    sectionWords(section),
  ]);
  return [...settings.map(settingRow), ...labels.map(pageRowOf), ...sections.map(sectionRow)];
};

/** `own` is the tab of the settings itself: every match, and with no words every section and setting. */
export const settingsGroup = (
  catalog: readonly ICatalogSetting[] | null,
  terms: ITerms,
  own = false,
  person: PersonSettings | null = null,
): ISearchGroup | null => {
  if (terms.list.length === 0 && !own) return null;
  const rows = settingRows(catalog ?? [], terms.list.length === 0 ? null : terms, person).slice(
    0,
    own ? ROWS_OWN * 2 : ROWS_MAX + 1,
  );
  return rows.length > 0 ? { id: SCOPE.settings, label: t('next.search.groupSettings'), rows } : null;
};

/** «3 ч», «вчера», «ср», «13 нояб.» for a time the catalog gave, as the chats' rows say it; '' for none. */
const dayOf = (at: number | null): string =>
  at === null || Number.isNaN(at) ? '' : formatWhen(at, Date.now());

const docRow = (doc: ICatalogDoc): ISearchRow => ({
  id: `doc:${doc.slug}`,
  kind: 'doc',
  icon: 'file-text',
  title: doc.title,
  meta: dayOf(doc.changedAt === '' ? null : Date.parse(doc.changedAt)),
  go: { k: ROW_GO.link, url: doc.url },
  preview: { kind: t('next.search.kindDoc'), text: t('next.search.docHint') },
  subject: { k: SUBJECT.doc, doc },
});

const newestDocs = (docs: readonly ICatalogDoc[]): ICatalogDoc[] =>
  [...docs].sort((a, b) => b.changedAt.localeCompare(a.changedAt));

export const docsGroup = (
  docs: readonly ICatalogDoc[] | null,
  terms: ITerms,
  own = false,
): ISearchGroup | null => {
  if (terms.list.length === 0 && !own) return null;
  const sorted = newestDocs(docs ?? []);
  const found =
    terms.list.length === 0 ? sorted : matching(sorted, terms, (doc) => [doc.title, doc.slug, doc.owner]);
  const rows = found.slice(0, own ? ROWS_OWN : ROWS_MAX).map(docRow);
  return rows.length > 0 ? { id: SCOPE.docs, label: t('next.search.groupDocs'), rows } : null;
};

const designRow = (design: DesignArtifact, board: string | null): ISearchRow => ({
  id: board === null ? `design:${design.id}` : `design:${design.id}:${board}`,
  kind: 'board',
  icon: designIcon(design),
  title: board ?? design.title,
  snippet: board === null ? undefined : design.title,
  meta: dayOf(design.updatedAt),
  go: { k: ROW_GO.design, id: design.id },
  subject: { k: SUBJECT.design, design, board },
  preview: {
    kind: t('next.search.kindDesign'),
    text: board === null ? t('next.search.designHint') : t('next.search.boardHint', { design: design.title }),
  },
});

const newestDesigns = (designs: readonly DesignArtifact[]): DesignArtifact[] =>
  [...designs].sort((a, b) => b.updatedAt - a.updatedAt);

/** The designs whose name has the words, then the boards that have them (a board says which design it is of). */
export const designsGroup = (
  designs: readonly DesignArtifact[] | null,
  terms: ITerms,
  own = false,
): ISearchGroup | null => {
  if (terms.list.length === 0 && !own) return null;
  const sorted = newestDesigns(designs ?? []);
  if (terms.list.length === 0) {
    const all = sorted.slice(0, ROWS_OWN).map((design) => designRow(design, null));
    return all.length > 0 ? { id: SCOPE.designs, label: t('next.search.groupDesigns'), rows: all } : null;
  }
  const byName = matching(sorted, terms, (design) => [design.title]).map((design) => designRow(design, null));
  const boards = sorted.flatMap((design) => design.boards.map((board) => ({ design, title: board.title })));
  const byBoard = matching(boards, terms, (board) => [board.title]).map((board) =>
    designRow(board.design, board.title),
  );
  const rows = [...byName, ...byBoard].slice(0, own ? ROWS_OWN : ROWS_MAX);
  return rows.length > 0 ? { id: SCOPE.designs, label: t('next.search.groupDesigns'), rows } : null;
};

const FOUND_ICON: Record<TFoundKind, string> = {
  [FOUND_KIND.mr]: 'git-pull-request',
  [FOUND_KIND.issue]: 'circle-dot',
  [FOUND_KIND.page]: 'book-open',
};

const foundRow = (conn: string, found: IFound): ISearchRow => ({
  id: `found:${conn}:${found.url}`,
  kind: 'found',
  icon: FOUND_ICON[found.kind],
  title: found.title,
  snippet: [found.place, t(`next.search.found.${found.kind}`), found.state].filter(Boolean).join(' · '),
  meta: dayOf(found.updated === '' ? null : Date.parse(found.updated)),
  subject: { k: SUBJECT.found, conn, found },
  go:
    found.kind === FOUND_KIND.mr ? { k: ROW_GO.review, url: found.url } : { k: ROW_GO.link, url: found.url },
  preview: { kind: t(`next.search.found.${found.kind}`), text: t('next.search.foundHint') },
});

/** What one connected system found (or lists of the person's own), under the name its tab has. */
export const foundGroup = (
  conn: { id: string; label: string },
  found: readonly IFound[],
): ISearchGroup | null =>
  found.length > 0
    ? { id: connScope(conn.id), label: conn.label, rows: found.map((item) => foundRow(conn.id, item)) }
    : null;

/** What a connection gives the search, by what it can do. */
const CAPABILITY = { code: 'code', issues: 'issues', pages: 'pages' } as const;

export interface IConnTab {
  id: string;
  /** What the tab is called: what the system is for («Трекер»), its product for a code host, its own name when two
   *  connections would be called the same. */
  label: string;
  /** The connection's own name and address, for the tab's hint. */
  hint: string;
}

const hostOf = (address: string): string => {
  try {
    return new URL(address).host;
  } catch {
    return address;
  }
};

const PRODUCT: Readonly<Record<string, string>> = {
  gitlab: 'GitLab',
  github: 'GitHub',
  jira: 'Jira',
  youtrack: 'YouTrack',
  linear: 'Linear',
  confluence: 'Confluence',
  gitea: 'Gitea',
  bitbucket: 'Bitbucket',
};

const tabLabel = (conn: { kind: string; capabilities: readonly string[] }): string => {
  if (conn.capabilities.includes(CAPABILITY.code)) return PRODUCT[conn.kind] ?? t('next.search.tabCode');
  if (conn.capabilities.includes(CAPABILITY.issues)) return t('next.search.tabTracker');
  return t('next.search.tabWiki');
};

/** The tabs of the connected systems that can be searched, each under a name that says what it is. */
export const connTabs = (
  conns: readonly {
    id: string;
    kind: string;
    name: string;
    baseUrl: string;
    capabilities: readonly string[];
  }[],
): readonly IConnTab[] => {
  const searched = conns.filter((conn) =>
    conn.capabilities.some((capability) => Object.values<string>(CAPABILITY).includes(capability)),
  );
  const labels = searched.map(tabLabel);
  return searched.map((conn, i) => {
    const label = labels[i] ?? conn.name;
    const twice = labels.filter((other) => other === label).length > 1;
    const host = hostOf(conn.baseUrl);
    return {
      id: conn.id,
      label: twice ? `${label} · ${PRODUCT[conn.kind] ?? conn.name}` : label,
      hint: conn.name === host || conn.name === '' ? host : `${conn.name} · ${host}`,
    };
  });
};

export interface IIdleInput {
  chats: readonly ISearchRow[];
  designs: readonly DesignArtifact[] | null;
  docs: readonly ICatalogDoc[] | null;
}

const IDLE_CHATS = 4;
const IDLE_OTHERS = 2;

/** Before anything is typed: what to go on with, and what the search can start. */
export const idleGroups = ({ chats, designs, docs }: IIdleInput): readonly ISearchGroup[] => {
  const goOn = [
    ...chats.slice(0, IDLE_CHATS),
    ...newestDesigns(designs ?? [])
      .slice(0, IDLE_OTHERS)
      .map((design) => designRow(design, null)),
    ...newestDocs(docs ?? [])
      .slice(0, IDLE_OTHERS)
      .map(docRow),
  ];
  const actions: ISearchRow[] = [
    {
      id: 'act:new-chat',
      kind: 'action',
      icon: 'plus',
      title: t('next.search.actNewChat'),
      snippet: t('next.search.actNewChatHint'),
      meta: '',
      go: { k: ROW_GO.newChat },
      preview: { kind: t('next.search.kindAction'), text: t('next.search.actNewChatText') },
    },
    {
      id: 'act:designs',
      kind: 'action',
      icon: 'pen-tool',
      title: t('next.search.actDesigns'),
      meta: '',
      go: { k: ROW_GO.designs },
      preview: { kind: t('next.search.kindAction'), text: t('next.search.actDesignsText') },
    },
    {
      id: 'act:settings',
      kind: 'action',
      icon: 'settings',
      title: t('next.search.actSettings'),
      meta: '',
      go: { k: ROW_GO.section, section: null },
      preview: { kind: t('next.search.kindAction'), text: t('next.search.actSettingsText') },
    },
  ];
  return [
    ...(goOn.length > 0 ? [{ id: 'go-on', label: t('next.search.groupGoOn'), rows: goOn }] : []),
    { id: 'actions', label: t('next.search.groupActions'), rows: actions },
  ];
};

/** What a tab says when it has nothing to list before anything is typed. */
export interface IScopeEmpty {
  title: string;
  hint: string;
}

const EMPTY_KEY: Readonly<Record<string, string>> = {
  [SCOPE.chats]: 'chats',
  [SCOPE.designs]: 'designs',
  [SCOPE.docs]: 'docs',
  [SCOPE.apps]: 'apps',
  [SCOPE.settings]: 'settings',
};

export const scopeEmpty = (scope: string): IScopeEmpty => {
  const key = EMPTY_KEY[scope] ?? 'conn';
  return { title: t(`next.search.empty.${key}`), hint: t(`next.search.empty.${key}Hint`) };
};

/** The groups whose rows «Всё» shows only the first of: their own tab has the rest. */
const CAPPED: ReadonlySet<string> = new Set([SCOPE.settings, SCOPE.docs, SCOPE.designs, SCOPE.apps]);

/** The groups a scope keeps: all of them for «Всё» (the long ones cut short), else the one of that scope (the chats'
 *  scope keeps the messages). */
export const inScope = (groups: readonly ISearchGroup[], scope: string): readonly ISearchGroup[] => {
  if (scope === SCOPE.all)
    return groups.map((group) =>
      CAPPED.has(group.id) && group.rows.length > ROWS_MAX
        ? { ...group, rows: group.rows.slice(0, ROWS_MAX) }
        : group,
    );
  if (scope === SCOPE.chats)
    return groups.filter((group) => group.id === SCOPE.chats || group.id.startsWith('messages:'));
  return groups.filter((group) => group.id === scope);
};

/** How many rows the words found in a scope. */
export const countIn = (groups: readonly ISearchGroup[], scope: string): number => {
  const own =
    scope === SCOPE.all
      ? groups
      : groups.filter(
          (group) => group.id === scope || (scope === SCOPE.chats && group.id.startsWith('messages:')),
        );
  return own.reduce((sum, group) => sum + group.rows.length, 0);
};
