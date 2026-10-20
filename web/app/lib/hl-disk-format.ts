/** Sizes and speeds of the Disk tab as a person reads them, in the page's language. */
import { locale, t } from '@/lib/i18n';

const UNITS = ['bytes', 'kilobytes', 'megabytes', 'gigabytes', 'terabytes'] as const;
const STEP = 1024;

const fractionDigits = (value: number, unit: number): number => {
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
  const digits = fractionDigits(value, unit);
  const number = new Intl.NumberFormat(locale(), {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
    useGrouping: false,
  }).format(value);
  return `${number} ${t(`lib.hlDiskFormat.${UNITS[unit]}`)}`;
};

export const formatSpeed = (bytesPerSecond: number): string =>
  t('lib.hlDiskFormat.perSecond', { size: formatBytes(bytesPerSecond) });

/** `a/b` + `c` → `a/b/c`; an empty side is the root of the disk. */
export const joinDiskPath = (base: string, name: string): string => [base, name].filter(Boolean).join('/');
