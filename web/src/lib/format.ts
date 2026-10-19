const pad2 = (n: number) => String(n).padStart(2, '0');

/** A running timer: 4:07, 1:02:09. */
export const formatClock = (ms: number): string => {
  const total = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  return h > 0 ? `${h}:${pad2(m)}:${pad2(s)}` : `${m}:${pad2(s)}`;
};

/** A rounded duration: 12 s, 4 min, 2 h 5 min. */
export const formatSpan = (ms: number): string => {
  const total = Math.max(1, Math.round(ms / 1000));
  if (total < 60) return `${total} s`;
  const minutes = Math.floor(total / 60);
  if (minutes < 60) return `${minutes} min`;
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return m > 0 ? `${h} h ${m} min` : `${h} h`;
};

export const formatPct = (value: number): string => `${Math.round(value)}%`;

const GB = 1024 ** 3;
const MB = 1024 ** 2;

export const formatBytes = (bytes: number): string => {
  if (bytes >= GB) return `${(bytes / GB).toFixed(1)} GB`;
  if (bytes >= MB) return `${Math.round(bytes / MB)} MB`;
  return `${Math.round(bytes / 1024)} KB`;
};

/** Seconds of uptime as days and hours. */
export const formatUptime = (sec: number): string => {
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  if (d > 0) return `${d} d ${h} h`;
  const m = Math.floor((sec % 3600) / 60);
  return h > 0 ? `${h} h ${m} min` : `${m} min`;
};

/** When an epoch-seconds moment comes, from now: "in 2 h 5 min". */
export const formatUntil = (epochSec: number, now = Date.now()): string => {
  const ms = epochSec * 1000 - now;
  return ms <= 0 ? 'now' : `in ${formatSpan(ms)}`;
};
