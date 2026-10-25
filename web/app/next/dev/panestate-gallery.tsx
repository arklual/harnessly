// The right pane kept per chat, on the real pane hook and made-up chats: ?dev=panestate with the
// installation's next-ui feature. Two chats on a machine that does not answer (the tabs' contents say so, their places
// and the strip are what matters here). &seed=1 starts chat A with the pane open, wide, on a browser tab beside a
// files tab, and chat B with nothing kept; «Ждать в другом чате» asks the other chat's pane for a site, which it shows
// when the person comes back. Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/icons.css';
import '@/next/styles/ui.css';
import '@/next/styles/shell.css';
import '@/next/styles/header.css';
import '@/next/styles/chat.css';
import '@/next/styles/pane.css';

import { useState } from 'react';
import { useRouter } from '@/platform/router';
import AppFrame from '@/next/features/shell/app-frame';
import { Button } from '@/next/ui/button';
import { useRightPane } from '@/next/bindings/right-pane';
import { chatPanes } from '@/next/stores/pane-state';
import { parseAddress } from '@/next/features/pane/address';
import type { ConvId, Conversation, MachineId } from '@/next/protocol/v2';

const MACHINE = 'gallery-machine' as MachineId;

const chat = (id: string, title: string): Conversation => ({
  id: `tab-${id}` as ConvId,
  machineId: MACHINE,
  projectId: null,
  cwd: `/home/coder/${id}`,
  provider: 'claude',
  model: null,
  title,
  titleSource: 'user',
  pinned: false,
  createdAt: 0,
  lastPromptAt: 0,
  lastActivityAt: 0,
  state: { k: 'ready', since: 0 },
  seenAt: 0,
  lastTurn: null,
  queue: { count: 0, paused: null },
  gate: null,
  rev: 1,
});

const CHATS = [chat('a', 'Чат A — лендинг'), chat('b', 'Чат B — бэкенд')] as const;

let seeded = false;
const seed = () => {
  if (seeded) return;
  seeded = true;
  const address = parseAddress('localhost:4400/afisha');
  chatPanes.update(CHATS[0].id, MACHINE, (p) => ({
    ...p,
    open: true,
    width: 720,
    tabs: [
      { id: 'pa1', kind: 'files', path: 'src/app', file: false },
      { id: 'pa2', kind: 'browser', hist: address ? [address] : [], hi: address ? 0 : -1 },
    ],
    active: 'pa2',
  }));
};

const PaneStateGallery = () => {
  const { query } = useRouter();
  if (query.seed === '1') seed();
  const [at, setAt] = useState(0);
  const conv = CHATS[at];
  const other = CHATS[1 - at];
  const pane = useRightPane(conv, 'gallery', null);

  const sidebar = (
    <nav className="hx-sidebar" aria-label="chats">
      {CHATS.map((c, i) => (
        <Button key={c.id} variant={i === at ? 'primary' : 'ghost'} onClick={() => setAt(i)}>
          {c.title}
        </Button>
      ))}
    </nav>
  );
  return (
    <AppFrame
      rail={false}
      drawerOpen={false}
      onDrawerChange={() => undefined}
      sidebar={sidebar}
      aside={pane.drawer}
    >
      <header className="hx-header">
        <h1 data-testid="chat-title">{conv.title}</h1>
        <Button variant="ghost" onClick={() => pane.header?.onToggle()}>
          {pane.open ? 'Закрыть панель' : 'Открыть панель'}
        </Button>
        <Button
          variant="ghost"
          onClick={() =>
            chatPanes.queue(other.id, MACHINE, { k: 'browser', target: 'example.com', reload: false })
          }
        >
          Ждать в другом чате
        </Button>
      </header>
    </AppFrame>
  );
};

export default PaneStateGallery;
