/**
 * Secret and path redaction for conversation snapshots.
 *
 * Runs over every string of a snapshot when it is built and again on every render, so the output of
 * one pass must pass through the next unchanged: placeholders never match a rule, and rules that
 * keep a key and hide its value skip values that are already placeholders.
 * Regexes do not catch everything: the owner previews the snapshot before it leaves the machine.
 */
import { t } from '@/lib/i18n';

export const REDACTION_RULE = {
  knownSecret: 'known-secret',
  privateKey: 'private-key',
  jwt: 'jwt',
  anthropic: 'anthropic',
  openai: 'openai',
  github: 'github',
  gitlab: 'gitlab',
  slack: 'slack',
  telegram: 'telegram',
  aws: 'aws',
  google: 'google',
  yandex: 'yandex',
  bearer: 'bearer',
  cookie: 'cookie',
  urlCredentials: 'url-credentials',
  connString: 'conn-string',
  envAssignment: 'env-assignment',
  kvSecret: 'kv-secret',
  highEntropy: 'high-entropy',
} as const;

export type TRedactionRule = (typeof REDACTION_RULE)[keyof typeof REDACTION_RULE];

/** Names for the report; the popover prints "Hidden: GitHub token × 2". */
export const REDACTION_RULE_LABELS: Record<TRedactionRule, string> = {
  'known-secret': t('lib.hlShareRedact.knownSecret'),
  'private-key': t('lib.hlShareRedact.privateKey'),
  jwt: 'JWT',
  anthropic: t('lib.hlShareRedact.anthropic'),
  openai: t('lib.hlShareRedact.openai'),
  github: t('lib.hlShareRedact.github'),
  gitlab: t('lib.hlShareRedact.gitlab'),
  slack: t('lib.hlShareRedact.slack'),
  telegram: t('lib.hlShareRedact.telegram'),
  aws: t('lib.hlShareRedact.aws'),
  google: t('lib.hlShareRedact.google'),
  yandex: t('lib.hlShareRedact.yandex'),
  bearer: t('lib.hlShareRedact.bearer'),
  cookie: 'cookie',
  'url-credentials': t('lib.hlShareRedact.urlCredentials'),
  'conn-string': t('lib.hlShareRedact.connString'),
  'env-assignment': t('lib.hlShareRedact.envAssignment'),
  'kv-secret': t('lib.hlShareRedact.kvSecret'),
  'high-entropy': t('lib.hlShareRedact.highEntropy'),
};

/**
 * Protocol value, not page copy: the runtime's redactor (internal/share/redact.go) writes the same placeholder into
 * stored snapshots, in the runtime's language (RUNTIME_LOCALE), so the two stay byte-identical per language.
 */
export const redactionPlaceholder = (rule: TRedactionRule) => t('lib.hlShareRedact.placeholder', { rule });

/**
 * A value that an earlier pass already hid: rules that keep a key and hide its value leave it alone. Both languages'
 * placeholders are recognised, so a snapshot stored with the old Russian one (`[скрыто: …]`) is not redacted twice.
 */
const NOT_PLACEHOLDER = '(?![\\s"\']*\\[(?:скрыто|hidden: ))';

export interface IRedactionContext {
  /** Exact secret values of this runtime (auth secret, CLI token, secret env values). */
  knownSecrets: readonly string[];
  /** Home directories to shorten to `~` (the runtime's $HOME). /home/<u>, /root and profile homes are always shortened. */
  homeDirs: readonly string[];
  /** HL_WORKSPACE_DIR: its prefix is dropped so paths read relative to the repository. */
  workspaceDir: string | null;
}

export interface IRedactionReport {
  pathsShortened: number;
  redactions: Partial<Record<TRedactionRule, number>>;
}

interface IPatternRule {
  id: TRedactionRule;
  re: RegExp;
  /** Replaces the whole match by default; `keep` returns the part of the match that stays visible before the placeholder. */
  keep?: (match: string, ...groups: string[]) => string;
}

const keepGroup = (_match: string, prefix: string) => prefix;

/** Order matters: specific shapes first, so a GitHub token inside `TOKEN=` is counted as GitHub. */
const PATTERN_RULES: readonly IPatternRule[] = [
  { id: 'private-key', re: /-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----/g },
  { id: 'jwt', re: /\beyJ[\w-]{10,}\.[\w-]{10,}\.[\w-]{10,}/g },
  { id: 'anthropic', re: /\bsk-ant-[\w-]{20,}/g },
  { id: 'openai', re: /\bsk-(?:proj-)?[\w-]{20,}/g },
  { id: 'github', re: /\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_\w{22,})/g },
  { id: 'gitlab', re: /\bglpat-[\w-]{20,}/g },
  { id: 'slack', re: /\bxox[abprs]-[\w-]{10,}/g },
  { id: 'telegram', re: /\b\d{8,10}:[\w-]{35}(?![\w-])/g },
  { id: 'aws', re: /\bAKIA[0-9A-Z]{16}\b/g },
  {
    id: 'aws',
    re: new RegExp(`(aws_secret_access_key["']?\\s*[:=]\\s*["']?)${NOT_PLACEHOLDER}[A-Za-z0-9/+=]{40}`, 'gi'),
    keep: keepGroup,
  },
  { id: 'google', re: /\bAIza[\w-]{35}/g },
  { id: 'yandex', re: /\b(?:t1\.[\w-]+=*\.[\w-]{86}=*|AQVN[\w-]{35,38}|y0_[\w-]{50,})/g },
  {
    id: 'bearer',
    re: new RegExp(
      `(Authorization["']?\\s*:\\s*["']?(?:Bearer|Basic|Token)\\s+)${NOT_PLACEHOLDER}[^\\s"']+`,
      'gi',
    ),
    keep: keepGroup,
  },
  {
    id: 'bearer',
    re: new RegExp(`(\\bBearer\\s+)${NOT_PLACEHOLDER}[A-Za-z0-9._~+/=-]{16,}`, 'g'),
    keep: keepGroup,
  },
  {
    id: 'cookie',
    re: new RegExp(`^(\\s*(?:Set-)?Cookie:\\s*)${NOT_PLACEHOLDER}.+$`, 'gim'),
    keep: keepGroup,
  },
  {
    id: 'conn-string',
    re: new RegExp(
      `(\\b(?:postgres(?:ql)?|mysql|mongodb(?:\\+srv)?|rediss?|amqps?)://[^:\\s/@]+:)${NOT_PLACEHOLDER}[^@\\s]+(?=@)`,
      'gi',
    ),
    keep: keepGroup,
  },
  {
    id: 'url-credentials',
    re: new RegExp(`(\\bhttps?://[^\\s:/@]+:)${NOT_PLACEHOLDER}[^\\s@/]+(?=@)`, 'gi'),
    keep: keepGroup,
  },
  {
    id: 'env-assignment',
    re: new RegExp(
      `^(\\s*(?:export\\s+)?[A-Z0-9_]*(?:KEY|SECRET|TOKEN|PASSWORD|PASS|DSN)[A-Z0-9_]*=)${NOT_PLACEHOLDER}(?=\\S).+$`,
      'gm',
    ),
    keep: keepGroup,
  },
  {
    id: 'kv-secret',
    re: new RegExp(
      `(\\b(?:password|passwd|secret|token|api[_-]?key|access[_-]?key|client[_-]?secret)["']?\\s*[:=]\\s*["']?)${NOT_PLACEHOLDER}[^\\s"',;]{6,}`,
      'gi',
    ),
    keep: keepGroup,
  },
];

const BIDI_CONTROLS = /[\u202A-\u202E\u2066-\u2069]/g;

const HIGH_ENTROPY_CANDIDATE = /[A-Za-z0-9+/_=-]{32,}/g;
const HIGH_ENTROPY_BITS = 4.2;

export const shannonEntropy = (value: string): number => {
  const counts = new Map<string, number>();
  for (const char of value) counts.set(char, (counts.get(char) ?? 0) + 1);
  let bits = 0;
  for (const count of counts.values()) {
    const p = count / value.length;
    bits -= p * Math.log2(p);
  }
  return bits;
};

/** A key has a long dense run of letters and digits; paths, flags and `NAME=value` pairs are made of short words. */
const DENSE_RUN = /[A-Za-z0-9]{16,}/g;

/** Commit SHAs, hashes and UUIDs stay; paths (`src/lib/…`, `HOME=/home/…`) are not keys either. */
const looksLikeSecret = (value: string): boolean => {
  if (/^[0-9a-fA-F-]+$/.test(value)) return false;
  const denseRuns = value.match(DENSE_RUN) ?? [];
  if (!denseRuns.some((run) => /[A-Za-z]/.test(run) && /\d/.test(run))) return false;
  if (value.includes('/') && value.split('/').every((part) => /^[a-z0-9._-]*$/.test(part))) return false;
  return shannonEntropy(value) >= HIGH_ENTROPY_BITS;
};

const escapeRegExp = (value: string) => value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/** Home-like prefixes: `/home/<u>`, `/root`, `/var/lib/hl-ai/profiles/<u>/home`, followed by `/` or the end of the path. */
const PATH_END = '(?=/|$|[^A-Za-z0-9_.-])';
/** Not inside a longer path or word, and not the host part of `scheme://`. */
const PATH_START = '(?<![\\w.~-])(?<!:/)';
const GENERIC_HOMES = [
  new RegExp(`${PATH_START}/var/lib/hl-ai/profiles/[A-Za-z0-9_.-]+/home${PATH_END}`, 'g'),
  new RegExp(`${PATH_START}/home/[A-Za-z0-9_.-]+${PATH_END}`, 'g'),
  new RegExp(`${PATH_START}/root${PATH_END}`, 'g'),
] as const;

export interface IRedactor {
  redact: (text: string) => string;
  /** Counters since the redactor was created. */
  report: () => IRedactionReport;
}

export const createRedactor = (context: IRedactionContext): IRedactor => {
  const redactions: Partial<Record<TRedactionRule, number>> = {};
  let pathsShortened = 0;
  const count = (rule: TRedactionRule) => {
    redactions[rule] = (redactions[rule] ?? 0) + 1;
  };

  // Longest first, so a secret that contains another one is hidden whole.
  const knownSecrets = [...new Set(context.knownSecrets.filter((value) => value.length >= 8))].sort(
    (a, b) => b.length - a.length,
  );
  const knownSecretRe =
    knownSecrets.length > 0 ? new RegExp(knownSecrets.map(escapeRegExp).join('|'), 'g') : null;

  const workspaceDir = context.workspaceDir?.replace(/\/+$/, '') || null;
  const workspaceRe = workspaceDir ? new RegExp(`${PATH_START}${escapeRegExp(workspaceDir)}/`, 'g') : null;
  const homeRes = [
    ...new Set(context.homeDirs.map((dir) => dir.replace(/\/+$/, '')).filter((dir) => dir.length > 1)),
  ]
    .sort((a, b) => b.length - a.length)
    .map((dir) => new RegExp(`${PATH_START}${escapeRegExp(dir)}${PATH_END}`, 'g'));

  const shortenPaths = (text: string): string => {
    let result = text;
    if (workspaceRe) {
      result = result.replace(workspaceRe, () => {
        pathsShortened += 1;
        return '';
      });
    }
    for (const re of [...homeRes, ...GENERIC_HOMES]) {
      result = result.replace(re, () => {
        pathsShortened += 1;
        return '~';
      });
    }
    return result;
  };

  const redact = (text: string): string => {
    if (!text) return text;
    let result = text.replace(BIDI_CONTROLS, '');
    if (knownSecretRe) {
      result = result.replace(knownSecretRe, () => {
        count('known-secret');
        return redactionPlaceholder('known-secret');
      });
    }
    for (const rule of PATTERN_RULES) {
      result = result.replace(rule.re, (match: string, ...groups: unknown[]) => {
        count(rule.id);
        const kept = rule.keep
          ? rule.keep(match, ...groups.filter((group): group is string => typeof group === 'string'))
          : '';
        return `${kept}${redactionPlaceholder(rule.id)}`;
      });
    }
    result = shortenPaths(result);
    result = result.replace(HIGH_ENTROPY_CANDIDATE, (match) => {
      if (!looksLikeSecret(match)) return match;
      count('high-entropy');
      return redactionPlaceholder('high-entropy');
    });
    return result;
  };

  return {
    redact,
    report: () => ({ pathsShortened, redactions: { ...redactions } }),
  };
};

const SECRET_ENV_NAME = /KEY|SECRET|TOKEN|PASSWORD/i;

/**
 * Exact secret values of this runtime: NEXTAUTH_SECRET, the auth secret from config, the runtime's CLI token
 * and every env value whose name looks secret. Values that are paths or contain spaces are not secrets.
 */
export const collectEnvSecrets = (env: Readonly<Record<string, string | undefined>>): string[] => {
  const values: string[] = [];
  for (const [name, value] of Object.entries(env)) {
    if (!value || value.length < 8) continue;
    if (name !== 'NEXTAUTH_SECRET' && !SECRET_ENV_NAME.test(name)) continue;
    if (value.startsWith('/') || /\s/.test(value)) continue;
    values.push(value);
  }
  return values;
};
