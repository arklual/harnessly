import { useEffect, useState } from 'react';
import { t } from '@/lib/i18n';

const format = (ms: number) => {
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s < 60) return t('timeline.elapsedTime.seconds', { s });
  const m = Math.floor(s / 60);
  if (m < 60) return t('timeline.elapsedTime.minutes', { m, s: String(s % 60).padStart(2, '0') });
  return t('timeline.elapsedTime.hours', { h: Math.floor(m / 60), m: String(m % 60).padStart(2, '0') });
};

/** Time since `since` (ms), ticking every second. */
export default function ElapsedTime({ since }: { since: number }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);
  return <>{format(now - since)}</>;
}
