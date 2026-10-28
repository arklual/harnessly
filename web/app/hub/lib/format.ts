// Small pure helpers behind the screens: sizes, the machine line, proxy URLs, the console link, codes.
import { currentLang, plural, t } from './i18n';
import type { Machine, PreflightReport, ProviderCheck } from './types';

const GIB = 1024 ** 3;

/** «412 ГБ» / «512 МБ» for a size in bytes. */
export const formatBytes = (bytes: number): string => {
  if (bytes >= GIB) return t('fmt.gb', { n: Math.round(bytes / GIB) });
  return t('fmt.mb', { n: Math.max(0, Math.round(bytes / 1024 ** 2)) });
};

/** «Ubuntu 22.04 · 32 ядра · 64 ГБ памяти · 412 ГБ свободно», the line under a machine that connected. */
export const machineLine = (p: PreflightReport): string =>
  [
    p.osName,
    plural(p.cpus, 'fmt.cores'),
    t('fmt.memory', { size: formatBytes(p.memTotal) }),
    t('fmt.free', { size: formatBytes(p.disk.free) }),
  ].join(' · ');

export const viaLabel = (via: Machine['via']): string => t(`via.${via}`);

export const isSocksUrl = (url: string): boolean => /^socks/i.test(url.trim());

/** Whether the text is a proxy URL the agent accepts: http, https, socks5 or socks5h with a host (and a port). */
export const validProxyUrl = (text: string): boolean => {
  const url = text.trim();
  if (!/^(https?|socks5h?):\/\//i.test(url)) return false;
  try {
    const u = new URL(url);
    return u.hostname !== '' && u.pathname.replace(/\//g, '') === '' && u.search === '' && u.hash === '';
  } catch {
    return false;
  }
};

/**
 * Where «Открыть терминал» goes. `openUrl` is the answer of GET /api/machines/<id>/open (path mode: `/m/<id>/`;
 * subdomain mode: a full URL with a one-time `hl_ticket`), `consoleUrl` the login's path under the base path
 * (`/m/<id>/console/<tabId>`, a console page of the runtime). The console's own path wins; the origin and the ticket
 * come from the open URL. The console address is the machine's word: it must stay on the open URL's origin and under
 * `/m/<machineId>/`, or this throws before the ticket is attached.
 */
export const consoleTarget = (
  openUrl: string,
  consoleUrl: string,
  here: string,
  machineId: string,
): string => {
  const open = new URL(openUrl, here);
  // The console address comes from the machine: it stays on the origin the hub sent us to, under this machine's own
  // runtime path. Anything else is refused before the ticket is attached.
  const target = new URL(consoleUrl, open);
  if (target.origin !== open.origin) throw new Error('console address is on another origin');
  if (!target.pathname.startsWith(`/m/${encodeURIComponent(machineId)}/`)) {
    throw new Error('console address is outside the machine');
  }
  const ticket = open.searchParams.get('hl_ticket');
  if (ticket) target.searchParams.set('hl_ticket', ticket);
  return target.origin === new URL(here).origin
    ? `${target.pathname}${target.search}${target.hash}`
    : target.href;
};

/** The count-down text «0:42». */
export const clock = (seconds: number): string => {
  const s = Math.max(0, Math.ceil(seconds));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
};

/** «только что» / «3 мин назад» for a check made at `at` (ms). */
export const checkedAgo = (at: number, now: number): string => {
  const sec = Math.max(0, Math.round((now - at) / 1000));
  if (sec < 60) return t('fmt.justNow');
  return plural(Math.floor(sec / 60), 'fmt.minAgo');
};

/** The «Ответ» line of the provider failure card. */
export const checkAnswer = (c: ProviderCheck): string => {
  if (c.verdict === 'rejected') {
    return c.status ? t('check.answerRejected', { status: c.status }) : t('check.answerRejectedNoStatus');
  }
  return c.detail ?? t('check.answerNetwork');
};

export const isEmailLike = (text: string): boolean => /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/.test(text.trim());

/** Names accepted by env.deliver (docs/hub/api.md): `^[A-Z_][A-Z0-9_]{0,63}$` minus the reserved ones. */
export const envNameProblem = (name: string): 'format' | 'reserved' | null => {
  if (!/^[A-Z_][A-Z0-9_]{0,63}$/.test(name)) return 'format';
  const reserved =
    /^(PATH|HOME|SHELL|USER|LD_.*|DYLD_.*|HTTP_PROXY|HTTPS_PROXY|NO_PROXY|ANTHROPIC_.*|OPENAI_.*|HARNESSLY_.*|RUNTIME_.*|AGENT_.*)$/;
  return reserved.test(name) ? 'reserved' : null;
};

/** The secrets of the access step that have their own rows: not listed again under «Секреты». */
export const GIT_ENV = {
  github: ['GITHUB_TOKEN', 'GH_TOKEN'],
  gitlab: ['GITLAB_TOKEN'],
} as const;

export const ownEnvNames: readonly string[] = [...GIT_ENV.github, ...GIT_ENV.gitlab];

/** The time of day «14:05» of an RFC 3339 instant, in the browser's zone. */
export const hhmm = (iso: string | number): string =>
  new Date(iso).toLocaleTimeString(currentLang(), { hour: '2-digit', minute: '2-digit' });
