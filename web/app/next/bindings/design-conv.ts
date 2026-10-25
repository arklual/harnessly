// The design's chats on the stores (features/designly/design-conv.ts says which one the column shows): the last chat
// the person worked on each design in and the chats it was opened from, kept in the browser so the design opens on its
// chat at once; the first message of a design without a chat, which creates exactly one conversation however many times
// it is pressed; and the message of that create, shown in the column from the moment it is sent until the new chat's
// own feed has it.
import { useEffect, useState } from 'react';
import { createConversation } from '@/next/commands';
import { failureText } from '@/next/bindings/outcome';
import { createKeyedStore, useKey } from '@/next/core/store';
import {
  designKey,
  pickDesignConv,
  withSeen,
  type IDesignBinding,
  type TDesignConv,
} from '@/next/features/designly/design-conv';
import type { IModelChoice } from '@/next/features/composer/agents';
import type { AttachmentRef, ClientId, ConvId, MachineId } from '@/next/protocol/v2';
import { useConversationList, useConvsAnswered } from '@/next/stores/conversations';
import { goTo, ROUTE } from '@/next/routes/routes';
import { t } from '@/next/i18n';
import { toast } from '@/next/ui/toast';

const STORAGE_KEY = 'hx-next-design-convs';

/** The last chat of each design. */
export const designBindingStore = createKeyedStore<string, IDesignBinding>();
/** The chats each design was opened from or worked in, newest first. */
export const designSeenStore = createKeyedStore<string, readonly string[]>();

const NO_SEEN: readonly string[] = [];

let restored = false;
/** What the browser kept, read once. An older page kept only the last chat, as a bare id. */
const restore = () => {
  if (restored) return;
  restored = true;
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? 'null');
    if (typeof raw !== 'object' || raw === null) return;
    for (const [k, value] of Object.entries(raw as Record<string, unknown>)) {
      const kept =
        typeof value === 'string' ? { last: value, seen: [value] } : (value as Record<string, unknown>);
      const last = typeof kept?.last === 'string' ? kept.last : null;
      const seen = Array.isArray(kept?.seen)
        ? kept.seen.filter((x): x is string => typeof x === 'string')
        : [];
      if (last && !designBindingStore.has(k)) designBindingStore.set(k, { id: last, fresh: false });
      if (seen.length > 0 && !designSeenStore.has(k)) designSeenStore.set(k, seen);
    }
  } catch {
    // Private mode or a broken value: the designs open on a new chat.
  }
};

const keep = () => {
  const all: Record<string, { last: string | null; seen: readonly string[] }> = {};
  for (const k of new Set([...designBindingStore.keys(), ...designSeenStore.keys()]))
    all[k] = { last: designBindingStore.get(k)?.id ?? null, seen: designSeenStore.get(k) ?? NO_SEEN };
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(all));
  } catch {
    // Full or blocked storage: it lives for this page.
  }
};

/** Makes a chat the design's last one (null: none, the next message starts a new one); it joins the design's chats. */
export const bindDesignConv = (key: string, binding: IDesignBinding | null): void => {
  restore();
  if (binding) {
    designBindingStore.set(key, binding);
    designSeenStore.set(key, withSeen(designSeenStore.get(key) ?? NO_SEEN, binding.id));
  } else designBindingStore.delete(key);
  keep();
};

export const useDesignSeen = (key: string): readonly string[] => {
  restore();
  return useKey(designSeenStore, key) ?? NO_SEEN;
};

/** Only for tests: forget everything, what the browser kept included. */
export const resetDesignConvs = (): void => {
  for (const k of [...designBindingStore.keys()]) designBindingStore.delete(k);
  for (const k of [...designSeenStore.keys()]) designSeenStore.delete(k);
  for (const k of [...creatingStore.keys()]) creatingStore.delete(k);
  restored = false;
};

/** A design's first message on its way: the create is asked, its conversation not known yet or its feed not read. */
export interface IDesignCreating {
  clientId: ClientId;
  conv: ConvId | null;
}

export const creatingStore = createKeyedStore<string, IDesignCreating>();

export const useDesignCreating = (key: string | null) => useKey(creatingStore, key);

export interface IDesignFirst {
  machine: MachineId;
  key: string;
  dir: string;
  choice: IModelChoice | null;
  text: string;
  attachments: AttachmentRef[];
  /** The words go back to the field: the machine refused the create. */
  onRefused: (text: string) => void;
}

/**
 * The design's first message: one conversation in the design's folder, bound to the design the moment the machine
 * names it. A second press while it is being made does nothing (null), so a design never gets two chats from one start.
 * The client id of the message, for its bubble.
 */
export const startDesignConv = ({
  machine,
  key,
  dir,
  choice,
  text,
  attachments,
  onRefused,
}: IDesignFirst): ClientId | null => {
  if (creatingStore.has(key)) return null;
  const created = createConversation(machine, {
    where: { cwd: dir },
    provider: choice?.provider ?? 'claude',
    ...(choice?.profile ? { profile: choice.profile } : {}),
    model: choice?.model ? { id: choice.model, effort: choice.effort } : null,
    first: { text, attachments },
  });
  if (!created.firstClientId) return null;
  const clientId = created.firstClientId;
  creatingStore.set(key, { clientId, conv: null });
  void created.done.then((st) => {
    if (st.k !== 'failed') return;
    creatingStore.delete(key);
    onRefused(text);
    toast(t('next.newChat.failed'), { tone: 'error', description: failureText(st.failure) });
  });
  void created.conversation.then((id) => {
    if (!id) return;
    bindDesignConv(key, { id, fresh: true });
    creatingStore.patch(key, (have) => have && { ...have, conv: id });
  });
  return clientId;
};

/** The create is over for the column: the new chat's feed shows its message itself. */
export const settleDesignCreating = (key: string): void => creatingStore.delete(key);

export interface IDesignConv {
  picked: TDesignConv;
  /** The person's own pick in the switch: a chat, or null for a new one. It is remembered as the design's last. */
  pick: (id: string | null) => void;
}

/**
 * The design's current chat: the one the person came from (`fromChat`, until they pick another), else the design's
 * last one, else none. Nothing else moves it: not a list read again, not another chat's activity. The chat came from
 * becomes the design's last; a chat made here stops being fresh once the machine's list has it. A pick takes `?chat=`
 * off the address.
 */
export const useDesignConv = (machine: MachineId, design: string, fromChat: string | null): IDesignConv => {
  restore();
  const key = designKey(machine, design);
  const bound = useKey(designBindingStore, key) ?? null;
  const list = useConversationList(machine);
  const answered = useConvsAnswered(machine);
  // The arrival counts until the person picks: a pick leaves the address as it is but is the one shown.
  const [overridden, setOverridden] = useState<string | null>(null);
  const arrived = fromChat !== null && fromChat !== overridden ? fromChat : null;
  const picked = pickDesignConv({ arrived, bound, list, answered });
  const listed = bound !== null && (list?.some((c) => c.id === bound.id) ?? false);
  const settled = bound !== null && bound.fresh && listed;
  const boundId = bound?.id ?? null;
  useEffect(() => {
    if (arrived !== null && arrived !== boundId) bindDesignConv(key, { id: arrived, fresh: false });
  }, [key, arrived, boundId]);
  useEffect(() => {
    if (settled && boundId) bindDesignConv(key, { id: boundId, fresh: false });
  }, [key, settled, boundId]);
  const pick = (id: string | null) => {
    setOverridden(fromChat);
    // The address stops naming the chat it came from: a reload shows the pick (the design's last chat), not the old one.
    if (fromChat !== null) goTo({ kind: ROUTE.site, id: design }, { replace: true });
    bindDesignConv(key, id === null ? null : { id, fresh: false });
  };
  return { picked, pick };
};
