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
    id: '2026-11-08a',
    items: [
      { key: 'libToolbar', icon: 'sliders-horizontal' },
      { key: 'libCardMenu', icon: 'ellipsis' },
      { key: 'slotPages', icon: 'presentation' },
    ],
  },
  {
    id: '2026-11-07zz',
    items: [{ key: 'appMentions', icon: 'layout-grid' }],
  },
  {
    id: '2026-11-07za',
    items: [
      { key: 'machinePick', icon: 'server' },
      { key: 'machineStatus', icon: 'activity' },
    ],
  },
  {
    id: '2026-11-07z',
    items: [
      { key: 'designFolders', icon: 'folders' },
      { key: 'designExamples', icon: 'folder-open' },
    ],
  },
  {
    id: '2026-11-07w',
    items: [
      { key: 'appSecrets', icon: 'key-round' },
      { key: 'appSecretsShared', icon: 'lock' },
    ],
  },
  {
    id: '2026-11-07v',
    items: [
      { key: 'appAI', icon: 'sparkles' },
      { key: 'appAgentsRun', icon: 'messages-square' },
      { key: 'appBudget', icon: 'gauge' },
    ],
  },
  {
    id: '2026-11-07u',
    items: [
      { key: 'appTerminal', icon: 'square-terminal' },
      { key: 'appBrowser', icon: 'globe' },
      { key: 'appHooks', icon: 'shield-check' },
    ],
  },
  {
    id: '2026-11-07t',
    items: [
      { key: 'sinopiaSees', icon: 'eye' },
      { key: 'sinopiaHands', icon: 'presentation' },
      { key: 'appKeys', icon: 'keyboard' },
    ],
  },
  {
    id: '2026-11-07s',
    items: [
      { key: 'appRun', icon: 'terminal' },
      { key: 'appDevServer', icon: 'monitor' },
      { key: 'appAgentAsks', icon: 'shield-check' },
    ],
  },
  {
    id: '2026-11-07r',
    items: [
      { key: 'appButtons', icon: 'layout-grid' },
      { key: 'appSearch', icon: 'search' },
      { key: 'appLinks', icon: 'link' },
    ],
  },
  {
    id: '2026-11-07q',
    items: [{ key: 'appFast', icon: 'layout-grid' }],
  },
  {
    id: '2026-11-07n',
    items: [
      { key: 'libFolders', icon: 'folder' },
      { key: 'libFilters', icon: 'list-filter' },
      { key: 'libTrash', icon: 'trash-2' },
    ],
  },
  {
    id: '2026-11-07m',
    items: [
      { key: 'appFiles', icon: 'folder' },
      { key: 'appGit', icon: 'git-branch' },
      { key: 'appEvents', icon: 'bell' },
    ],
  },
  {
    id: '2026-11-07l',
    items: [
      { key: 'appConsent', icon: 'shield-check' },
      { key: 'appSwitch', icon: 'shield-off' },
      { key: 'appCompat', icon: 'refresh-cw' },
    ],
  },
  {
    id: '2026-11-07k',
    items: [{ key: 'sinopiaChatLeft', icon: 'panel-left' }],
  },
  {
    id: '2026-11-07j',
    items: [
      { key: 'annotate', icon: 'message-square-plus' },
      { key: 'annotateSend', icon: 'send' },
      { key: 'annotateChips', icon: 'map-pin' },
    ],
  },
  {
    id: '2026-11-07i',
    items: [
      { key: 'tidyMenu', icon: 'panel-left' },
      { key: 'fullTitle', icon: 'message-square-text' },
    ],
  },
  {
    id: '2026-11-07h',
    items: [{ key: 'longMessages', icon: 'file-text' }],
  },
  {
    id: '2026-11-07g',
    items: [
      { key: 'kitFile', icon: 'download' },
      { key: 'kitFonts', icon: 'palette' },
      { key: 'kitLayouts', icon: 'layout-grid' },
    ],
  },
  {
    id: '2026-11-07f',
    items: [{ key: 'appHeader', icon: 'layout-grid' }],
  },
  {
    id: '2026-11-07e',
    items: [{ key: 'appsDebug', icon: 'bug' }],
  },
  {
    id: '2026-11-07d',
    items: [{ key: 'appViewsLoad', icon: 'refresh-cw' }],
  },
  {
    id: '2026-11-07c',
    items: [{ key: 'liveFallback', icon: 'refresh-cw' }],
  },
  {
    id: '2026-11-07b',
    items: [
      { key: 'uploadFiles', icon: 'upload' },
      { key: 'anySize', icon: 'paperclip' },
      { key: 'newFolder', icon: 'folder-plus' },
    ],
  },
  {
    id: '2026-11-07a',
    items: [
      { key: 'lessonRing', icon: 'square-mouse-pointer' },
      { key: 'lessonPick', icon: 'message-square-text' },
    ],
  },
  {
    id: '2026-11-06zl',
    items: [{ key: 'appSafeMode', icon: 'shield-question' }],
  },
  {
    id: '2026-11-06zk',
    items: [{ key: 'appPrecheck', icon: 'shield-question' }],
  },
  {
    id: '2026-11-06zj',
    items: [{ key: 'appState', icon: 'shield-question' }],
  },
  {
    id: '2026-11-06zi',
    items: [
      { key: 'pageTools', icon: 'blocks' },
      { key: 'remoteViews', icon: 'link' },
    ],
  },
  {
    id: '2026-11-06zh',
    items: [
      { key: 'tabsTidy', icon: 'layers' },
      { key: 'tabsFront', icon: 'shield-question' },
      { key: 'appCatalog', icon: 'blocks' },
      { key: 'appsInPlus', icon: 'layout-grid' },
    ],
  },
  {
    id: '2026-11-06zg',
    items: [
      { key: 'learn', icon: 'graduation-cap' },
      { key: 'demo', icon: 'palette' },
      { key: 'fileHead', icon: 'file-text' },
    ],
  },
  {
    id: '2026-11-06zf',
    items: [
      { key: 'codexApps', icon: 'blocks' },
      { key: 'appConnection', icon: 'link' },
      { key: 'appJournal', icon: 'shield-question' },
    ],
  },
  {
    id: '2026-11-06ze',
    items: [
      { key: 'envApps', icon: 'layout-grid' },
      { key: 'envForms', icon: 'blocks' },
    ],
  },
  {
    id: '2026-11-06zd',
    items: [{ key: 'envPresets', icon: 'layers' }],
  },
  {
    id: '2026-11-06zc',
    items: [{ key: 'groupFolders', icon: 'folder' }],
  },
  {
    id: '2026-11-06zb',
    items: [{ key: 'basePackage', icon: 'layers' }],
  },
  {
    id: '2026-11-06z',
    items: [
      { key: 'viewQuiet', icon: 'eye' },
      { key: 'keyStep', icon: 'shield-check' },
    ],
  },
  {
    id: '2026-11-06y',
    items: [{ key: 'recommended', icon: 'sparkles' }],
  },
  {
    id: '2026-11-06x',
    items: [
      { key: 'envGroups', icon: 'folder' },
      { key: 'envApply', icon: 'rotate-cw' },
    ],
  },
  {
    id: '2026-11-06w',
    items: [
      { key: 'chatWidgets', icon: 'layout-grid' },
      { key: 'appsAlways', icon: 'blocks' },
      { key: 'appsSafer', icon: 'shield-check' },
    ],
  },
  {
    id: '2026-11-06b',
    items: [{ key: 'previewByPath', icon: 'shield-check' }],
  },
  {
    id: '2026-11-06',
    items: [
      { key: 'docsLibrary', icon: 'book-open' },
      { key: 'docsTeam', icon: 'folder-open' },
      { key: 'docsAgents', icon: 'square-pen' },
    ],
  },
  {
    id: '2026-11-05r',
    items: [
      { key: 'setupCard', icon: 'laptop' },
      { key: 'reposSkill', icon: 'folder-git' },
    ],
  },
  {
    id: '2026-11-05q',
    items: [
      { key: 'envScripts', icon: 'file-terminal' },
      { key: 'envSandbox', icon: 'shield-check' },
      { key: 'envMachine', icon: 'server' },
    ],
  },
  {
    id: '2026-11-05p',
    items: [
      { key: 'modeSwitch', icon: 'pencil' },
      { key: 'shareSmooth', icon: 'gauge' },
      { key: 'designLinks', icon: 'external-link' },
      { key: 'agentFiles', icon: 'file-text' },
    ],
  },
  {
    id: '2026-11-05n',
    items: [{ key: 'designChats', icon: 'messages-square' }],
  },
  {
    id: '2026-11-05m',
    items: [{ key: 'askAccess', icon: 'lock-keyhole' }],
  },
  {
    id: '2026-11-05h',
    items: [
      { key: 'shareEverywhere', icon: 'share-2' },
      { key: 'docLinks', icon: 'file-lock' },
      { key: 'deckLinks', icon: 'presentation' },
    ],
  },
  {
    id: '2026-11-05g',
    items: [{ key: 'castFast', icon: 'gauge' }],
  },
  {
    id: '2026-11-05f',
    items: [{ key: 'liveTools', icon: 'square-mouse-pointer' }],
  },
  {
    id: '2026-11-05e',
    items: [
      { key: 'guestLinks', icon: 'link' },
      { key: 'guestLinkSettings', icon: 'sliders-horizontal' },
      { key: 'siteAddress', icon: 'globe' },
    ],
  },
  {
    id: '2026-11-05d',
    items: [{ key: 'agentCards', icon: 'message-square-text' }],
  },
  {
    id: '2026-11-05b',
    items: [{ key: 'noAsk', icon: 'square-dashed-mouse-pointer' }],
  },
  {
    id: '2026-11-05a',
    items: [{ key: 'liveBrowser', icon: 'globe' }],
  },
  {
    id: '2026-11-04z',
    items: [{ key: 'browserAsk', icon: 'shield-question' }],
  },
  {
    id: '2026-11-04y',
    items: [
      { key: 'findInFiles', icon: 'text-search' },
      { key: 'gitMarks', icon: 'git-compare' },
      { key: 'fileOps', icon: 'file-plus' },
    ],
  },
  {
    id: '2026-11-04x',
    items: [{ key: 'claudeConfigDir', icon: 'message-square-text' }],
  },
  {
    id: '2026-11-04w',
    items: [
      { key: 'machineFiles', icon: 'hard-drive' },
      { key: 'quickOpen', icon: 'search' },
      { key: 'phoneFiles', icon: 'list-checks' },
    ],
  },
  {
    id: '2026-11-04u',
    items: [{ key: 'teleportApps', icon: 'log-in' }],
  },
  {
    id: '2026-11-04t',
    items: [{ key: 'fullQuiet', icon: 'maximize-2' }],
  },
  {
    id: '2026-11-04s',
    items: [{ key: 'modeLeft', icon: 'pencil' }],
  },
  {
    id: '2026-11-04r',
    items: [{ key: 'sinopiaDownload', icon: 'download' }],
  },
  {
    id: '2026-11-04q',
    items: [{ key: 'vaultLogin', icon: 'key-round' }],
  },
  {
    id: '2026-11-04p',
    items: [{ key: 'siteHops', icon: 'log-in' }],
  },
  {
    id: '2026-11-04o',
    items: [{ key: 'siteTools', icon: 'blocks' }],
  },
  {
    id: '2026-11-04n',
    items: [
      { key: 'shareOne', icon: 'share' },
      { key: 'shareTeams', icon: 'users' },
      { key: 'publishInShare', icon: 'globe' },
    ],
  },
  {
    id: '2026-11-04m',
    items: [
      { key: 'diff', icon: 'git-compare' },
      { key: 'askCode', icon: 'message-square-text' },
    ],
  },
  {
    id: '2026-11-04l',
    items: [
      { key: 'phonePill', icon: 'smartphone' },
      { key: 'netPill', icon: 'wifi-off' },
      { key: 'chatMode', icon: 'pencil' },
      { key: 'devtools', icon: 'panel-bottom' },
      { key: 'inspect', icon: 'square-mouse-pointer' },
    ],
  },
  {
    id: '2026-11-04k',
    items: [
      { key: 'offlineSend', icon: 'send' },
      { key: 'offlineVoice', icon: 'mic' },
    ],
  },
  {
    id: '2026-11-04j',
    items: [
      { key: 'agentBrowser', icon: 'sparkles' },
      { key: 'agentAsk', icon: 'shield-check' },
    ],
  },
  {
    id: '2026-11-04i',
    items: [
      { key: 'fileMenu', icon: 'ellipsis' },
      { key: 'viewers', icon: 'eye' },
    ],
  },
  {
    id: '2026-11-04h',
    items: [
      { key: 'download', icon: 'download' },
      { key: 'htmlPage', icon: 'code-xml' },
    ],
  },
  {
    id: '2026-11-04g',
    items: [
      { key: 'pick', icon: 'square-mouse-pointer' },
      { key: 'devices', icon: 'smartphone' },
    ],
  },
  {
    id: '2026-11-04f',
    items: [
      { key: 'links', icon: 'external-link' },
      { key: 'linkMenu', icon: 'menu' },
    ],
  },
  {
    id: '2026-11-04e',
    items: [
      { key: 'viewMode', icon: 'eye' },
      { key: 'swipeToast', icon: 'bell' },
      { key: 'livePages', icon: 'file-text' },
    ],
  },
  {
    id: '2026-11-04d',
    items: [
      { key: 'fitRotate', icon: 'rotate-cw' },
      { key: 'voiceRetry', icon: 'mic' },
      { key: 'boardError', icon: 'message-square' },
    ],
  },
  {
    id: '2026-11-04c',
    items: [
      { key: 'sidebar', icon: 'panel-left' },
      { key: 'askCard', icon: 'message-square' },
      { key: 'startAttach', icon: 'paperclip' },
    ],
  },
  {
    id: '2026-11-04b',
    items: [
      { key: 'designChat', icon: 'panel-left' },
      { key: 'designCanvas', icon: 'maximize-2' },
      { key: 'allScreens', icon: 'layout-grid' },
    ],
  },
  {
    id: '2026-11-04a',
    items: [
      { key: 'deckTerminal', icon: 'square-terminal' },
      { key: 'startFiles', icon: 'image' },
    ],
  },
  {
    id: '2026-11-04',
    items: [{ key: 'officialLogos', icon: 'blocks' }],
  },
  {
    id: '2026-11-03h',
    items: [{ key: 'brandIcons', icon: 'blocks' }],
  },
  {
    id: '2026-11-03g',
    items: [
      { key: 'pictureEdit', icon: 'pencil' },
      { key: 'sinopiaHover', icon: 'presentation' },
    ],
  },
  {
    id: '2026-11-03f',
    items: [
      { key: 'characters', icon: 'sparkles' },
      { key: 'record', icon: 'mouse-pointer' },
      { key: 'overlays', icon: 'text-quote' },
    ],
  },
  {
    id: '2026-11-03e',
    items: [{ key: 'mcpFlow', icon: 'blocks' }],
  },
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

/** What the card has room for; a person who was away for long sees the latest of it. */
export const MAX_NEWS_ITEMS = 5;

export interface INewsItem extends IReleaseItem {
  /** The release it came with: its texts are under this id. */
  release: string;
}

export interface INews {
  /** The newest release: remembered as seen when the card is put away. */
  id: string;
  items: readonly INewsItem[];
}

/**
 * What to tell this person: all the releases newer than the one they saw, newest first, as several go out in a day. Who
 * has seen none (a new browser) hears of the newest alone, not of the whole history.
 */
export const unseenNews = (releases: readonly IRelease[], seen: string | null): INews | null => {
  const latest = releases[0];
  if (latest === undefined) return null;
  if (seen !== null && seen >= latest.id) return null;
  const told = seen === null ? [latest] : releases.filter((release) => release.id > seen);
  const items = told
    .flatMap((release) => release.items.map((item) => ({ ...item, release: release.id })))
    .slice(0, MAX_NEWS_ITEMS);
  return { id: latest.id, items };
};
