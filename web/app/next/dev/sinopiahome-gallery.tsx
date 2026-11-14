// Sinopia's home on made-up data, for the eye and for screenshots: ?dev=sinopiahome. The folder «UI-киты» first among
// the folders; &open=kits opens it, &kits=loading keeps the kits loading, &kits=none has none,
// &library=loading keeps the whole library loading (its skeleton with the folder in place), &theme=light|dark and
// &lang=en|ru pick the look. The machine does not answer here. Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import '@/next/styles/menu.css';
import '@/next/styles/composer.css';
import '@/next/styles/kit.css';
import '@/next/dev/sinopiahome-gallery.css';

import { useEffect, useState } from 'react';
import { useRouter } from '@/platform/router';
import { pinLocale } from '@/next/i18n';
import DesignlyHome, { type IDesignCard } from '@/next/features/designly/designly-home';
import type { IKitShelfCard } from '@/next/features/designly/kit/kit-api';
import type { ILibraryOrganizer } from '@/next/features/designly/library-organizer';
import type { MachineId } from '@/next/protocol/v2';

const shot = (a: string, b: string) =>
  `data:image/svg+xml;utf8,${encodeURIComponent(
    `<svg xmlns="http://www.w3.org/2000/svg" width="520" height="320"><rect width="520" height="320" fill="${a}"/><rect x="40" y="48" width="280" height="30" rx="8" fill="${b}"/><rect x="40" y="96" width="200" height="18" rx="8" fill="${b}" opacity=".6"/><rect x="40" y="200" width="440" height="72" rx="16" fill="${b}" opacity=".35"/></svg>`,
  )}`;

const card = (id: string, title: string, ago: number, a: string, b: string, folder = ''): IDesignCard => ({
  id,
  title,
  boards: 3,
  updatedAt: Date.now() - ago,
  image: shot(a, b),
  kind: 'site',
  pinned: false,
  drawing: false,
  owner: null,
  folder,
});

const CARDS: IDesignCard[] = [
  card('landing', 'Лендинг мероприятия', 60_000, '#1b1b21', '#7b61ff'),
  card('checkout', 'Оплата заказа', 3_600_000, '#f4f2ee', '#2742e8'),
  card('analytics', 'Аналитика организатора', 86_400_000, '#0f1a24', '#2fb67c'),
  card('scanner', 'Сканер на входе', 172_800_000, '#fff7ed', '#ea580c'),
];

const KITS: IKitShelfCard[] = [
  {
    key: 'acme-ui',
    kit: 'acme-ui',
    version: '1.2.0',
    theme: null,
    title: 'Acme UI',
    kitTitle: '',
    cover: shot('#eef2ff', '#4f46e5'),
    layouts: 12,
  },
  {
    key: 'deck/sun',
    kit: 'deck',
    version: '0.7.1',
    theme: 'sun',
    title: 'Солнце',
    kitTitle: 'Слайды',
    cover: shot('#fefce8', '#ca8a04'),
    layouts: 24,
  },
  {
    key: 'deck/night',
    kit: 'deck',
    version: '0.7.1',
    theme: 'night',
    title: 'Ночь',
    kitTitle: 'Слайды',
    cover: shot('#0b1020', '#38bdf8'),
    layouts: 24,
  },
];

const ORGANIZER: ILibraryOrganizer = {
  folders: [
    { id: 'work', name: 'Работа', parent: '' },
    { id: 'drafts', name: 'Черновики', parent: '' },
  ],
  onFolderSave: () => Promise.resolve(),
  onFolderDelete: () => Promise.resolve(),
  onMove: () => Promise.resolve(),
  onDeleteMany: () => Promise.resolve(),
  trash: [],
  loadTrash: () => Promise.resolve(),
  onRestore: () => Promise.resolve(),
  onPurge: () => Promise.resolve(),
};

const kitsOf = (q: unknown): IKitShelfCard[] | null => {
  if (q === 'loading') return null;
  if (q === 'none') return [];
  return KITS;
};

const SinopiaHomeGallery = () => {
  const { query } = useRouter();
  const dark = query.theme === 'dark';
  pinLocale(query.lang === 'en' ? 'en' : 'ru');
  const [now] = useState(() => Date.now());
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = dark ? 'dark' : 'light';
    root.classList.toggle('dark', dark);
    root.style.colorScheme = dark ? 'dark' : 'light';
  }, [dark]);
  // The folder opens as a person would open it: a click on its tile once the page is drawn.
  useEffect(() => {
    if (query.open !== 'kits') return;
    const timer = window.setTimeout(() => {
      document.querySelector<HTMLButtonElement>('.hx-dl__folders .hx-dl-folder__open')?.click();
    }, 50);
    return () => window.clearTimeout(timer);
  }, [query.open]);
  return (
    <div className="hx-root hx-gallery-sh">
      <DesignlyHome
        cards={query.library === 'loading' ? null : CARDS}
        error={null}
        now={now}
        onPin={() => Promise.resolve()}
        onOpen={() => {}}
        onRetry={() => {}}
        organizer={ORGANIZER}
        compose={{
          machine: 'gallery' as MachineId,
          systems: [],
          onCreate: () => Promise.resolve(),
          brief: '',
          onBrief: () => {},
          uploads: {
            attachments: [],
            ready: [],
            uploading: false,
            onFiles: () => {},
            onRemove: () => {},
            onRetry: () => {},
            reset: () => {},
          },
          picker: null,
          voice: { recording: null, onCancel: () => {}, onFinish: () => {} },
          appKinds: [],
        }}
        kits={{ cards: kitsOf(query.kits), cover: (k) => k.cover, onOpen: () => {} }}
      />
    </div>
  );
};

export default SinopiaHomeGallery;
