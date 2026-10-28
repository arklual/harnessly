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
  /** Issue link of the tracker with `{key}` in it, e.g. `https://tracker.example.com/issue/{key}` (RUNTIME_TRACKER_URL). */
  trackerIssueUrl: string | null;
  /** Hosts whose `/issue/<KEY>` links are tasks of the tracker. */
  trackerHosts: string[];
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
  /** The product's mark as an image URL (RUNTIME_APP_MARK_URL); none: the harnessly mark. */
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
  people: {},
  features: { chats: false, efficiency: false, vault: false, design: false },
  locale: 'en',
  copy: {},
  uploadHeaderPrefix: 'x-harnessly-',
  githubHost: 'github.com',
  trackerIssueUrl: null,
  trackerHosts: [],
  taskKeyPattern: '[A-Z][A-Z0-9]{1,9}-\\d{1,6}',
  gitlabHosts: ['gitlab.com'],
  productName: 'harnessly',
  designName: 'Designly',
  markUrl: null,
  speedPosthogHost: null,
  speedPosthogKey: null,
  layoutKey: 'layout',
  launchDefaultsKey: 'launchDefaults',
  hubUrl: null,
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
    locale: read.locale === 'ru' ? 'ru' : 'en',
    copy: read.copy ?? {},
    basePath: (read.basePath ?? baseHref ?? DEFAULTS.basePath).replace(/\/+$/, ''),
    platformApi: (read.platformApi ?? platformMeta ?? DEFAULTS.platformApi).replace(/\/+$/, ''),
    hubUrl: typeof read.hubUrl === 'string' && read.hubUrl ? read.hubUrl.replace(/\/+$/, '') : null,
  };
  return cached;
};

export const appBasePath = (): string => appConfig().basePath;
export const platformApi = (): string => appConfig().platformApi;

/** The page layout saved in the runtime's config under its configured key. */
export const savedLayoutOf = <T>(config: Record<string, unknown> | null | undefined): T | undefined =>
  (config?.[appConfig().layoutKey] as T | undefined) ?? undefined;
