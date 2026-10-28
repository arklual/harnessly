// The create screen of a presentation with no machine behind it, for the eye and for screenshots: ?dev=deck-create.
// &theme=light|dark and &lang=en|ru pick the look. Uploads and the template read need a machine: a screenshot script
// answers them (PUT /uploads, POST /design/deck/pptx-style). Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import '@/next/styles/picker.css';
import '@/next/styles/designly.css';
import '@/next/styles/deck.css';

import { useEffect } from 'react';
import { useRouter } from '@/platform/router';
import { pinLocale } from '@/next/i18n';
import DeckCreate from '@/next/features/designly/deck/deck-create';
import type { MachineId } from '@/next/protocol/v2';

const SYSTEMS = [
  { id: 'acme', label: 'Acme', desc: 'Corporate design system' },
  { id: 'kit:hx', label: 'hx-ui', desc: 'UI kit, version 2.4.0' },
  { id: 'shop', label: 'Shop', desc: 'Storefront components' },
];

const DeckCreateGallery = () => {
  const { query } = useRouter();
  const dark = query.theme === 'dark';
  pinLocale(query.lang === 'en' ? 'en' : 'ru');
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = dark ? 'dark' : 'light';
    root.classList.toggle('dark', dark);
    root.style.colorScheme = dark ? 'dark' : 'light';
  }, [dark]);
  return (
    <div className="hx-root hx-gallery-deck">
      <DeckCreate
        machine={'gallery' as MachineId}
        systems={SYSTEMS}
        onBack={() => {}}
        onCreate={() => new Promise<void>(() => {})}
      />
    </div>
  );
};

export default DeckCreateGallery;
