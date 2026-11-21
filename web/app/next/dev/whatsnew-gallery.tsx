// «Что нового» on its own, for the eye and for screenshots: ?dev=whatsnew. ?theme=light|dark&lang=en|ru pick the look.
// Forgets what was seen on every load, so the card always comes: the entries of the last week untold, a few before them
// told. Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';

import { useEffect, useRef, useState } from 'react';
import { PortalContext } from '@/next/ui/portal';
import { useRouter } from '@/platform/router';
import { pinLocale } from '@/next/i18n';
import WhatsNew from '@/next/features/whats-new/whats-new';
import { ENTRIES } from '@/next/features/whats-new/entries';
import { SEEN_KEY } from '@/next/features/whats-new/seen';

/** Told of everything older than a week before the newest entry. */
const lastWeekUntold = () => {
  const at = new Date(`${ENTRIES[0]?.date ?? '2026-01-01'}T00:00:00Z`);
  at.setUTCDate(at.getUTCDate() - 7);
  return JSON.stringify({ upTo: at.toISOString().slice(0, 10), ids: [] });
};

const WhatsNewGallery = () => {
  const { query } = useRouter();
  const theme = query.theme === 'dark' ? 'dark' : 'light';
  pinLocale(query.lang === 'en' ? 'en' : 'ru');
  const portalRef = useRef<HTMLDivElement>(null);
  // Forgotten before the card first looks, so it always comes.
  const [ready] = useState(() => {
    try {
      window.localStorage.setItem(SEEN_KEY, lastWeekUntold());
    } catch {
      // Storage blocked: the card shows anyway.
    }
    return true;
  });

  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = theme;
    root.classList.toggle('dark', theme === 'dark');
    root.style.colorScheme = theme;
  }, [theme]);

  return (
    <PortalContext value={portalRef}>
      <div className="hx-root">
        <div ref={portalRef} />
        <WhatsNew ready={ready} updating={false} />
      </div>
    </PortalContext>
  );
};

export default WhatsNewGallery;
