import { LocalStorage } from "@raycast/api";
import { Chat, ChatFilter, Conn, describeError, listChats, ApiError } from "./api";

export type Instance = {
  id: string;
  name: string;
  url: string;
  token: string;
  email: string;
  tokenId: string;
};

const KEY = "instances";

export async function loadInstances(): Promise<Instance[]> {
  const raw = await LocalStorage.getItem<string>(KEY);
  if (!raw) return [];
  try {
    const list = JSON.parse(raw) as Instance[];
    return Array.isArray(list) ? list : [];
  } catch {
    return [];
  }
}

async function saveInstances(list: Instance[]): Promise<void> {
  await LocalStorage.setItem(KEY, JSON.stringify(list));
}

/** Adds an instance; the same url + email replaces the old entry (reconnect). */
export async function addInstance(i: Omit<Instance, "id">): Promise<Instance> {
  const list = await loadInstances();
  const same = list.find((x) => x.url === i.url && x.email === i.email);
  const inst: Instance = {
    ...i,
    id: same?.id ?? `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`,
  };
  await saveInstances([...list.filter((x) => x.id !== inst.id), inst]);
  return inst;
}

export async function removeInstance(id: string): Promise<void> {
  const list = await loadInstances();
  await saveInstances(list.filter((x) => x.id !== id));
}

export const connOf = (i: Instance): Conn => ({ url: i.url, token: i.token });

export type TaggedChat = { instance: Instance; chat: Chat };
export type InstanceError = { instance: Instance; message: string; tokenInvalid: boolean };
export type AllChats = {
  items: TaggedChat[];
  errors: InstanceError[];
  counts: { all: number; attention: number; working: number };
  instances: Instance[];
};

/** Chats of every instance, merged, newest prompt first. One failing hub does not fail the rest. */
export async function loadAllChats(filter: ChatFilter = "all"): Promise<AllChats> {
  const instances = await loadInstances();
  const results = await Promise.all(
    instances.map(async (instance) => {
      try {
        return { instance, res: await listChats(connOf(instance), filter) };
      } catch (e) {
        return { instance, error: e };
      }
    }),
  );
  const out: AllChats = { items: [], errors: [], counts: { all: 0, attention: 0, working: 0 }, instances };
  for (const r of results) {
    if ("error" in r) {
      out.errors.push({
        instance: r.instance,
        message: describeError(r.error),
        tokenInvalid: r.error instanceof ApiError && r.error.tokenInvalid,
      });
      continue;
    }
    out.counts.all += r.res.counts.all;
    out.counts.attention += r.res.counts.attention;
    out.counts.working += r.res.counts.working;
    for (const chat of r.res.chats) out.items.push({ instance: r.instance, chat });
  }
  out.items.sort((a, b) => b.chat.lastPromptAt - a.chat.lastPromptAt);
  return out;
}

const attentionLabel: Record<string, string> = {
  question: "Asks a question",
  permission: "Needs permission",
  plan: "Plan to review",
  unread: "Unread",
};

export const attentionText = (a: string): string => attentionLabel[a] ?? "";
