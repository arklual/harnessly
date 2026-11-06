// The commands of the apps that are on in a chat, as the «/» palette takes them.
import type { IInstalledApp } from '@/next/features/app-views/api';
import type { IAppCommands } from '@/next/features/composer/slash-model';

/** Claude Code's plugin name of an app (internal/apps PluginName): its commands are /<plugin>:<name> to the agent. */
const pluginOf = (id: string): string => id.replaceAll('.', '-');

/** The apps from `on` (the chat's ids, in their order) that have commands. */
export const appCommandsOf = (installed: readonly IInstalledApp[], on: readonly string[]): IAppCommands[] =>
  on.flatMap((id) => {
    const app = installed.find((a) => a.id === id);
    const commands = app?.commands ?? [];
    return app && commands.length > 0
      ? [{ appId: app.id, appName: app.name, plugin: pluginOf(app.id), commands }]
      : [];
  });
