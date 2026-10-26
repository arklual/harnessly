// How an agent (Claude Code, Codex) is signed in from the page: the in-page login of a subscription and the key of the
// API. The login runs on the machine; the page only shows its link and code and takes back the code the browser gives.

/** What the page shows of a login that runs on the machine. */
export type LoginState =
  | { k: 'idle' }
  | { k: 'starting' }
  /** The code went to the CLI and the machine checks it. */
  | { k: 'checking' }
  /** The person opens `url` in a browser; `userCode` is the code to type there (a device login), `asksCode` says the
   *  browser gives a code back to paste in here (a paste-back login). */
  | { k: 'waiting'; url: string; userCode: string | null; asksCode: boolean }
  | { k: 'failed'; reason: string };

/** How a card signs in: a login that runs on the machine (a subscription; for Claude also the console's API billing) or,
 *  for Codex, an API key typed here. */
export type LoginMethod = 'subscription' | 'console' | 'key';
/** The login the machine runs for a method. */
export type LoginVia = 'subscription' | 'console';
export const METHODS_OF = {
  claude: ['subscription', 'console'],
  codex: ['subscription', 'key'],
} as const satisfies Record<'claude' | 'codex', readonly LoginMethod[]>;

/** What the card can do with the agent's credentials; each resolves null when taken, else the reason. */
export interface ICredentialActions {
  onLogin: (via: LoginVia) => Promise<string | null>;
  /** Hands the code the browser gave back to the login that waits for it. */
  onSubmitCode: (code: string) => Promise<string | null>;
  onCancelLogin: () => Promise<void>;
  onLogout: () => Promise<string | null>;
  /** Codex only: Claude takes no key, only the console's login. */
  onSaveKey: (key: string) => Promise<string | null>;
}

/** The key the machine has of the agent: whether one is saved (the machine never gives back its digits). */
export interface ISavedKey {
  configured: boolean;
  tail: null;
}
