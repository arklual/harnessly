import { beforeAll, describe, expect, it } from 'vitest';
import { pinLocale } from '@/next/i18n';
import {
  BASE_URL_VAR,
  EMPTY_RULES,
  MAX_ENV_VALUE,
  MAX_RULE_LENGTH,
  buildRule,
  envError,
  ruleAffixes,
  ruleError,
  skillOn,
  skillRule,
  withRule,
  withoutRule,
} from '@/next/features/settings/rules';

beforeAll(() => pinLocale('en'));

describe('buildRule', () => {
  it('writes a tool and a pattern as Claude Code reads them', () => {
    expect(buildRule('Bash', ' npm run * ')).toBe('Bash(npm run *)');
    expect(buildRule('Read', './.env')).toBe('Read(./.env)');
    expect(buildRule('Edit', '')).toBe('Edit');
  });

  it('takes a domain from a pasted address', () => {
    expect(buildRule('WebFetch', 'https://github.com/ly/x')).toBe('WebFetch(domain:github.com)');
    expect(buildRule('WebFetch', 'domain:npmjs.com')).toBe('WebFetch(domain:npmjs.com)');
    expect(buildRule('WebFetch', '')).toBe('WebFetch');
  });

  it('names an MCP server once, and has no rule without one', () => {
    expect(buildRule('mcp', 'gitlab')).toBe('mcp__gitlab');
    expect(buildRule('mcp', 'mcp__gitlab')).toBe('mcp__gitlab');
    expect(buildRule('mcp', '  ')).toBe('');
  });

  it('gives the text around the field', () => {
    expect(ruleAffixes('Bash')).toEqual({ prefix: 'Bash(', suffix: ')' });
    expect(ruleAffixes('WebFetch')).toEqual({ prefix: 'WebFetch(domain:', suffix: ')' });
    expect(ruleAffixes('mcp')).toEqual({ prefix: 'mcp__', suffix: '' });
  });
});

describe('ruleError', () => {
  const rules = { allow: ['Bash(ls)'], ask: [], deny: ['Read(./.env)'] };

  it('refuses an empty, a long and a repeated rule, in words', () => {
    expect(ruleError('', rules)).toBeTruthy();
    expect(ruleError('x'.repeat(MAX_RULE_LENGTH + 1), rules)).toBeTruthy();
    expect(ruleError('Read(./.env)', rules)).toBeTruthy();
    expect(ruleError('Bash(ls)', rules)).toBeTruthy();
  });

  it('takes a new one', () => {
    expect(ruleError('Bash(pwd)', rules)).toBeNull();
    expect(ruleError('x'.repeat(MAX_RULE_LENGTH), rules)).toBeNull();
  });
});

describe('withRule and withoutRule', () => {
  it('change one list and leave the others and the original alone', () => {
    const added = withRule(EMPTY_RULES, 'deny', 'Bash(rm *)');
    expect(added).toEqual({ allow: [], ask: [], deny: ['Bash(rm *)'] });
    expect(EMPTY_RULES.deny).toEqual([]);
    expect(withoutRule(added, 'deny', 'Bash(rm *)')).toEqual(EMPTY_RULES);
    expect(withoutRule(added, 'allow', 'Bash(rm *)')).toEqual(added);
  });
});

describe('skills', () => {
  it('turn a skill off as a deny rule of its own', () => {
    expect(skillRule('release-notes')).toBe('Skill(release-notes)');
    expect(skillOn(EMPTY_RULES, 'release-notes')).toBe(true);
    expect(skillOn(withRule(EMPTY_RULES, 'deny', 'Skill(release-notes)'), 'release-notes')).toBe(false);
    expect(skillOn(withRule(EMPTY_RULES, 'allow', 'Skill(release-notes)'), 'release-notes')).toBe(true);
  });
});

describe('envError', () => {
  const env = [{ key: 'GITLAB_TOKEN', value: 'x', secret: true }];

  it('accepts a name in capitals and a one-line value', () => {
    expect(envError('MY_VAR', 'v', env)).toBeNull();
  });

  it('refuses a bad name, a repeat, the address variable the page edits elsewhere, and a bad value', () => {
    expect(envError('my_var', 'v', env)).toBeTruthy();
    expect(envError('1A', 'v', env)).toBeTruthy();
    expect(envError('GITLAB_TOKEN', 'v', env)).toBeTruthy();
    expect(envError(BASE_URL_VAR, 'https://x', env)).toBeTruthy();
    expect(envError('MY_VAR', '', env)).toBeTruthy();
    expect(envError('MY_VAR', 'a\nb', env)).toBeTruthy();
    expect(envError('MY_VAR', 'a'.repeat(MAX_ENV_VALUE + 1), env)).toBeTruthy();
  });
});
