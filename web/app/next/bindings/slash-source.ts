// What «/» lists over a field, read from the machine: the agent's commands and skills for the chat's folder (the
// catalog the Settings screen shows the skills of), the MCP servers set up for the agent, and the chat's folder for
// «@». Nothing is asked until the palette first opens.
import { useCallback, useMemo, useState } from 'react';
import { useAgentSettings, useCatalog, type SkillCatalog } from '@/next/bindings/settings-data';
import type { ICatalogItem } from '@/next/features/settings/api';
import { listFolder } from '@/next/features/pane/chat-files-api';
import type { ISlashSource } from '@/next/features/composer/slash-palette';
import type { IMcpRef, ISlashActions } from '@/next/features/composer/slash-model';
import type { ConvId, MachineId, Provider } from '@/next/protocol/v2';

interface IOptions {
  machine: MachineId | null;
  /** The chat whose folder «@» lists; null on the start page. */
  conv: ConvId | null;
  provider: Provider | null;
  /** Where the agent works, so a project's own commands are found. */
  cwd: string | null;
  actions: ISlashActions;
}

/** The folders that are not the person's files to point at. */
const HIDDEN_FOLDERS: ReadonlySet<string> = new Set(['.git']);

/** A catalog that could not be read leaves the settings and the quick prompts: the palette does not wait for it forever. */
const listOf = (state: SkillCatalog): readonly ICatalogItem[] | null => {
  if (state.k === 'ready') return state.value;
  return state.k === 'failed' ? [] : null;
};

export const useSlashSource = ({ machine, conv, provider, cwd, actions }: IOptions): ISlashSource | null => {
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

  return useMemo(
    () =>
      machine !== null && provider !== null
        ? {
            provider,
            wake,
            catalog: list,
            mcp,
            actions,
            starting: conv === null,
            filesScope: conv ?? '',
            listFolder: lister,
          }
        : null,
    [machine, provider, wake, list, mcp, actions, conv, lister],
  );
};
