// An agent changed the person's look from a chat (`harnessly theme`, the MCP tool theme_set; cmd/runtime
// routes_looks.go): the machine holds the request, this page puts it on the person's looks (their settings: every
// device gets it) and says so in a notice with «Отменить». The machine learns it through its settings' watch, so no
// page asks more often than it already does. The page also tells the machine its looks, for `harnessly theme get`.
import { useEffect, useEffectEvent } from 'react';
import { getPage, sendBody } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { useSettingsTick } from '@/next/bindings/settings-watch';
import { getLooks, putLook, subscribeLooks } from '@/next/features/looks/store';
import { readLook, type TLookScope } from '@/next/features/looks/types';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { toast } from '@/next/ui/toast';

interface IRequest {
  id: string;
  conv: string;
  scope: 'all' | 'folder' | 'chat';
  look: unknown;
  reset: boolean;
}

const readRequests = (v: unknown): IRequest[] => {
  if (typeof v !== 'object' || v === null || !Array.isArray((v as { requests?: unknown }).requests))
    return [];
  return (v as { requests: unknown[] }).requests.flatMap((raw) => {
    if (typeof raw !== 'object' || raw === null) return [];
    const r = raw as Record<string, unknown>;
    const scope = r.scope === 'folder' || r.scope === 'chat' ? r.scope : 'all';
    return typeof r.id === 'string'
      ? [
          {
            id: r.id,
            conv: typeof r.conv === 'string' ? r.conv : '',
            scope,
            look: r.look,
            reset: r.reset === true,
          },
        ]
      : [];
  });
};

/** Requests this page has taken, so a second read of the list (or another machine's tick) does not apply one twice. */
const taken = new Set<string>();

export interface IAgentLooksProps {
  machine: MachineId;
  /** The folder of a chat, by its id (the sidebar's); null when the chat has none or is unknown. */
  folderOf: (chat: string) => string | null;
}

const done = (machine: MachineId, id: string, state: 'applied' | 'failed', note = '') =>
  sendBody(
    'POST',
    runtimePath(platformPath(`/look-requests/${id}/done`), machine),
    { state, note },
    () => null,
    8000,
  );

/** One machine's agents' requests; mounted per machine the person works with (bindings/shell.tsx). */
const AgentLooks = ({ machine, folderOf }: IAgentLooksProps) => {
  const tick = useSettingsTick(machine);
  const folderOfNow = useEffectEvent((chat: string) => folderOf(chat));
  useEffect(() => {
    let off = false;
    void getPage(runtimePath(platformPath('/look-requests'), machine), readRequests, 8000).then((res) => {
      if (off || !('ok' in res)) return;
      for (const req of res.value) {
        if (taken.has(req.id)) continue;
        taken.add(req.id);
        let scope: TLookScope = { k: 'all' };
        if (req.scope === 'chat') scope = { k: 'chat', id: req.conv };
        if (req.scope === 'folder') {
          const folder = folderOfNow(req.conv);
          if (folder === null) {
            void done(machine, req.id, 'failed', 'the chat is in no folder: use --scope chat or all');
            continue;
          }
          scope = { k: 'folder', id: folder };
        }
        const look = req.reset ? null : readLook(req.look);
        if (!req.reset && look === null) {
          void done(machine, req.id, 'failed', 'the look does not read');
          continue;
        }
        const before = putLook(scope, look);
        void done(machine, req.id, 'applied');
        toast(t(`next.looks.agent.${req.reset ? 'reset' : 'applied'}.${scope.k}`), {
          tone: 'success',
          description: look?.name,
          action: { label: t('next.looks.undo'), onClick: () => putLook(scope, before) },
        });
      }
    });
    return () => {
      off = true;
    };
  }, [machine, tick]);
  // The machine knows the looks for `harnessly theme get`: told on the first tick and on each change.
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | null = null;
    const tell = () => {
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => {
        void sendBody(
          'PUT',
          runtimePath(platformPath('/look-state'), machine),
          { looks: getLooks() },
          () => null,
          8000,
        );
      }, 400);
    };
    tell();
    const unsubscribe = subscribeLooks(tell);
    return () => {
      unsubscribe();
      if (timer) clearTimeout(timer);
    };
  }, [machine]);
  return null;
};

export default AgentLooks;
