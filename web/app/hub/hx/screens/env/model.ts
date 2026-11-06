// What the environment screen knows about the kinds of items: the form of each kind (the draft), how it turns into the
// catalog's `source` and back, and the words the board says about it (the name, the line under it, «Что сделает»).
// Pure, so each piece is tested on its own (model.test.ts).
import type { CatalogItem, CatalogKind, CatalogSource, ScriptFrom } from '../../../../hub-api/env';
import { plural } from './format';

/** The kinds a person adds. «Модель» of the board is not here: the hub has no storage for it yet. */
export const ADD_KINDS = ['access', 'repo', 'script', 'skill', 'mcp'] as const;
export type AddKind = (typeof ADD_KINDS)[number];

/** The order of the groups in a layer; a document (made elsewhere) is shown last. */
export const GROUP_ORDER: readonly CatalogKind[] = [
  'access',
  'repo',
  'script',
  'skill',
  'mcp',
  'plugin',
  'claude-settings',
  'doc',
];

export const isAddKind = (k: CatalogKind): k is AddKind => (ADD_KINDS as readonly CatalogKind[]).includes(k);

interface KindMeta {
  icon: string;
  one: string;
  many: string;
  about: string;
  newT: string;
}
export const KIND: Record<CatalogKind, KindMeta> = {
  access: {
    icon: 'key-round',
    one: 'Доступ',
    many: 'Доступы',
    about: 'GitLab или GitHub — каждый входит своим аккаунтом',
    newT: 'Новый доступ',
  },
  repo: {
    icon: 'folder-git-2',
    one: 'Репозиторий',
    many: 'Репозитории',
    about: 'Склонируем на машину каждого',
    newT: 'Новый репозиторий',
  },
  script: {
    icon: 'file-code',
    one: 'Скрипт',
    many: 'Скрипты',
    about: 'Запустим на машине: файл из репозитория, команды или файл',
    newT: 'Новый скрипт',
  },
  skill: {
    icon: 'sparkles',
    one: 'Скилл',
    many: 'Скиллы',
    about: 'Инструкция для агента: ссылкой, файлом или текстом',
    newT: 'Новый скилл',
  },
  mcp: {
    icon: 'plug',
    one: 'MCP',
    many: 'MCP',
    about: 'Подключит к агенту сервис: по адресу, командой или конфигом',
    newT: 'Новый MCP',
  },
  plugin: {
    icon: 'blocks',
    one: 'Плагин',
    many: 'Плагины',
    about: 'Плагин Claude Code: скилы, хуки и MCP вместе',
    newT: 'Новый плагин',
  },
  'claude-settings': {
    icon: 'sliders-horizontal',
    one: 'Настройки Claude',
    many: 'Настройки Claude',
    about: 'Ключи settings.json и правила в CLAUDE.md',
    newT: 'Новые настройки',
  },
  doc: {
    icon: 'book-open',
    one: 'Документ',
    many: 'Документы',
    about: 'Агент читает перед работой',
    newT: 'Новый документ',
  },
};

/** The ways an item of a kind comes from, as the board's «Откуда». */
export const SOURCES = {
  skill: [
    ['link', 'Ссылка'],
    ['file', 'Файл'],
    ['text', 'Написать здесь'],
  ],
  mcp: [
    ['url', 'Адрес'],
    ['cmd', 'Команда'],
    ['json', 'Конфиг JSON'],
  ],
  script: [
    ['repo', 'Из репозитория'],
    ['text', 'Команды здесь'],
    ['file', 'Загрузить файл'],
    ['link', 'По ссылке'],
  ],
} as const;

// ── the draft ─────────────────────────────────────────────────────────────────────────────────────────────────────

/** Every field of every kind as the form holds it: text, so an empty box is an empty string. */
export interface Fields {
  src: string;
  name: string;
  about: string;
  url: string;
  branch: string;
  dir: string;
  svc: 'gitlab' | 'github';
  host: string;
  body: string;
  fileName: string;
  cmd: string;
  json: string;
  auth: 'own' | 'shared' | 'none';
  secret: string;
  repo: string; // the item id of a repository of the list
  repoOther: boolean;
  repoUrl: string;
  path: string;
  run: string;
  rerun: 'once' | 'check';
  check: string;
  secrets: string[];
}

export interface Draft {
  k: AddKind;
  f: Fields;
}

/** A repository of the list a script may be run from. */
export interface RepoRef {
  id: string;
  name: string;
}

const GITLAB_HOST = 'gitlab.com';
const GITHUB_HOST = 'github.com';

export const blankFields = (k: AddKind, repos: readonly RepoRef[] = []): Fields => ({
  src: { skill: 'link', mcp: 'url', script: 'repo', repo: '', access: '' }[k],
  name: '',
  about: '',
  url: '',
  branch: '',
  dir: '',
  svc: 'gitlab',
  host: '',
  body: '',
  fileName: '',
  cmd: '',
  json: '',
  auth: k === 'mcp' ? 'own' : 'none',
  secret: '',
  repo: repos[0]?.id ?? '',
  repoOther: false,
  repoUrl: '',
  path: '',
  run: '',
  rerun: 'once',
  check: '',
  secrets: [],
});

export const blankDraft = (k: AddKind, repos: readonly RepoRef[] = []): Draft => ({
  k,
  f: blankFields(k, repos),
});

// ── small text helpers ────────────────────────────────────────────────────────────────────────────────────────────

const has = (v: string): boolean => v.trim() !== '';
export const lastSeg = (u: string): string =>
  (u.split('/').filter(Boolean).pop() ?? '').replace(/\.git$/, '').replace(/^.*:/, '');
export const pretty = (x: string): string => {
  const t = x
    .replace(/\.git$/, '')
    .replace(/\.(sh|py|js)$/, '')
    .replace(/[-_]+/g, ' ')
    .trim();
  return t.charAt(0).toUpperCase() + t.slice(1);
};
export const short = (x: string, n = 46): string => {
  const t = x.replace(/\s+/g, ' ').trim();
  return t.length > n ? `${t.slice(0, n - 1)}…` : t;
};

/** brew has no place on a Linux machine; the hub refuses such a script, the form says so before. */
export const hasBrew = (x: string): boolean => /(^|[\s;&|])brew\s/m.test(x);

/** npm-token → NPM_TOKEN: the variable a script (or an MCP server) gets a secret of the safe in. */
export const secretVar = (name: string): string => {
  const v = name.toUpperCase().replace(/[^A-Z0-9]/g, '_');
  return /^[0-9]/.test(v) ? `_${v}` : v;
};

const withScheme = (u: string): string => {
  const t = u.trim();
  if (t === '' || /^(https?:\/\/|ssh:\/\/|git@)/.test(t)) return t;
  return `https://${t}`;
};
const noScheme = (u: string): string =>
  u
    .replace(/^https?:\/\//, '')
    .replace(/\.git$/, '')
    .replace(/\/+$/, '');

const repoName = (url: string): string => lastSeg(noScheme(url)) || 'репозиторий';

/** The command the file is run with when the person wrote none. */
export const runOf = (f: Fields): string => {
  let file = '';
  if (f.src === 'repo') file = f.path.trim();
  if (f.src === 'file') file = f.fileName.trim();
  if (f.src === 'link' && has(f.url)) {
    const n = lastSeg(f.url);
    file = /\.\w+$/.test(n) ? n : 'script.sh';
  }
  if (file === '') return '';
  const bin = /\.py$/.test(file) ? 'python3' : /\.(js|mjs)$/.test(file) ? 'node' : 'bash';
  return `${bin} ${file}`;
};

const repoOf = (f: Fields, repos: readonly RepoRef[]): string =>
  f.repoOther ? repoName(f.repoUrl) : (repos.find((r) => r.id === f.repo)?.name ?? '');

// ── what the board says about an item ─────────────────────────────────────────────────────────────────────────────

export interface ItemView {
  name: string;
  hint: string;
  /** «Что сделает» */
  does: string;
  /** The lines of the view-only window. */
  lines: [string, string][];
}

interface Parsed {
  name: string;
  url: string;
  command: string;
  args: string[];
  sse: boolean;
}
/** The first server of an `mcpServers` config; null: not a config. */
export const parseMcpJson = (raw: string): Parsed | null => {
  try {
    const data: unknown = JSON.parse(raw);
    if (typeof data !== 'object' || data === null) return null;
    const servers = (data as { mcpServers?: unknown }).mcpServers;
    if (typeof servers !== 'object' || servers === null) return null;
    const [name, cfg] = Object.entries(servers)[0] ?? [];
    if (!name || typeof cfg !== 'object' || cfg === null) return null;
    const c = cfg as { url?: unknown; command?: unknown; args?: unknown; type?: unknown };
    const url = typeof c.url === 'string' ? c.url : '';
    const command = typeof c.command === 'string' ? c.command : '';
    if (url === '' && command === '') return null;
    const args = Array.isArray(c.args) ? c.args.filter((a): a is string => typeof a === 'string') : [];
    return { name, url, command, args, sse: c.type === 'sse' };
  } catch {
    return null;
  }
};

const guessMcpName = (f: Fields): string => {
  if (f.src === 'url')
    return (
      noScheme(f.url)
        .split('/')[0]
        ?.replace(/^mcp\./, '')
        .split('.')[0] ?? ''
    );
  if (f.src === 'json') return parseMcpJson(f.json)?.name ?? '';
  const tok = f.cmd.split(/\s+/).find((x) => /mcp|server/i.test(x)) ?? '';
  const m = tok.match(/^@([^/]+)\/([^@]+)/);
  const pkg = m
    ? /^(mcp|server|mcp-server)$/i.test(m[2] ?? '')
      ? (m[1] ?? '')
      : (m[2] ?? '')
    : (tok
        .replace(/@[^/]*$/, '')
        .split('/')
        .pop() ?? '');
  return pkg.replace(/(^|-)(mcp|server)(?=-|$)/gi, '').replace(/^-+|-+$/g, '');
};

/** The name of the item: what the person wrote, else what the source says. */
export const nameOf = (d: Draft): string => {
  const f = d.f;
  const typed = f.name.trim();
  if (d.k === 'access') return f.svc === 'github' ? 'GitHub' : 'GitLab';
  if (d.k === 'repo') return repoName(f.url);
  if (d.k === 'skill') {
    if (typed) return typed;
    if (f.src === 'file') {
      const m = f.body.match(/^name:\s*(.+)$/m);
      return pretty(m?.[1]?.trim() ?? lastSeg(f.fileName).replace(/\.(zip|md)$/i, '')) || 'Скилл';
    }
    return (f.src === 'link' ? pretty(lastSeg(f.url)) : '') || 'Скилл';
  }
  if (d.k === 'mcp') return typed || pretty(guessMcpName(f)) || 'MCP';
  if (typed) return typed;
  if (f.src === 'text') return 'Команды';
  if (f.src === 'file') return pretty(f.fileName) || 'Скрипт';
  if (f.src === 'link') return pretty(lastSeg(f.url)) || 'Скрипт';
  return pretty(f.path.split('/').pop() ?? '') || 'Скрипт';
};

const mcpAuth = (f: Fields): string => {
  const remote = f.src === 'url';
  if (f.auth === 'shared') return `общий ключ ${f.secret || '…'}`;
  if (f.auth === 'none') return remote ? 'без входа' : 'без ключа';
  return remote ? 'вход своим аккаунтом' : 'ключ у каждого свой';
};
const MCP_AUTHS: Fields['auth'][] = ['own', 'shared', 'none'];

/** The part of the line under the name that comes from the source; what the person wrote about the item goes before it. */
const hintTail = (d: Draft, repos: readonly RepoRef[]): string => {
  const f = d.f;
  if (d.k === 'repo') return `ветка ${f.branch.trim() || 'main'}`;
  if (d.k === 'access') return `${f.svc === 'github' ? 'gh' : 'glab'} и git · каждый входит своим аккаунтом`;
  if (d.k === 'skill') {
    if (f.src === 'file') return `файл ${lastSeg(f.fileName) || '…'}`;
    if (f.src === 'text') return 'написан здесь';
    return 'обновляется сам';
  }
  if (d.k === 'mcp') return mcpAuth(f);
  const n = f.body.split('\n').filter((x) => x.trim()).length;
  if (f.src === 'text') return `${n} ${plural(n, 'команда', 'команды', 'команд')} · при настройке машины`;
  if (f.src === 'file') return `файл ${f.fileName || '…'} · при настройке машины`;
  if (f.src === 'link') return 'по ссылке · при настройке машины';
  const rp = repoOf(f, repos);
  return `${f.path.trim() || '…'}${rp ? ` из ${rp}` : ''} · после клона`;
};

/** The line under the name in a row. A repository leads with its path. */
export const hintOf = (d: Draft, repos: readonly RepoRef[] = []): string => {
  const tail = hintTail(d, repos);
  if (d.k === 'repo') {
    const raw = noScheme(d.f.url);
    const parts = raw.split('/');
    const path = parts.length > 1 ? parts.slice(1).join('/') : raw;
    return `${path} · ${tail}`;
  }
  const about = d.f.about.trim();
  return about ? `${about} · ${tail}` : tail;
};

/** What the person wrote about the item, taken back out of the line the hub keeps. */
const aboutFrom = (hint: string, tails: string[]): string => {
  for (const t of tails) {
    if (hint === t) return '';
    if (hint.endsWith(` · ${t}`)) return hint.slice(0, -(t.length + 3));
  }
  return '';
};

export const viewOf = (d: Draft, repos: readonly RepoRef[] = []): ItemView => {
  const f = d.f;
  const name = nameOf(d);
  const hint = hintOf(d, repos);
  if (d.k === 'repo') {
    const raw = noScheme(f.url);
    const br = f.branch.trim() || 'main';
    const dir = f.dir.trim() || `~/code/${repoName(f.url)}`;
    return {
      name,
      hint,
      does: `склонирует ${raw || '…'} в ${dir}, ветка ${br}. Свежий код человек подтягивает сам`,
      lines: [
        ['Адрес', raw],
        ['Ветка', br],
        ['Папка на машине', dir],
        ['Обновления', 'Свежий код человек подтягивает сам'],
      ],
    };
  }
  if (d.k === 'access') {
    const tool = f.svc === 'github' ? 'gh' : 'glab';
    const host = f.host.trim() || (f.svc === 'github' ? GITHUB_HOST : GITLAB_HOST);
    return {
      name,
      hint,
      does: `поставит ${tool} и попросит человека войти в ${host} своим аккаунтом. Токен ляжет в его личный сейф`,
      lines: [
        ['Адрес', host],
        ['Вход', 'Каждый своим аккаунтом'],
        ['Токен', 'В личном сейфе человека, в окружении его нет'],
      ],
    };
  }
  if (d.k === 'skill') {
    if (f.src === 'file')
      return {
        name,
        hint,
        does: 'положит скилл из файла в чаты Claude Code и Codex. Новая версия — загрузите файл заново, машины подтянут сами',
        lines: [
          ['Откуда', `Файл ${f.fileName}`],
          ['Текст', short(f.body, 140)],
          ['Обновления', 'Загрузите новый файл — машины подтянут сами'],
        ],
      };
    if (f.src === 'text')
      return {
        name,
        hint,
        does: 'положит этот текст как SKILL.md в чаты Claude Code и Codex. Поменяете здесь — у всех обновится само',
        lines: [
          ['Откуда', 'Написан здесь, в окружении'],
          ['Текст', short(f.body, 140)],
          ['Обновления', 'Поменяли здесь — поменяется у всех'],
        ],
      };
    return {
      name,
      hint,
      does: 'положит скилл из ссылки в чаты Claude Code и Codex. Поменяете файл по ссылке — у всех обновится само',
      lines: [
        ['Откуда', f.url],
        ['Обновления', 'Сами: поменяли по ссылке — поменяется у всех'],
      ],
    };
  }
  if (d.k === 'mcp') {
    const remote = f.src === 'url';
    const how =
      f.auth === 'shared'
        ? `Общий ключ из сейфа: ${f.secret || '…'}`
        : f.auth === 'none'
          ? 'Не нужен'
          : remote
            ? 'При первом вызове агент попросит человека войти своим аккаунтом'
            : 'Каждый один раз положит свой ключ — спросим на главной';
    let what = `подключит ${name} к агенту по вставленному конфигу`;
    let src: [string, string] = ['Конфиг', short(f.json, 140)];
    if (remote) {
      what = `подключит ${name} к агенту по адресу ${f.url || '…'}`;
      src = ['Адрес', f.url];
    }
    if (f.src === 'cmd') {
      what = `запустит «${short(f.cmd, 60) || '…'}» на машине и подключит ${name} к агенту`;
      src = ['Команда', f.cmd];
    }
    return { name, hint, does: `${what}. ${how}`, lines: [src, ['Вход', how]] };
  }
  // a script
  const check = f.rerun === 'check' ? f.check.trim() : '';
  const chk = check
    ? `Сначала проверит «${check}»: проходит — уже готово, не запускает`
    : 'На каждой машине — один раз';
  const run = f.run.trim() || runOf(f);
  const runLine: [string, string] = ['Запуск', run || '—'];
  const runQ = short(run.replace(/\s*\n\s*/g, '; '), 60);
  const secs = f.secrets.map((x) => `$${secretVar(x)}`);
  const secTxt = secs.length ? ` Токены из сейфа подставит в ${secs.join(', ')}.` : '';
  const secLine: [string, string] = [
    'Токены и пароли',
    secs.length ? `${secs.join(', ')} — из сейфа` : 'Не нужны',
  ];
  const chkLine: [string, string] = [
    'Повтор',
    check ? `Если не готово: ${check} не проходит — запускаем снова` : 'Один раз на каждой машине',
  ];
  if (f.src === 'text')
    return {
      name,
      hint,
      does: `выполнит команды по порядку при настройке машины. ${chk}.${secTxt}`,
      lines: [['Команды', f.body], ['Когда', 'При настройке машины'], secLine, chkLine],
    };
  if (f.src === 'file')
    return {
      name,
      hint,
      does: `запустит «${runQ || '…'}» при настройке машины. ${chk}.${secTxt}`,
      lines: [
        ['Файл', f.fileName],
        runLine,
        ['Когда', 'При настройке машины'],
        secLine,
        chkLine,
        ['Обновления', 'Загрузите новый файл — машины подтянут сами'],
      ],
    };
  if (f.src === 'link')
    return {
      name,
      hint,
      does: `скачает ${f.url || '…'} и запустит «${runQ || '…'}» при настройке машины. ${chk}.${secTxt}`,
      lines: [
        ['Ссылка', f.url],
        runLine,
        ['Когда', 'При настройке машины'],
        secLine,
        chkLine,
        ['Обновления', 'Поменяли по ссылке — в следующий раз скачает новый'],
      ],
    };
  const rp = repoOf(f, repos);
  return {
    name,
    hint,
    does: `склонирует ${rp || 'репозиторий'} и запустит в его папке «${runQ || '…'}». ${chk}.${secTxt}`,
    lines: [
      ['Файл', `${f.path}${rp ? ` из репозитория ${rp}` : ''}`],
      runLine,
      ['Когда', 'После клона репозитория'],
      secLine,
      chkLine,
    ],
  };
};

// ── valid or not ──────────────────────────────────────────────────────────────────────────────────────────────────

const scriptSourceOk = (f: Fields): boolean => {
  if (f.src === 'repo') return has(f.path) && (f.repoOther ? has(f.repoUrl) : has(f.repo));
  if (f.src === 'text' || f.src === 'file') return has(f.body);
  return /^https?:\/\/\S+/.test(withScheme(f.url));
};

export const valid = (d: Draft): boolean => {
  const f = d.f;
  if (d.k === 'repo') return /\S+[/:]\S+/.test(f.url);
  if (d.k === 'access') return true;
  if (d.k === 'skill') {
    if (f.src === 'link') return has(f.url);
    if (f.src === 'file') return has(f.body);
    return has(f.name) && has(f.body);
  }
  if (d.k === 'mcp') {
    const source = { url: has(f.url), cmd: has(f.cmd), json: parseMcpJson(f.json) !== null }[
      f.src as 'url' | 'cmd' | 'json'
    ];
    return source && (f.auth !== 'shared' || has(f.secret));
  }
  if (hasBrew(f.body) || hasBrew(f.run)) return false;
  if (f.rerun === 'check' && !has(f.check)) return false;
  return scriptSourceOk(f);
};

/** What is still to fill in, as the sentence in place of «Что сделает». */
export const need = (d: Draft): string => {
  const f = d.f;
  let t = d.k === 'repo' ? 'адрес репозитория' : 'адрес GitLab или GitHub';
  if (d.k === 'skill') {
    t = {
      link: 'ссылку на папку со скиллом',
      file: 'файл скилла',
      text: has(f.name) ? 'текст скилла' : 'название и текст скилла',
    }[f.src as 'link' | 'file' | 'text'];
  }
  if (d.k === 'mcp') {
    const filled = { url: f.url, cmd: f.cmd, json: parseMcpJson(f.json) ? 'x' : '' }[
      f.src as 'url' | 'cmd' | 'json'
    ];
    t = has(filled)
      ? 'ключ из сейфа'
      : { url: 'адрес MCP', cmd: 'команду запуска', json: 'конфиг MCP' }[f.src as 'url' | 'cmd' | 'json'];
  }
  if (d.k === 'script') {
    if (hasBrew(f.body) || hasBrew(f.run))
      return 'Уберите brew: на Linux-машинах его нет — поставьте через apt или curl';
    t = {
      repo: (f.repoOther ? has(f.repoUrl) : has(f.repo)) ? 'путь к файлу скрипта' : 'ссылку на репозиторий',
      text: 'команды',
      file: 'файл скрипта',
      link: 'ссылку на скрипт',
    }[f.src as 'repo' | 'text' | 'file' | 'link'];
    if (scriptSourceOk(f)) t = 'проверку, что уже готово';
  }
  return `Укажите ${t} — здесь появится, что машина сделает`;
};

// ── the draft and the catalog's source ────────────────────────────────────────────────────────────────────────────

const orUndef = (v: string): string | undefined => (has(v) ? v.trim() : undefined);

/** The `source` the hub stores: only the keys of the kind, nothing secret. */
export const toSource = (d: Draft): CatalogSource => {
  const f = d.f;
  if (d.k === 'repo') return { url: withScheme(f.url), branch: orUndef(f.branch), dir: orUndef(f.dir) };
  if (d.k === 'access') return { service: f.svc, host: orUndef(f.host) };
  if (d.k === 'skill') {
    if (f.src === 'link') return { ref: withScheme(f.url) };
    return { body: f.body };
  }
  if (d.k === 'mcp') {
    const secretRefs = f.auth === 'shared' ? [secretVar(f.secret)] : undefined;
    if (f.src === 'url') return { transport: 'http', url: withScheme(f.url), secretRefs };
    if (f.src === 'cmd') {
      const [command = '', ...args] = f.cmd.trim().split(/\s+/);
      return { transport: 'stdio', command, args, secretRefs };
    }
    const p = parseMcpJson(f.json);
    if (p?.url) return { transport: p.sse ? 'sse' : 'http', url: p.url, secretRefs };
    return { transport: 'stdio', command: p?.command ?? '', args: p?.args ?? [], secretRefs };
  }
  const from: ScriptFrom = f.src === 'link' ? 'url' : (f.src as ScriptFrom);
  const out: CatalogSource = {
    from,
    run: orUndef(f.run),
    rerun: f.rerun,
    check: f.rerun === 'check' ? orUndef(f.check) : undefined,
    secrets: f.secrets.length ? f.secrets : undefined,
  };
  if (from === 'text') out.body = f.body;
  if (from === 'file') {
    out.body = f.body;
    out.fileName = f.fileName;
  }
  if (from === 'url') out.url = withScheme(f.url);
  if (from === 'repo') {
    out.path = f.path.trim();
    if (f.repoOther) out.repoUrl = withScheme(f.repoUrl);
    else out.repo = f.repo;
  }
  return out;
};

/** The draft of an item of the catalog, to edit it. */
export const fromItem = (it: CatalogItem): Draft | null => {
  if (!isAddKind(it.kind)) return null;
  const s = it.source;
  const d = blankDraft(it.kind);
  const f = d.f;
  if (it.kind === 'repo') {
    f.url = noScheme(s.url ?? '');
    f.branch = s.branch ?? '';
    f.dir = s.dir ?? '';
  }
  if (it.kind === 'access') {
    f.svc = s.service ?? 'gitlab';
    f.host = s.host ?? '';
  }
  if (it.kind === 'skill') {
    f.name = it.name;
    if (s.ref) {
      f.src = 'link';
      f.url = noScheme(s.ref);
    } else {
      f.src = 'text';
      f.body = s.body ?? '';
    }
    f.about = aboutFrom(it.hint, ['обновляется сам', 'написан здесь']);
  }
  if (it.kind === 'mcp') {
    f.name = it.name;
    if (s.transport === 'stdio') {
      f.src = 'cmd';
      f.cmd = [s.command ?? '', ...(s.args ?? [])].join(' ').trim();
    } else {
      f.src = 'url';
      f.url = s.url ?? '';
    }
    f.auth = s.secretRefs?.length ? 'shared' : f.src === 'url' ? 'own' : 'none';
    f.secret = s.secretRefs?.[0] ?? '';
    const tails = MCP_AUTHS.map((a) => mcpAuth({ ...f, auth: a }));
    if (f.auth !== 'shared')
      tails.push(...MCP_AUTHS.map((a) => mcpAuth({ ...f, auth: a, src: f.src === 'url' ? 'cmd' : 'url' })));
    f.about = aboutFrom(it.hint, tails);
    if (f.auth !== 'shared') {
      // the hint says which of «своим аккаунтом» and «без входа» it was
      const picked = MCP_AUTHS.find(
        (a) => it.hint === mcpAuth({ ...f, auth: a }) || it.hint.endsWith(` · ${mcpAuth({ ...f, auth: a })}`),
      );
      if (picked) f.auth = picked;
    }
  }
  if (it.kind === 'script') {
    f.name = it.name;
    f.src = s.from === 'url' ? 'link' : (s.from ?? 'text');
    f.body = s.body ?? '';
    f.fileName = s.fileName ?? '';
    f.url = s.url ?? '';
    f.repo = s.repo ?? '';
    f.repoOther = !s.repo && !!s.repoUrl;
    f.repoUrl = noScheme(s.repoUrl ?? '');
    f.path = s.path ?? '';
    f.run = s.run ?? '';
    f.rerun = s.rerun ?? 'once';
    f.check = s.check ?? '';
    f.secrets = s.secrets ?? [];
  }
  return d;
};

// ── what a window shows about an item of a kind the form does not edit (a document) ──────────────────────────────

export const viewLinesOf = (it: CatalogItem, repos: readonly RepoRef[]): [string, string][] => {
  const d = fromItem(it);
  if (d) return viewOf(d, repos).lines;
  const s = it.source;
  return [
    ['Адрес', s.url ?? ''],
    ['Текст', short(s.text ?? '', 140)],
  ].filter(([, v]) => v !== '') as [string, string][];
};
