// «Что нового»: what a release brings, in the person's words — no versions, no file names. The newest release goes
// first; its id is the day it went out (a second one that day takes a letter: 2026-10-27b), so ids sort as strings.
// The texts are next.whatsNew.<id>.<key>.title / .text in app/locales/<lang>/next-whats-new.json.
export interface IReleaseItem {
  key: string;
  icon: string;
}

export interface IRelease {
  id: string;
  items: readonly IReleaseItem[];
}

export const RELEASES = [
  {
    id: '2026-11-03d',
    items: [
      { key: 'connections', icon: 'plug' },
      { key: 'agentsRead', icon: 'book-open' },
      { key: 'mcp', icon: 'blocks' },
    ],
  },
  {
    id: '2026-11-03c',
    items: [{ key: 'docs', icon: 'book-open' }],
  },
  {
    id: '2026-11-03b',
    items: [
      { key: 'queueNote', icon: 'message-square' },
      { key: 'gates', icon: 'shield-question' },
    ],
  },
  {
    id: '2026-11-03a',
    items: [{ key: 'files', icon: 'paperclip' }],
  },
  {
    id: '2026-11-02q',
    items: [
      { key: 'paneOverview', icon: 'palette' },
      { key: 'playerMoment', icon: 'play' },
    ],
  },
  {
    id: '2026-11-02p',
    items: [{ key: 'oneStart', icon: 'palette' }],
  },
  {
    id: '2026-11-02o',
    items: [
      { key: 'videoSeek', icon: 'play' },
      { key: 'designNoSetup', icon: 'pen-tool' },
    ],
  },
  {
    id: '2026-11-02n',
    items: [{ key: 'limitReset', icon: 'clock' }],
  },
  {
    id: '2026-11-02m',
    items: [
      { key: 'language', icon: 'languages' },
      { key: 'longCommand', icon: 'shield-question' },
    ],
  },
  {
    id: '2026-11-02l',
    items: [
      { key: 'office', icon: 'file-text' },
      { key: 'history', icon: 'history' },
    ],
  },
  {
    id: '2026-11-02k',
    items: [
      { key: 'selection', icon: 'locate' },
      { key: 'sheet', icon: 'file-spreadsheet' },
    ],
  },
  {
    id: '2026-11-02j',
    items: [
      { key: 'hubMoney', icon: 'chart-line' },
      { key: 'hubMachines', icon: 'server' },
    ],
  },
  {
    id: '2026-11-02i',
    items: [{ key: 'deckApps', icon: 'presentation' }],
  },
  {
    id: '2026-11-02h',
    items: [
      { key: 'player', icon: 'play' },
      { key: 'pdf', icon: 'file-text' },
    ],
  },
  {
    id: '2026-11-02g',
    items: [
      { key: 'appDocs', icon: 'library' },
      { key: 'appsWall', icon: 'shield-check' },
    ],
  },
  {
    id: '2026-11-02f',
    items: [{ key: 'language', icon: 'languages' }],
  },
  {
    id: '2026-11-02e',
    items: [
      { key: 'longCommand', icon: 'shield-question' },
      { key: 'enterAnswers', icon: 'keyboard' },
    ],
  },
  {
    id: '2026-11-02d',
    items: [
      { key: 'longVoice', icon: 'mic' },
      { key: 'sinopiaSize', icon: 'presentation' },
      { key: 'pictureList', icon: 'image' },
    ],
  },
  {
    id: '2026-11-02c',
    items: [
      { key: 'motion', icon: 'play' },
      { key: 'show', icon: 'eye' },
    ],
  },
  {
    id: '2026-11-02b',
    items: [
      { key: 'pictureEdit', icon: 'pencil' },
      { key: 'sinopiaHover', icon: 'presentation' },
    ],
  },
  {
    id: '2026-11-02',
    items: [{ key: 'manage', icon: 'sliders-horizontal' }],
  },
  {
    id: '2026-10-31k',
    items: [{ key: 'appsMain', icon: 'layout-grid' }],
  },
  {
    id: '2026-10-31j',
    items: [{ key: 'keys', icon: 'keyboard' }],
  },
  {
    id: '2026-10-31i',
    items: [
      { key: 'words', icon: 'book-open' },
      { key: 'models', icon: 'mic' },
    ],
  },
  {
    id: '2026-10-31h',
    items: [
      { key: 'images', icon: 'image' },
      { key: 'sinopiaImages', icon: 'presentation' },
    ],
  },
  {
    id: '2026-10-31g',
    items: [
      { key: 'quotaRetry', icon: 'rotate-ccw' },
      { key: 'keys', icon: 'keyboard' },
      { key: 'step', icon: 'arrow-right-left' },
    ],
  },
  {
    id: '2026-10-31f',
    items: [
      { key: 'gpt', icon: 'sparkles' },
      { key: 'models', icon: 'key-round' },
    ],
  },
  {
    id: '2026-10-31e',
    items: [{ key: 'questions', icon: 'message-square' }],
  },
  {
    id: '2026-10-31d',
    items: [
      { key: 'looks', icon: 'palette' },
      { key: 'notices', icon: 'bell' },
      { key: 'settings', icon: 'sliders-horizontal' },
    ],
  },
  {
    id: '2026-10-31c',
    items: [
      { key: 'createField', icon: 'sliders-horizontal' },
      { key: 'shortMessage', icon: 'message-square' },
    ],
  },
  {
    id: '2026-10-31b',
    items: [{ key: 'apps', icon: 'blocks' }],
  },
  {
    id: '2026-10-31',
    items: [
      { key: 'share', icon: 'share' },
      { key: 'boardError', icon: 'triangle-alert' },
      { key: 'designsFold', icon: 'chevron-right' },
    ],
  },
  {
    id: '2026-10-30c',
    items: [
      { key: 'activeDesigns', icon: 'palette' },
      { key: 'video', icon: 'play' },
      { key: 'rail', icon: 'panel-left' },
      { key: 'layout', icon: 'palette' },
    ],
  },
  {
    id: '2026-10-30b',
    items: [
      { key: 'designs', icon: 'palette' },
      { key: 'folders', icon: 'folder' },
      { key: 'designMachine', icon: 'server' },
    ],
  },
  {
    id: '2026-10-30',
    items: [{ key: 'browser', icon: 'globe' }],
  },
  {
    id: '2026-10-28i',
    items: [
      { key: 'same', icon: 'messages-square' },
      { key: 'machine', icon: 'server' },
      { key: 'publish', icon: 'globe' },
    ],
  },
  {
    id: '2026-10-28h',
    items: [{ key: 'terminal', icon: 'square-terminal' }],
  },
  {
    id: '2026-10-28g',
    items: [{ key: 'account', icon: 'user' }],
  },
  {
    id: '2026-10-28e',
    items: [
      { key: 'takes', icon: 'mic' },
      { key: 'keep', icon: 'history' },
    ],
  },
  {
    id: '2026-10-28d',
    items: [
      { key: 'edit', icon: 'pencil' },
      { key: 'kept', icon: 'mic' },
    ],
  },
  {
    id: '2026-10-28c',
    items: [{ key: 'login', icon: 'log-in' }],
  },
  {
    id: '2026-10-28b',
    items: [
      { key: 'folders', icon: 'folder' },
      { key: 'machine', icon: 'server' },
    ],
  },
  {
    id: '2026-10-28',
    items: [
      { key: 'one', icon: 'messages-square' },
      { key: 'here', icon: 'arrow-right-left' },
      { key: 'off', icon: 'history' },
    ],
  },
  {
    id: '2026-10-27b',
    items: [
      { key: 'safe', icon: 'lock' },
      { key: 'safeSettings', icon: 'key-round' },
    ],
  },
  {
    id: '2026-10-27',
    items: [
      { key: 'telegram', icon: 'send' },
      { key: 'full', icon: 'maximize-2' },
      { key: 'version', icon: 'link' },
    ],
  },
] as const satisfies readonly IRelease[];

/** The release to tell about: the newest one, unless this person has already seen it (or a later one). */
export const unseenRelease = (releases: readonly IRelease[], seen: string | null): IRelease | null => {
  const latest = releases[0];
  if (!latest) return null;
  if (seen !== null && seen >= latest.id) return null;
  return latest;
};
