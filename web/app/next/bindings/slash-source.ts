// What «/» lists over a field, read from the machine: the agent's commands and skills for the chat's folder (the
// catalog the Settings screen shows the skills of), the MCP servers set up for the agent, and the chat's folder for
// «@». Nothing is asked until the palette first opens.
import { findAppMentions } from '@/next/features/app-views/api';
import { useCallback, useMemo, useState } from 'react';
import { useAgentSettings, useCatalog, type SkillCatalog } from '@/next/bindings/settings-data';
import type { ICatalogItem } from '@/next/features/settings/api';
import { listFolder } from '@/next/features/pane/chat-files-api';
import { findChats } from '@/next/features/composer/chat-sources';
import type { ISlashSource } from '@/next/features/composer/slash-palette';
import type { IAppCommands, IMcpRef, ISlashActions } from '@/next/features/composer/slash-model';
import type { ConvId, MachineId, MessageRef, Provider } from '@/next/protocol/v2';

interface IOptions {
  machine: MachineId | null;
  /** The chat whose folder «@» lists; null on the start page. */
  conv: ConvId | null;
  provider: Provider | null;
  /** Where the agent works, so a project's own commands are found. */
  cwd: string | null;
  actions: ISlashActions;
  /** The commands of the apps on in the chat. */
  appCommands?: readonly IAppCommands[];
  /** A chat picked after «@»: the message points at it. */
  onChat: (ref: MessageRef) => void;
  /** Some app on in the chat answers «@» (chat.mentions). */
  appMentions?: boolean;
}

/** The folders that are not the person's files to point at. */
const HIDDEN_FOLDERS: ReadonlySet<string> = new Set(['.git']);

/** A catalog that could not be read leaves the settings and the quick prompts: the palette does not wait for it forever. */
const listOf = (state: SkillCatalog): readonly ICatalogItem[] | null => {
  if (state.k === 'ready') return state.value;
  return state.k === 'failed' ? [] : null;
};

export const useSlashSource = ({
  machine,
  conv,
  provider,
  cwd,
  actions,
  appCommands,
  onChat,
  appMentions = false,
}: IOptions): ISlashSource | null => {
  const [woken, setWoken] = useState(false);
  const wake = useCallback(() => setWoken(true), []);
  const on = woken && machine !== null && provider !== null;
  const catalog = useCatalog(machine ?? '', provider ?? 'claude', on, cwd);
  const settings = useAgentSettings(machine ?? '', provider ?? 'claude', on);

  const list = useMemo(() => listOf(catalog.state), [catalog.state]);
  const mcp = useMemo<IMcpRef[]>(
    () =>
      settings.state.k === 'ready'
        ? settings.state.value.mcp.filter((s) => s.enabled).map((s) => ({ name: s.name, target: s.target }))
        : [],
    [settings.state],
  );

  const lister = useMemo(
    () =>
      machine !== null && conv !== null
        ? async (dir: string, signal: AbortSignal) =>
            (await listFolder(machine, conv, dir, signal)).entries.filter((e) => !HIDDEN_FOLDERS.has(e.name))
        : null,
    [machine, conv],
  );

  // Another chat is a place a message points at; the start page's first message has no such places.
  const chats = useMemo(
    () =>
      machine !== null && conv !== null
        ? (query: string, signal: AbortSignal) => findChats(machine, conv, query, signal)
        : null,
    [machine, conv],
  );

  // The apps' finds for «@»: asked only where an app of the chat answers it.
  const mentions = useMemo(
    () =>
      machine !== null && conv !== null && appMentions
        ? (query: string, signal: AbortSignal) => findAppMentions(machine, conv, query, signal)
        : null,
    [machine, conv, appMentions],
  );

  return useMemo(
    () =>
      machine !== null && provider !== null
        ? {
            provider,
            wake,
            catalog: list,
            mcp,
            appCommands,
            actions,
            starting: conv === null,
            filesScope: conv ?? '',
            listFolder: lister,
            findChats: chats,
            onChat,
            findMentions: mentions,
          }
        : null,
    [machine, provider, wake, list, mcp, appCommands, actions, conv, lister, chats, onChat, mentions],
  );
};
