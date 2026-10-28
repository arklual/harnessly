// «Что нового» on its own, for the eye and for screenshots: ?dev=whatsnew. ?theme=light|dark&lang=en|ru pick the look.
// Forgets what was seen on every load, so the card always comes. Never part of a production path.
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

const WhatsNewGallery = () => {
  const { query } = useRouter();
  const theme = query.theme === 'dark' ? 'dark' : 'light';
  pinLocale(query.lang === 'en' ? 'en' : 'ru');
  const portalRef = useRef<HTMLDivElement>(null);
  // Forgotten before the card first looks, so it always comes.
  const [ready] = useState(() => {
    try {
      window.localStorage.removeItem('hx:whats-new');
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
