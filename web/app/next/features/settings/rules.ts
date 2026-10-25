// Claude Code's permission rules and environment, as the builder on the page writes them: a tool and a pattern become
// the rule string Claude Code reads (`Bash(npm run *)`, `WebFetch(domain:github.com)`, `mcp__gitlab`).
import { t } from '@/next/i18n';
import type { AgentEnvVar, AgentRules } from '@/next/protocol/v2';

export const RULE_KINDS = ['allow', 'ask', 'deny'] as const;
export type RuleKind = (typeof RULE_KINDS)[number];

export const RULE_TOOLS = [
  { id: 'Bash', icon: 'square-terminal', suggestions: ['npm run *', 'git diff *', 'git push *'] },
  { id: 'Read', icon: 'file-search', suggestions: ['./src/**', './.env', '~/.ssh/**'] },
  { id: 'Edit', icon: 'file-pen', suggestions: ['./src/**', './package.json'] },
  { id: 'WebFetch', icon: 'globe', suggestions: ['github.com', 'npmjs.com'] },
  { id: 'mcp', icon: 'plug', suggestions: [] },
] as const;
export type RuleToolId = (typeof RULE_TOOLS)[number]['id'];

/** The text before and after the pattern in the builder's field. */
export const ruleAffixes = (tool: RuleToolId): { prefix: string; suffix: string } => {
  if (tool === 'mcp') return { prefix: 'mcp__', suffix: '' };
  if (tool === 'WebFetch') return { prefix: 'WebFetch(domain:', suffix: ')' };
  return { prefix: `${tool}(`, suffix: ')' };
};

/** The rule a tool and a pattern make; '' while an MCP rule has no server. */
export const buildRule = (tool: RuleToolId, raw: string): string => {
  const value = raw.trim();
  if (tool === 'mcp') return value ? `mcp__${value.replace(/^mcp__/, '')}` : '';
  if (tool === 'WebFetch') {
    const domain = value
      .replace(/^domain:/, '')
      .replace(/^https?:\/\//, '')
      .replace(/\/.*$/, '');
    return value ? `WebFetch(domain:${domain})` : 'WebFetch';
  }
  return value ? `${tool}(${value})` : tool;
};

/** The longest rule the runtime takes (wire.MaxAgentRuleLen). */
export const MAX_RULE_LENGTH = 300;

/** Why a rule cannot be added, in words; null when it can. */
export const ruleError = (rule: string, rules: AgentRules): string | null => {
  if (!rule) return t('next.settings.rules.errMcp');
  if (rule.length > MAX_RULE_LENGTH) return t('next.settings.rules.errLong');
  if ([...rules.allow, ...rules.ask, ...rules.deny].includes(rule)) return t('next.settings.rules.errTwice');
  return null;
};

/** The rules of a file that has none. */
export const EMPTY_RULES: AgentRules = { allow: [], ask: [], deny: [] };

export const withRule = (rules: AgentRules, kind: RuleKind, rule: string): AgentRules => ({
  ...rules,
  [kind]: [...rules[kind], rule],
});

export const withoutRule = (rules: AgentRules, kind: RuleKind, rule: string): AgentRules => ({
  ...rules,
  [kind]: rules[kind].filter((item) => item !== rule),
});

/** A skill Claude Code is told not to use is a deny rule of its own. */
export const skillRule = (name: string): string => `Skill(${name})`;

export const skillOn = (rules: AgentRules, name: string): boolean => !rules.deny.includes(skillRule(name));

/** The variable the requests' address lives in: the page edits it as the `baseUrl` option, not here. */
export const BASE_URL_VAR = 'ANTHROPIC_BASE_URL';

const ENV_NAME = /^[A-Z_][A-Z0-9_]{0,63}$/;
/** The longest value the runtime takes (wire.MaxAgentEnvValueLen). */
export const MAX_ENV_VALUE = 2000;

/** Why a variable cannot be added, in words; null when it can. */
export const envError = (key: string, value: string, env: readonly AgentEnvVar[]): string | null => {
  if (!ENV_NAME.test(key)) return t('next.settings.env.errName');
  if (key === BASE_URL_VAR) return t('next.settings.env.errBaseUrl');
  if (env.some((item) => item.key === key)) return t('next.settings.env.errTwice');
  if (!value) return t('next.settings.env.errValue');
  if (value.length > MAX_ENV_VALUE || /[\r\n\0]/.test(value)) return t('next.settings.env.errValue');
  return null;
};
