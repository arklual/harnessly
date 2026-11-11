// The start page's right pane on the real hook and made-up machines: ?dev=homepane. Its tools (the machine's, the
// services, review) and the machine's choice with two machines; &one=1 with one machine (no choice). The machines do
// not answer here: what a tab shows is not the point, the tiles are. Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/icons.css';
import '@/next/styles/ui.css';
import '@/next/styles/shell.css';
import '@/next/styles/header.css';
import '@/next/styles/chat.css';
import '@/next/styles/pane.css';
import '@/next/styles/menu.css';

import { useState } from 'react';
import { useRouter } from '@/platform/router';
import AppFrame from '@/next/features/shell/app-frame';
import { Button } from '@/next/ui/button';
import { useHomePane } from '@/next/bindings/home-pane';
import { updateHomePane } from '@/next/stores/home-pane-state';
import { machinesStore } from '@/next/stores/machines';
import type { IMachineChoice } from '@/next/features/sidebar/types';
import type { Machine } from '@/next/protocol/v2';

const MACHINES: IMachineChoice[] = [
  { id: 'gallery-1', name: 'runner-2', health: 'ok', note: '', off: false },
  { id: 'gallery-2', name: 'dev-department', health: 'ok', note: '', off: false },
];

// The machines have their shells and their load: the tiles of both are there. Run once, when the gallery opens.
const openGallery = (): true => {
  for (const m of MACHINES)
    machinesStore.set(m.id, {
      machine: { id: m.id, name: m.name } as Machine,
      runtime: 'v2',
      epoch: null,
      features: ['terminal', 'machine'],
      limits: null,
    });
  updateHomePane((s) => ({ ...s, open: true, active: '' }));
  return true;
};

const HomePaneGallery = () => {
  const { query } = useRouter();
  useState(openGallery);
  const machines = query.one === '1' ? MACHINES.slice(0, 1) : MACHINES;
  const pane = useHomePane({ machine: 'gallery-1', machines, conversations: [], enabled: true });
  return (
    <AppFrame
      rail={false}
      drawerOpen={false}
      onDrawerChange={() => undefined}
      sidebar={<nav className="hx-sidebar" aria-label="chats" />}
      aside={pane.drawer}
    >
      <header className="hx-header">
        <Button variant="ghost" onClick={() => pane.header?.onToggle()}>
          {pane.open ? 'Закрыть панель' : 'Открыть панель'}
        </Button>
      </header>
    </AppFrame>
  );
};

export default HomePaneGallery;
