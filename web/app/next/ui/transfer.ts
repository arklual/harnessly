// What a transfer looks like to a person: «34% · 1,2 ГБ из 3,4 ГБ · 12 МБ/с · ещё 3 мин». Shared by the files going
// into a folder of «Файлы» and the files going into a message; what is not known yet is left out.
import { t } from '@/next/i18n';

const UNITS = ['b', 'kb', 'mb', 'gb', 'tb'] as const;
const STEP = 1024;
const MINUTE = 60;
const HOUR = 3600;

const digitsOf = (value: number, unit: number): number => {
  if (unit === 0 || value >= 100) return 0;
  return value >= 10 ? 1 : 2;
};

export const formatBytes = (bytes: number): string => {
  if (!Number.isFinite(bytes) || bytes < 0) return '—';
  let value = bytes;
  let unit = 0;
  while (value >= STEP && unit < UNITS.length - 1) {
    value /= STEP;
    unit += 1;
  }
  const digits = digitsOf(value, unit);
  const number = new Intl.NumberFormat(undefined, {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
    useGrouping: false,
  }).format(value);
  return `${number} ${t(`next.transfer.unit.${UNITS[unit]}`)}`;
};

export const formatSpeed = (bytesPerSecond: number): string =>
  t('next.transfer.perSecond', { size: formatBytes(bytesPerSecond) });

/** «ещё 3 мин»: how long it goes on at the speed it has, rounded up. */
export const formatRemaining = (seconds: number): string => {
  if (!Number.isFinite(seconds) || seconds < 0) return '';
  if (seconds < MINUTE) return t('next.transfer.leftSeconds', { n: Math.max(1, Math.ceil(seconds)) });
  if (seconds < HOUR) return t('next.transfer.leftMinutes', { n: Math.ceil(seconds / MINUTE) });
  const hours = Math.floor(seconds / HOUR);
  return t('next.transfer.leftHours', { h: hours, m: Math.ceil((seconds - hours * HOUR) / MINUTE) % 60 });
};

export interface IProgress {
  sent: number;
  size: number;
  /** Bytes per second; 0 while it is not known yet. */
  speed: number;
}

/** The whole percent it is at; 100 only when every byte is there. */
export const percentOf = ({ sent, size }: IProgress): number =>
  size <= 0 ? 0 : Math.min(sent >= size ? 100 : 99, Math.floor((100 * sent) / size));

export const formatProgress = (progress: IProgress): string => {
  const parts = [
    `${percentOf(progress)}%`,
    t('next.transfer.of', { sent: formatBytes(progress.sent), size: formatBytes(progress.size) }),
  ];
  if (progress.speed > 0) {
    parts.push(formatSpeed(progress.speed));
    parts.push(formatRemaining((progress.size - progress.sent) / progress.speed));
  }
  return parts.filter(Boolean).join(' · ');
};
