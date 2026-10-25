// «Поделиться» of a design on made-up data, for the eye and for screenshots: ?dev=share with the
// installation's next-ui feature. The gateway is played in the page: every change answers with the new state, the people
// search knows two people. &role=view opens it for a person who may only look, &theme=light|dark and &lang=en|ru pick
// the look. Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import '@/next/styles/picker.css';
import '@/next/styles/header.css';

import { useEffect, useRef, useState } from 'react';
import { useRouter } from '@/platform/router';
import { pinLocale } from '@/next/i18n';
import ShareDialog from '@/next/features/designly/access/share-dialog';
import type { IProjectAccess, TGrantRole } from '@/next/features/designly/access/api';
import { PortalContext } from '@/next/ui/portal';

const DAY_MS = 86_400_000;
const STEP_MS = { '1h': 3_600_000, '1d': DAY_MS, '7d': 7 * DAY_MS, '30d': 30 * DAY_MS } as const;

const SEED: IProjectAccess = {
  role: 'owner',
  owner: 'peter@example.com',
  ownerName: 'Пётр',
  team: 'view',
  canManage: true,
  editorsCanShare: false,
  request: null,
  people: [
    {
      email: 'ivan@example.com',
      role: 'edit',
      grantedBy: 'peter@example.com',
      grantedAt: '2026-10-20T10:00:00Z',
      expiresAt: null,
      viaInvite: null,
    },
    {
      email: 'oleg@example.com',
      role: 'comment',
      grantedBy: 'peter@example.com',
      grantedAt: '2026-10-21T10:00:00Z',
      expiresAt: '2026-11-12T20:59:59Z',
      viaInvite: null,
    },
    {
      email: 'agency@partner.ru',
      role: 'view',
      grantedBy: 'peter@example.com',
      grantedAt: '2026-10-22T10:00:00Z',
      expiresAt: null,
      viaInvite: 'i1',
    },
  ],
  invites: [
    {
      id: 'i1',
      title: 'Агентство',
      role: 'view',
      roleTtl: '7d',
      expiresAt: '2099-01-01T00:00:00Z',
      maxUses: 5,
      uses: 1,
      revokedAt: null,
    },
  ],
  requests: [
    {
      id: 'r1',
      email: 'bob@partner.ru',
      level: 'edit',
      message: 'Поправлю тексты',
      status: 'open',
      createdAt: '2026-10-24T10:00:00Z',
    },
  ],
};

const VIEWER: IProjectAccess = {
  ...SEED,
  role: 'view',
  canManage: false,
  people: undefined,
  invites: undefined,
  requests: undefined,
};

const LOG = [
  {
    id: 3,
    actor: 'peter@example.com',
    action: 'grant',
    subject: 'oleg@example.com',
    at: '2026-10-21T10:00:00Z',
  },
  { id: 2, actor: 'peter@example.com', action: 'team', subject: 'view', at: '2026-10-20T11:00:00Z' },
  { id: 1, actor: 'peter@example.com', action: 'create', subject: '', at: '2026-10-20T10:00:00Z' },
];

const HITS = [
  { id: 'nastya@example.com', name: 'Настя' },
  { id: 'nikita@example.com', name: 'Никита' },
];

const until = (ttl: unknown): string | null => {
  if (typeof ttl !== 'string') return null;
  const step = STEP_MS[ttl as keyof typeof STEP_MS];
  return step === undefined ? ttl : new Date(Date.now() + step).toISOString();
};

/** The gateway, played in the page: the state lives in `box`, each answer is a fresh copy. */
const playGateway = (box: { access: IProjectAccess }) => {
  const real = window.fetch.bind(window);
  let made = 0;
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const answer = (body: unknown) =>
      new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } });
    if (url.includes('/docs/people')) {
      const q = decodeURIComponent(url.split('q=')[1] ?? '').toLowerCase();
      return answer({ people: HITS.filter((h) => h.id.startsWith(q) || h.name.toLowerCase().startsWith(q)) });
    }
    const at = url.indexOf('/design/access/');
    if (at < 0) return real(input, init);
    const path = url
      .slice(at + '/design/access/'.length)
      .split('/')
      .slice(1);
    const method = init?.method ?? 'GET';
    const body = (init?.body ? JSON.parse(String(init.body)) : {}) as Record<string, unknown>;
    await new Promise((resolve) => setTimeout(resolve, 350));
    const a = box.access;
    const [what, key] = path.map(decodeURIComponent);
    if (what === 'log') return answer({ log: LOG });
    if (what === 'team') box.access = { ...a, team: body.level as IProjectAccess['team'] };
    if (what === 'settings') box.access = { ...a, editorsCanShare: Boolean(body.editorsCanShare) };
    if (what === 'revoke-all') box.access = { ...a, team: 'none', people: [], invites: [] };
    if (what === 'owner') box.access = { ...a, owner: String(body.email), ownerName: '', role: 'manage' };
    if (what === 'people' && key && method === 'PUT') {
      const person = {
        email: key,
        role: body.role as TGrantRole,
        grantedBy: a.owner ?? '',
        grantedAt: new Date().toISOString(),
        expiresAt: until(body.ttl),
        viaInvite: null,
      };
      const people = a.people ?? [];
      box.access = {
        ...a,
        people: people.some((p) => p.email === key)
          ? people.map((p) => (p.email === key ? person : p))
          : [...people, person],
      };
    }
    if (what === 'people' && key && method === 'DELETE')
      box.access = { ...a, people: (a.people ?? []).filter((p) => p.email !== key) };
    if (what === 'requests' && key) box.access = { ...a, requests: [] };
    if (what === 'invites' && key && method === 'DELETE')
      box.access = { ...a, invites: (a.invites ?? []).filter((i) => i.id !== key) };
    if (what === 'invites' && !key) {
      made += 1;
      const id = `n${made}`;
      const invite = {
        id,
        title: String(body.title ?? ''),
        role: body.role as 'view',
        roleTtl: (body.roleTtl as string | null) ?? null,
        expiresAt: until(body.ttl) ?? '2099-01-01T00:00:00Z',
        maxUses: (body.maxUses as number | null) ?? null,
        uses: 0,
        revokedAt: null,
      };
      box.access = { ...a, invites: [...(a.invites ?? []), invite] };
      return answer({
        id,
        token: `tok_${id}`,
        url: `https://ai.example.com/i/tok_${id}`,
        expiresAt: invite.expiresAt,
      });
    }
    return answer(box.access);
  };
};

const ShareGallery = () => {
  const { query } = useRouter();
  const dark = query.theme !== 'light';
  pinLocale(query.lang === 'en' ? 'en' : 'ru');
  const portal = useRef<HTMLDivElement>(null);
  // The gateway is played from the first render on; the dialog shows what it answers.
  const [access, setAccess] = useState(() => {
    const box = { access: query.role === 'view' ? VIEWER : SEED };
    playGateway(box);
    return box.access;
  });
  // Opens once the portal is in the page: a dialog portalled before it would hide the page's root from the menus.
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const frame = requestAnimationFrame(() => setOpen(true));
    return () => cancelAnimationFrame(frame);
  }, []);
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = dark ? 'dark' : 'light';
    root.classList.toggle('dark', dark);
    root.style.colorScheme = dark ? 'dark' : 'light';
  }, [dark]);
  return (
    <PortalContext value={portal}>
      <div className="hx-root">
        <button type="button" className="hx-btn" data-gallery-open onClick={() => setOpen(true)}>
          Поделиться
        </button>
        <ShareDialog
          open={open}
          onClose={() => setOpen(false)}
          project="landing"
          title="Лендинг фестиваля"
          access={access}
          onNext={setAccess}
          onCopyLink={() => undefined}
          onClaim={async () => undefined}
        />
        <div ref={portal} className="hx-portal" />
      </div>
    </PortalContext>
  );
};

export default ShareGallery;
