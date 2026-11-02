// Settings → «Клавиши» and the shortcuts sheet on a made-up record (`?dev=keys`), for the eye and for screenshots.
//
//   &view=settings (default) | help
//   &theme=light|dark&lang=en|ru
import { useEffect, useRef, useState } from 'react';
import KeysHelp from '@/next/features/keys/keys-help';
import { KEY_SCOPE } from '@/next/features/keys/commands';
import { reset, withChord, withoutChord, type TKeyOverrides } from '@/next/features/keys/keymap';
import KeysSection, { KEY_CHANGE } from '@/next/features/settings/keys-section';
import { pinLocale, type TLang } from '@/next/i18n';
import { PortalContext } from '@/next/ui/portal';
import '@/next/styles/tokens.css';
import '@/next/styles/motion.css';
import '@/next/styles/icons.css';
import '@/next/styles/ui.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/settings.css';
import '@/next/styles/keys.css';

const params = new URLSearchParams(window.location.search);
const LANG: TLang = params.get('lang') === 'en' ? 'en' : 'ru';
pinLocale(LANG);
const VIEW = params.get('view') ?? 'settings';
const DARK = params.get('theme') === 'dark';

const Gallery = () => {
  const portal = useRef<HTMLDivElement>(null);
  const [overrides, setOverrides] = useState<TKeyOverrides>({ 'deck.first': ['KeyA'], 'global.help': [] });
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = DARK ? 'dark' : 'light';
    root.classList.toggle('dark', DARK);
    root.style.colorScheme = DARK ? 'dark' : 'light';
  }, []);
  return (
    <PortalContext value={portal}>
      <div className="hx-root hx-keys-gallery" data-testid="gallery">
        {VIEW === 'help' ? (
          <KeysHelp
            open
            onClose={() => undefined}
            scopes={[KEY_SCOPE.deck]}
            overrides={overrides}
            onSettings={() => undefined}
          />
        ) : (
          <KeysSection
            overrides={overrides}
            busy={new Set()}
            onRefused={() => undefined}
            onChange={(change) =>
              setOverrides((now) => {
                if (change.k === KEY_CHANGE.set) return withChord(now, change.id, change.chord, change.old);
                if (change.k === KEY_CHANGE.drop) return withoutChord(now, change.id, change.chord);
                return change.id === null ? {} : reset(now, change.id);
              })
            }
          />
        )}
        <div ref={portal} className="hx-portal" />
      </div>
    </PortalContext>
  );
};

const KeysGallery = () => <Gallery />;

export default KeysGallery;
