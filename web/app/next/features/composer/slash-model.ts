// The slash palette as plain functions: which word before the caret asks for it, the rows it lists for that word
// (settings of the chat, the agent's commands and skills, MCP servers, quick prompts, files) and what a row does when
// it is chosen. Nothing here touches the page: the hook in slash-palette.tsx puts it on the field.
import { bestMatch, fuzzyMatch, type TRange } from '@/next/features/composer/slash-match';
import {
  effortHint,
  effortLabel,
  modelOf,
  withModel,
  type IModelChoice,
  type IPickerAgent,
} from '@/next/features/composer/agents';
import type { IQuickPrompt } from '@/next/features/composer/prompts';
import type { ICatalogItem } from '@/next/features/settings/api';
import { t } from '@/next/i18n';
import { EFFORTS, type FileEntry, type Provider } from '@/next/protocol/v2';

export const GROUP = {
  setting: 'setting',
  command: 'command',
  skill: 'skill',
  mcp: 'mcp',
  quick: 'quick',
  file: 'file',
} as const;
export type TGroup = (typeof GROUP)[keyof typeof GROUP];

/** The order the groups are listed in. */
export const GROUP_ORDER: readonly TGroup[] = [
  GROUP.setting,
  GROUP.command,
  GROUP.skill,
  GROUP.mcp,
  GROUP.quick,
  GROUP.file,
];

/** What starts a word of the palette: a command, a skill of Codex, a file. */
export const TRIGGER_CHAR = { command: '/', skill: '$', file: '@' } as const;
export type TTriggerChar = (typeof TRIGGER_CHAR)[keyof typeof TRIGGER_CHAR];

export interface ITrigger {
  char: TTriggerChar;
  /** The word's place in the text: from its trigger to the end of it. */
  start: number;
  end: number;
  /** What is typed after the trigger, up to the caret. */
  query: string;
}

interface ITriggerOptions {
  /** Codex's skills are asked for with «$». */
  skills: boolean;
  /** The chat's folder can be listed, so «@» names a file. */
  files: boolean;
}

const WORD_BEFORE = /(^|\s)([/$@])(\S*)$/;

/**
 * The word before the caret that asks for the palette: «/» at the start of the field or after a space, «$» and «@»
 * the same way where they are used. A «/» word with another «/» in it is a path, not a command.
 */
export const detectTrigger = (text: string, caret: number, options: ITriggerOptions): ITrigger | null => {
  const found = WORD_BEFORE.exec(text.slice(0, caret));
  if (!found) return null;
  const char = found[2] as TTriggerChar;
  const query = found[3];
  if (char === TRIGGER_CHAR.skill && !options.skills) return null;
  if (char === TRIGGER_CHAR.file && !options.files) return null;
  if (char === TRIGGER_CHAR.command && query.includes('/')) return null;
  const start = found.index + found[1].length;
  const rest = /^\S*/.exec(text.slice(caret))?.[0] ?? '';
  return { char, start, end: caret + rest.length, query };
};

export const SUB = { model: 'model', effort: 'effort' } as const;
export type TSub = (typeof SUB)[keyof typeof SUB];

export interface ISubMode {
  sub: TSub;
  /** What is typed after «/model ». */
  query: string;
}

const SUB_LINE = /^\/(model|effort)[ \t]+([^\n]*)$/i;

/** «/model so»: the field is a choice among the models (or the efforts) that «so» narrows. */
export const detectSub = (text: string, subs: readonly TSub[]): ISubMode | null => {
  const found = SUB_LINE.exec(text);
  if (!found) return null;
  const sub = found[1].toLowerCase() as TSub;
  return subs.includes(sub) ? { sub, query: found[2].trim() } : null;
};

/** What the chat lets the palette do besides typing: each is absent where the screen has no such thing. */
export interface ISlashActions {
  goal?: () => void;
  newChat?: () => void;
  compact?: () => void;
  resume?: () => void;
}

export const RUN = { goal: 'goal', newChat: 'newChat', compact: 'compact', resume: 'resume' } as const;
export type TRun = (typeof RUN)[keyof typeof RUN];

export type TAction =
  /** The word becomes this text. */
  | { k: 'insert'; text: string }
  /** The word becomes «/model » and the field turns to the list of models. */
  | { k: 'sub'; sub: TSub }
  /** The word goes out of the field and the screen does it. */
  | { k: 'run'; run: TRun }
  /** The word becomes «@folder/» and goes on to the folder's entries. */
  | { k: 'dir'; text: string }
  /** The model (or the depth) is chosen: the existing switch, as the pill does it. */
  | { k: 'choose'; choice: IModelChoice };

export interface ISlashRow {
  id: string;
  group: TGroup;
  /** What the row is called, without its trigger. */
  name: string;
  /** What is put before the name: «/», «$», «@» or nothing (an MCP server). */
  prefix: string;
  /** The places of `name` the query matched. */
  ranges: TRange[];
  description: string;
  /** What follows the name: [instructions]. */
  hint: string | null;
  /** Where it comes from: skill, command, mcp. */
  source: string;
  icon: string;
  /** The model or depth the chat is on now. */
  current: boolean;
  action: TAction;
  score: number;
}

const ICON: Record<TGroup, string> = {
  setting: 'sliders-horizontal',
  command: 'square-slash',
  skill: 'sparkles',
  mcp: 'plug',
  quick: 'zap',
  file: 'file-text',
};

const SOURCE_KEY: Record<string, string> = {
  builtin: 'next.slash.src.builtin',
  user: 'next.slash.src.user',
  project: 'next.slash.src.project',
  skill: 'next.slash.src.skill',
  plugin: 'next.slash.src.plugin',
  quick: 'next.slash.src.quick',
};

/** A plugin's label names the plugin, so it stays the catalog's own. */
const sourceLabel = (item: ICatalogItem): string =>
  SOURCE_KEY[item.source] && item.source !== 'plugin' ? t(SOURCE_KEY[item.source]) : item.sourceLabel;

export interface IMcpRef {
  name: string;
  /** The program or the host behind it. */
  target: string;
}

export interface ISlashAvailable {
  model: boolean;
  effort: boolean;
  actions: ISlashActions;
}

interface IRowsInput {
  trigger: ITrigger;
  provider: Provider;
  /** null: not loaded yet. */
  catalog: readonly ICatalogItem[] | null;
  mcp: readonly IMcpRef[];
  quick: readonly IQuickPrompt[];
  available: ISlashAvailable;
  /** The chat is not started: only what may be a first message is offered. */
  starting: boolean;
}

export interface IRowList {
  rows: ISlashRow[];
  /** Matches left out of the list: the query is to be narrowed. */
  more: number;
}

/** A group lists this many rows for an empty query (the rest is for a query), this many for a typed one. */
const SHOWN_EMPTY = 8;
const SHOWN_TYPED = 24;
/** A description is searched from this many typed letters. */
const DESCRIPTION_FROM = 3;
/** A row found by its description comes after the ones found by their names. */
const DESCRIPTION_SCORE = 50;

type TRowBody = Omit<ISlashRow, 'score' | 'ranges'>;

interface IDraft extends TRowBody {
  /** The spellings the query is looked for in (the name, its aliases). */
  names: readonly string[];
  /** Some more text it is looked for in (the description). */
  also: string;
}

const scoreOf = (draft: IDraft, query: string): { score: number; ranges: TRange[] } | null => {
  if (!query) return { score: 0, ranges: [] };
  const named = bestMatch(query, draft.names);
  if (named) return { score: named.score, ranges: named.name === draft.name ? named.ranges : [] };
  if (query.length >= DESCRIPTION_FROM && draft.also.toLowerCase().includes(query.toLowerCase()))
    return { score: DESCRIPTION_SCORE, ranges: [] };
  return null;
};

const settingDrafts = (available: ISlashAvailable): IDraft[] => {
  const drafts: IDraft[] = [];
  const add = (
    name: string,
    description: string,
    action: TAction,
    names: readonly string[] = [name],
    hint: string | null = null,
  ) =>
    drafts.push({
      id: `setting:${name}`,
      group: GROUP.setting,
      name,
      prefix: TRIGGER_CHAR.command,
      description,
      hint,
      source: t('next.slash.src.setting'),
      icon: ICON.setting,
      current: false,
      action,
      names,
      also: description,
    });
  if (available.model) add('model', t('next.slash.model'), { k: 'sub', sub: SUB.model });
  if (available.effort) add('effort', t('next.slash.effort'), { k: 'sub', sub: SUB.effort });
  const { actions } = available;
  if (actions.goal) add('goal', t('next.slash.goal'), { k: 'run', run: RUN.goal });
  if (actions.compact) add('compact', t('next.slash.compact'), { k: 'run', run: RUN.compact });
  if (actions.newChat)
    add('clear', t('next.slash.clear'), { k: 'run', run: RUN.newChat }, ['clear', 'new', 'reset']);
  if (actions.resume) add('resume', t('next.slash.resume'), { k: 'run', run: RUN.resume });
  return drafts;
};

/** The names a setting row takes over from the agent's own commands. */
const shadowed = (drafts: readonly IDraft[]): Set<string> => new Set(drafts.flatMap((draft) => draft.names));

const isSkill = (item: ICatalogItem): boolean =>
  item.trigger === TRIGGER_CHAR.skill || item.source === 'skill' || item.source === 'plugin';

/**
 * What the agent's catalog offers to type: the commands that go to the agent as a message, not the ones that open its
 * terminal screens (the web has no place for those) and not the hidden ones. Skills only: after a space in the text.
 */
const catalogDrafts = (
  catalog: readonly ICatalogItem[],
  trigger: ITrigger,
  atStart: boolean,
  taken: ReadonlySet<string>,
  starting: boolean,
): IDraft[] =>
  catalog
    .filter((item) => !item.hidden && item.execution === 'send' && !taken.has(item.name))
    .filter((item) => !starting || item.welcome)
    .filter((item) => {
      if (trigger.char === TRIGGER_CHAR.skill) return item.trigger === TRIGGER_CHAR.skill;
      return atStart || isSkill(item);
    })
    .map((item) => ({
      id: `${item.trigger}${item.source}:${item.name}`,
      group: isSkill(item) ? GROUP.skill : GROUP.command,
      name: item.name,
      prefix: item.trigger,
      description: item.description,
      hint: item.argumentHint,
      source: sourceLabel(item),
      icon: isSkill(item) ? ICON.skill : ICON.command,
      current: false,
      action: { k: 'insert', text: `${item.trigger}${item.name} ` },
      names: [item.name, ...item.aliases],
      also: item.description,
    }));

const mcpDrafts = (mcp: readonly IMcpRef[], provider: Provider): IDraft[] =>
  mcp.map((server) => ({
    id: `mcp:${server.name}`,
    group: GROUP.mcp,
    name: server.name,
    prefix: '',
    description: server.target,
    hint: null,
    source: t('next.slash.src.mcp'),
    icon: ICON.mcp,
    current: false,
    // Claude names a server's tools mcp__<server>__<tool>; Codex reads the server's name.
    action: { k: 'insert', text: `${provider === 'claude' ? `mcp__${server.name}` : server.name} ` },
    names: [server.name],
    also: server.target,
  }));

const quickDrafts = (quick: readonly IQuickPrompt[]): IDraft[] =>
  quick.map((item) => ({
    id: `quick:${item.name}`,
    group: GROUP.quick,
    name: item.name,
    prefix: TRIGGER_CHAR.command,
    description: item.text,
    hint: null,
    source: t('next.slash.src.quick'),
    icon: ICON.quick,
    current: false,
    action: { k: 'insert', text: item.text },
    names: [item.name],
    also: item.text,
  }));

/** The drafts that match, in the order the palette lists them, with the groups cut to what is shown. */
const arranged = (drafts: readonly IDraft[], query: string): IRowList => {
  const scored: ISlashRow[] = [];
  for (const draft of drafts) {
    const match = scoreOf(draft, query);
    if (!match) continue;
    const { names: _names, also: _also, ...row } = draft;
    scored.push({ ...row, ...match });
  }
  const cap = query ? SHOWN_TYPED : SHOWN_EMPTY;
  const rows: ISlashRow[] = [];
  let more = 0;
  for (const group of GROUP_ORDER) {
    const inGroup = scored.filter((row) => row.group === group);
    // Settings keep the order they are defined in; the rest by how well they match (a stable sort keeps the catalog's order).
    if (group !== GROUP.setting) inGroup.sort((a, b) => b.score - a.score);
    rows.push(...inGroup.slice(0, cap));
    more += Math.max(0, inGroup.length - cap);
  }
  return { rows, more };
};

/** The rows for a «/» or «$» word. */
export const buildRows = ({
  trigger,
  provider,
  catalog,
  mcp,
  quick,
  available,
  starting,
}: IRowsInput): IRowList => {
  const atStart = trigger.start === 0;
  const settings = atStart && trigger.char === TRIGGER_CHAR.command ? settingDrafts(available) : [];
  const drafts = [
    ...settings,
    ...catalogDrafts(catalog ?? [], trigger, atStart, shadowed(settings), starting),
    ...(trigger.char === TRIGGER_CHAR.command ? [...mcpDrafts(mcp, provider), ...quickDrafts(quick)] : []),
  ];
  return arranged(drafts, trigger.query);
};

/** The line the person typed is exactly this row's name: Enter then sends it as it is, not completes it. */
export const isTyped = (row: ISlashRow, query: string): boolean =>
  row.action.k === 'insert' && row.name.toLowerCase() === query.toLowerCase();

/** The models of the agent for «/model», the chat's own marked. */
export const modelRows = (agent: IPickerAgent, value: IModelChoice, query: string): ISlashRow[] => {
  const now = modelOf(agent, value.model)?.id ?? null;
  const rows: ISlashRow[] = [];
  for (const model of agent.models) {
    const match = bestMatch(query, [model.label, model.id]);
    if (!match) continue;
    rows.push({
      id: `model:${model.id}`,
      group: GROUP.setting,
      name: model.label,
      prefix: '',
      ranges: match.name === model.label ? match.ranges : [],
      description: model.hint,
      hint: null,
      source: model.id,
      icon: ICON.setting,
      current: model.id === now,
      action: { k: 'choose', choice: withModel(value, agent, model) },
      score: match.score,
    });
  }
  return query ? rows.sort((a, b) => b.score - a.score) : rows;
};

/** The depths the chat's model takes for «/effort», the chat's own marked. */
export const effortRows = (agent: IPickerAgent, value: IModelChoice, query: string): ISlashRow[] => {
  const model = modelOf(agent, value.model);
  if (!model) return [];
  const rows: ISlashRow[] = [];
  for (const effort of EFFORTS.filter((e) => model.efforts.includes(e))) {
    const match = bestMatch(query, [effortLabel(effort), effort]);
    if (!match) continue;
    rows.push({
      id: `effort:${effort}`,
      group: GROUP.setting,
      name: effortLabel(effort),
      prefix: '',
      ranges: match.name === effortLabel(effort) ? match.ranges : [],
      description: effortHint(effort),
      hint: null,
      source: effort,
      icon: ICON.setting,
      current: effort === value.effort,
      action: {
        k: 'choose',
        choice: { ...value, provider: agent.provider, effort },
      },
      score: match.score,
    });
  }
  return query ? rows.sort((a, b) => b.score - a.score) : rows;
};

/** The «@» word as a folder and a name: «@src/ap» is the folder `src` and `ap`; «@ap» is the folder root. */
export const splitMention = (query: string): { dir: string; name: string } => {
  const cut = query.lastIndexOf('/');
  return cut < 0 ? { dir: '', name: query } : { dir: query.slice(0, cut), name: query.slice(cut + 1) };
};

const FILES_SHOWN = 30;

/** The entries of the folder that the name narrows: folders first, as the folder is listed. */
export const fileRows = (entries: readonly FileEntry[], name: string): IRowList => {
  const rows: ISlashRow[] = [];
  for (const entry of entries) {
    const match = fuzzyMatch(name, entry.name);
    if (!match) continue;
    const folder = entry.kind === 'directory';
    rows.push({
      id: `file:${entry.path}`,
      group: GROUP.file,
      name: folder ? `${entry.name}/` : entry.name,
      prefix: TRIGGER_CHAR.file,
      ranges: match.ranges,
      description: '',
      hint: null,
      source: t(folder ? 'next.slash.src.folder' : 'next.slash.src.file'),
      icon: folder ? 'folder' : ICON.file,
      current: false,
      action: folder ? { k: 'dir', text: `@${entry.path}/` } : { k: 'insert', text: `@${entry.path} ` },
      score: match.score,
    });
  }
  // A typed name puts the better match first; the folders-first order of the listing keeps among equals.
  const ordered = name ? [...rows].sort((a, b) => b.score - a.score) : rows;
  return { rows: ordered.slice(0, FILES_SHOWN), more: Math.max(0, ordered.length - FILES_SHOWN) };
};

/** The text with the word `[start, end)` replaced; the caret goes after the replacement. A space the replacement ends with
 *  takes the one that already follows the word. */
export const replaceWord = (
  text: string,
  start: number,
  end: number,
  insert: string,
): { text: string; caret: number } => {
  const merged = insert.endsWith(' ') && /^[ \t]/.test(text.slice(end)) ? end + 1 : end;
  return { text: text.slice(0, start) + insert + text.slice(merged), caret: start + insert.length };
};

/** The word taken out of the text, with the space it leaves doubled collapsed. */
export const removeWord = (text: string, start: number, end: number): { text: string; caret: number } => {
  const before = text.slice(0, start);
  const after = text.slice(end).replace(/^[ \t]/, '');
  return { text: before + after, caret: start };
};

/** The selection after ↑ (-1) or ↓ (1): it goes round the list. */
export { moveSelection, clampSelection } from '@/next/features/composer/prompts';
