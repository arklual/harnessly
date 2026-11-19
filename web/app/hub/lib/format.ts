// Small pure helpers behind the screens: sizes, the machine line, codes.
import { currentLang, plural, t } from './i18n';
import type { PreflightReport } from './types';

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

/** The count-down text «0:42». */
export const clock = (seconds: number): string => {
  const s = Math.max(0, Math.ceil(seconds));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
};

export const isEmailLike = (text: string): boolean => /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/.test(text.trim());

/** The time of day «14:05» of an RFC 3339 instant, in the browser's zone. */
export const hhmm = (iso: string | number): string =>
  new Date(iso).toLocaleTimeString(currentLang(), { hour: '2-digit', minute: '2-digit' });
