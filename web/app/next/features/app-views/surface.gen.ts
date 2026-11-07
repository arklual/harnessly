// Generated from internal/apps/surface.go and contributions.go (UPDATE_SURFACE=1 go test ./internal/apps -run TestSurfaceFilesAreCurrent); do not edit.

/** The right each method of the bridge needs; '' is a method that needs none. */
export const METHOD_PERMISSION = {
  'ui/initialize': '',
  'ui/notifications/initialized': '',
  'harnessly/debug.log': '',
  'ui/notifications/size-changed': '',
  'ui/open-link': '',
  'tools/call': '',
  'ui/message': 'chat.prompt',
  'harnessly/chat.read': 'chat.read',
  'harnessly/chat.card': 'chat.card',
  'harnessly/pane.open': 'pane.open',
  'harnessly/agents.list': 'agents.read',
  'harnessly/storage.get': 'storage',
  'harnessly/storage.set': 'storage',
  'harnessly/storage.delete': 'storage',
  'harnessly/storage.keys': 'storage',
  'harnessly/settings.get': '',
  'harnessly/notify': 'notify',
  'harnessly/files.read': 'files.read',
  'harnessly/files.write': 'files.write',
  'harnessly/tools.register': 'tools.page',
  'harnessly/tools.result': 'tools.page',
  'harnessly/context.publish': 'context.publish',
  'harnessly/ui.toast': '',
  'harnessly/ui.confirm': '',
  'harnessly/slot.done': '',
  'harnessly/sinopia.document': 'sinopia.read',
  'harnessly/sinopia.selection': 'sinopia.read',
  'harnessly/sinopia.node': 'sinopia.read',
  'harnessly/sinopia.query': 'sinopia.read',
  'harnessly/sinopia.fields': 'sinopia.read',
  'harnessly/sinopia.mark': 'sinopia.read',
  'harnessly/sinopia.patch': 'sinopia.write',
  'harnessly/sinopia.setFields': 'sinopia.write',
  'harnessly/sinopia.boards.add': 'sinopia.write',
  'harnessly/sinopia.render': 'sinopia.export',
  'harnessly/sinopia.comment': 'sinopia.comment',
} as const satisfies Record<string, string>;

/** The rights an app asks for in `permissions`; files.read, files.write and secrets are written right:<value>. */
export const APP_PERMISSIONS = [
  'chat.prompt',
  'chat.read',
  'chat.card',
  'agents.read',
  'pane.open',
  'storage',
  'storage.space.write',
  'files.read',
  'files.write',
  'secrets',
  'notify',
  'tools.page',
  'context.publish',
  'sinopia.read',
  'sinopia.write',
  'sinopia.comment',
  'sinopia.kind',
  'sinopia.export',
  'ai',
  'users.read',
] as const;

/** The places an app works in. */
export const APP_TARGETS = [
  'chat',
  'pane',
  'app',
  'settings',
  'onboarding',
  'object-action',
  'hub-settings',
  'branding',
  'sinopia.<kind>',
  'sinopia.*',
] as const;

/** The Sinopia methods (the part after `harnessly/sinopia.`). */
export const SINOPIA_METHODS = [
  'document',
  'selection',
  'node',
  'query',
  'fields',
  'mark',
  'patch',
  'setFields',
  'boards.add',
  'render',
  'comment',
] as const;

/** The kinds of contribution an app puts into the host. */
export const CONTRIBUTION_KINDS = [
  'view',
  'chat.command',
  'chat.card',
  'agent',
  'branding',
  'sinopia.kind',
  'sinopia.panel',
  'sinopia.selection',
  'sinopia.check',
  'sinopia.export',
  'sinopia.import',
] as const;

export type TContributionKind = (typeof CONTRIBUTION_KINDS)[number];
