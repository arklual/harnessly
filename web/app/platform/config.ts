// Where the page lives and what the runtime it talks to calls things. The runtime writes these into
// <meta name="hl-config"> when it serves index.html; development falls back to the defaults.
export interface IAppConfig {
  /** Path the page is served under, e.g. `/agents`; empty for the site root. */
  basePath: string;
  /** Prefix of the platform API (RUNTIME_PLATFORM_API on the runtime). */
  platformApi: string;
  /** ly-solutions/artifacts, when configured (RUNTIME_ARTIFACTS_URL). */
  artifactsUrl: string | null;
  /** ly-solutions/boards, when configured (RUNTIME_BOARDS_URL). */
  boardsUrl: string | null;
  /** Where document links open inside this app, e.g. `${origin}${basePath}/docs/{slug}`. */
  readerUrl: string | null;
  /** Wildcard domain of feature stands, e.g. `.preview.example.com`: their pages open in the pane by themselves. */
  standDomain: string | null;
  /** Hosts a stand may send a person to for signing in (the identity provider, the stands' auth proxy). */
  standSignInHosts: string[];
  /** Hosts whose pages may open in the pane's browser tab, besides the configured services. */
  paneHosts: string[];
  /** The pages a person finds pinned in the browser tab until they change their pins (RUNTIME_DEFAULT_PINS). */
  defaultPins: { text: string; name?: string }[];
  /** Issue link of the tracker with `{key}` in it, e.g. `https://tracker.example.com/issue/{key}` (RUNTIME_TRACKER_URL). */
  trackerIssueUrl: string | null;
  /** Hosts whose `/issue/<KEY>` links are tasks of the tracker. */
  trackerHosts: string[];
  /** Lower-case host[:port] of every connection that has issues or pages (Settings → Connections): their links are
   *  read through link-meta. */
  connectionHosts: string[];
  /** Task keys the page turns into chips, as a regular expression source (RUNTIME_TASK_KEY_RE). */
  taskKeyPattern: string;
  /** GitLab hosts whose merge requests and commits open in the pane (RUNTIME_GITLAB_HOSTS). */
  gitlabHosts: string[];
  /** Display names of the people of this installation by login (the part of the e-mail before @), in display order. */
  people: Record<string, string>;
  /** Optional parts of the page (RUNTIME_WEB_FEATURES): plain chats through an OpenWebUI, the team efficiency report,
   *  the password safe, Claude Design boards. */
  features: { chats: boolean; efficiency: boolean; vault: boolean; design: boolean };
  /** The language of the page (RUNTIME_LOCALE): `en` or `ru`. */
  locale: 'en' | 'ru';
  /** Product copy the installation overrides (RUNTIME_WEB_COPY_FILE), by key; see lib/copy.ts. */
  copy: Record<string, unknown>;
  /** The GitHub whose issues and pull requests the page shows (RUNTIME_GITHUB_HOST). */
  githubHost: string;
  /** Header prefix of the runtime's upload routes (RUNTIME_UPLOAD_HEADER_PREFIXES). */
  uploadHeaderPrefix: string;
  /** Product name in titles. */
  productName: string;
  /** The design tool's name in the page's texts, `{design}` (RUNTIME_DESIGN_NAME). */
  designName: string;
  /** The product's mark as an image URL (RUNTIME_APP_MARK_URL); none: the klodik mark. */
  markUrl: string | null;
  /** Key of the page layout in the runtime's config (RUNTIME_LAYOUT_KEY). */
  layoutKey: string;
  /** PostHog project for the page's speed events (none: nothing is sent). */
  speedPosthogHost: string | null;
  speedPosthogKey: string | null;
  /** Key of the remembered launch choice in the runtime's config (RUNTIME_LAUNCH_DEFAULTS_KEY). */
  launchDefaultsKey: string;
  /** The hub this machine is connected to (RUNTIME_HUB_URL), no trailing slash: the page links back to it and signing out
   *  goes through it. null: the runtime is reached directly. */
  hubUrl: string | null;
  /** Where the «Хаб» link leads (RUNTIME_HUB_LINK, else hubUrl), no trailing slash. It says nothing of who signs in. */
  hubLink: string | null;
  /** the hub keeps the chats' uploads (its store): what a person attaches goes there too, and the chat's
   *  pictures and files are read from there, the machine on or off. Set by the hub's workspace; false elsewhere. */
  centralUploads: boolean;
}

const DEFAULTS: IAppConfig = {
  basePath: '',
  platformApi: '/api/platform',
  artifactsUrl: null,
  boardsUrl: null,
  readerUrl: null,
  standDomain: null,
  standSignInHosts: [],
  paneHosts: [],
  defaultPins: [],
  people: {},
  features: { chats: false, efficiency: false, vault: false, design: false },
  locale: 'en',
  copy: {},
  uploadHeaderPrefix: 'x-harnessly-',
  githubHost: 'github.com',
  trackerIssueUrl: null,
  trackerHosts: [],
  connectionHosts: [],
  taskKeyPattern: '[A-Z][A-Z0-9]{1,9}-\\d{1,6}',
  gitlabHosts: ['gitlab.com'],
  productName: 'klodik.tech',
  designName: 'Designly',
  markUrl: null,
  speedPosthogHost: null,
  speedPosthogKey: null,
  layoutKey: 'layout',
  launchDefaultsKey: 'launchDefaults',
  hubUrl: null,
  hubLink: null,
  centralUploads: false,
};

let cached: IAppConfig | null = null;

export const appConfig = (): IAppConfig => {
  if (cached) return cached;
  let read: Partial<IAppConfig> = {};
  if (typeof document !== 'undefined') {
    const meta = document.querySelector('meta[name="hl-config"]')?.getAttribute('content');
    if (meta) {
      try {
        read = JSON.parse(meta) as Partial<IAppConfig>;
      } catch {
        read = {};
      }
    }
  }
  // The runtime fills <base href>, api-base and platform-api when it serves the page (cmd/runtime/routes_web.go).
  const baseHref =
    typeof document !== 'undefined' ? (document.querySelector('base')?.getAttribute('href') ?? '') : '';
  const platformMeta =
    typeof document !== 'undefined'
      ? document.querySelector('meta[name="platform-api"]')?.getAttribute('content')
      : null;
  // A null or a wrong type from the runtime falls back to the default, so the page can read every field as typed.
  for (const key of Object.keys(read) as (keyof IAppConfig)[]) {
    const fallback = DEFAULTS[key];
    const value = read[key];
    if (value === null && fallback !== null) delete read[key];
    else if (Array.isArray(fallback) && !Array.isArray(value)) delete read[key];
  }
  cached = {
    ...DEFAULTS,
    ...read,
    features: { ...DEFAULTS.features, ...(read.features ?? {}) },
    // The person's own choice (Settings → General) wins over the installation's language.
    locale:
      (typeof document !== 'undefined' ? savedLanguage() : null) ?? (read.locale === 'ru' ? 'ru' : 'en'),
    copy: read.copy ?? {},
    basePath: (read.basePath ?? baseHref ?? DEFAULTS.basePath).replace(/\/+$/, ''),
    platformApi: (read.platformApi ?? platformMeta ?? DEFAULTS.platformApi).replace(/\/+$/, ''),
    hubUrl: typeof read.hubUrl === 'string' && read.hubUrl ? read.hubUrl.replace(/\/+$/, '') : null,
    hubLink: typeof read.hubLink === 'string' && read.hubLink ? read.hubLink.replace(/\/+$/, '') : null,
    centralUploads: read.centralUploads === true,
  };
  return cached;
};

export const appBasePath = (): string => appConfig().basePath;
export const platformApi = (): string => appConfig().platformApi;

/** The page layout saved in the runtime's config under its configured key. */
export const savedLayoutOf = <T>(config: Record<string, unknown> | null | undefined): T | undefined =>
  (config?.[appConfig().layoutKey] as T | undefined) ?? undefined;

// The language the person chose for the interface in Settings → General: this browser's, as the theme is. Until one is
// chosen the installation's language (RUNTIME_LOCALE) holds.
export type TLanguage = 'ru' | 'en';

const KEY = 'hl:language';

/** The language chosen in this browser, or null while none is (or the storage cannot be read). */
export const savedLanguage = (): TLanguage | null => {
  try {
    const value = window.localStorage.getItem(KEY);
    return value === 'ru' || value === 'en' ? value : null;
  } catch {
    return null;
  }
};

/** Keeps the choice; false when the browser would not store it, so the choice must not be shown as made. */
export const saveLanguage = (lang: TLanguage): boolean => {
  try {
    window.localStorage.setItem(KEY, lang);
    return window.localStorage.getItem(KEY) === lang;
  } catch {
    return false;
  }
};
