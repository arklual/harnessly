// Settings → Notifications as in the app (`?dev=notifsettings`), for the eye, for screenshots and for the video: the
// page on this browser's own switches, the sounds play from ▶.
//
//   &theme=light|dark&lang=ru|en
//   A narrow window (≤ 900 px) shows the phone's page.
import { useEffect, useRef } from 'react';
import { NoticesPage } from '@/next/bindings/settings-notices';
import { unlockSound } from '@/next/features/notifications/ping';
import { pinLocale, t, type TLang } from '@/next/i18n';
import { PortalContext } from '@/next/ui/portal';
import '@/next/styles/tokens.css';
import '@/next/styles/motion.css';
import '@/next/styles/icons.css';
import '@/next/styles/ui.css';
import '@/next/styles/base.css';
import '@/next/styles/keys.css';
import '@/next/styles/settings.css';
import '@/next/dev/notifsettings-gallery.css';

const params = new URLSearchParams(window.location.search);
const LANG: TLang = params.get('lang') === 'en' ? 'en' : 'ru';
pinLocale(LANG);
const THEME = params.get('theme') === 'dark' ? 'dark' : 'light';

const Gallery = () => {
  const portal = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = THEME;
    root.dataset.hxTheme = THEME;
    root.classList.toggle('dark', THEME === 'dark');
    return unlockSound();
  }, []);

  return (
    <PortalContext value={portal}>
      <div className="hx-root hx-nsg">
        <section className="hx-set hx-nsg__set" data-page="true">
          <div className="hx-set__page hx-nsg__page">
            <div className="hx-set__page-in">
              <div className="hx-set__head">
                <div className="hx-set__titles">
                  <h2 className="hx-set__title">{t('next.settings.section.notifications')}</h2>
                  <p className="hx-set__lead">{t('next.settings.lead.notifications')}</p>
                </div>
              </div>
              <NoticesPage />
            </div>
          </div>
        </section>
        <div ref={portal} className="hx-portal" />
      </div>
    </PortalContext>
  );
};

const NotifSettingsGallery = () => <Gallery />;

export default NotifSettingsGallery;
