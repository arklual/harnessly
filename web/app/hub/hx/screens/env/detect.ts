// «Ссылкой, командой или файлом»: what a pasted line is. A repository, a skill, an MCP server (an address or a config)
// or a command; the person sees what was recognised and may correct it before adding.
import { blankDraft, type Draft, type Fields, type RepoRef } from './model';

export const BAD = 'bad' as const;

const make = (k: Draft['k'], f: Partial<Fields>, repos: readonly RepoRef[]): Draft => {
  const d = blankDraft(k, repos);
  return { k, f: { ...d.f, ...f } };
};

/** null: nothing pasted yet; BAD: not recognised. */
export const detect = (raw: string, repos: readonly RepoRef[] = []): Draft | typeof BAD | null => {
  const s = raw.trim();
  if (s === '') return null;
  if (/^\{[\s\S]*mcpServers/.test(s)) return make('mcp', { src: 'json', json: s, auth: 'none' }, repos);
  if (/^(npx|npm|pnpm|bunx|uvx|pipx?|curl|claude|codex|docker|sh|bash)\s/i.test(s)) {
    if (/mcp/i.test(s) && !/skills?\s+add|plugin\s+(install|add)|--skill/i.test(s))
      return make('mcp', { src: 'cmd', cmd: s, auth: 'none' }, repos);
    // a command of a skill's installation is a script of the machine: it runs once and the skill is in
    const skill = s.match(/--skill\s+(\S+)/);
    return make('script', { src: 'text', body: s, name: skill?.[1] ? skill[1] : '' }, repos);
  }
  const u = s
    .toLowerCase()
    .replace(/^https?:\/\//, '')
    .replace(/\/+$/, '');
  if (/(^|\.)mcp\.|\/mcp$|mcp-server|\bmcp\b/.test(u)) return make('mcp', { src: 'url', url: s }, repos);
  if (/skills?\/|skill\.md$/.test(u))
    return make('skill', { src: 'link', url: s.replace(/\/SKILL\.md$/i, '') }, repos);
  if (/\.(sh|py|js)$/.test(u)) {
    const m = s.replace(/^https?:\/\//, '').match(/^(.+?)\/-\/blob\/[^/]+\/(.+)$/);
    if (m)
      return make('script', { src: 'repo', repoOther: true, repoUrl: m[1] ?? '', path: m[2] ?? '' }, repos);
    return make('script', { src: 'link', url: s }, repos);
  }
  if (/(gitlab|github)\./.test(u) && u.split('/').length >= 3)
    return make('repo', { url: u.replace(/\.git$/, ''), branch: 'main' }, repos);
  return BAD;
};

/** «репозиторий», «скилл · ссылка», «MCP · адрес»: what the recognised thing is, in a word or two. */
export const detWord = (d: Draft): string => {
  const kinds = {
    access: 'доступ',
    repo: 'репозиторий',
    skill: 'скилл',
    mcp: 'MCP',
    script: 'скрипт',
    app: 'приложение',
    plugin: 'плагин',
    'claude-settings': 'настройки Claude',
  } as const;
  const src: Record<string, string> = {
    link: 'ссылка',
    file: 'файл',
    text: 'команды',
    url: 'адрес',
    json: 'конфиг',
    cmd: 'команда',
    repo: 'из репозитория',
  };
  const how = d.k === 'skill' || d.k === 'mcp' || d.k === 'script' ? (src[d.f.src] ?? '') : '';
  return how ? `${kinds[d.k]} · ${how}` : kinds[d.k];
};
