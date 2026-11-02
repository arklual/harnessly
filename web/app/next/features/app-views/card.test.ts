import { describe, expect, it } from 'vitest';
import { isOwnTool, parseAppCard, pluginToolOf } from '@/next/features/app-views/card';

const result = (uri: string, extra: Record<string, unknown> = {}) =>
  JSON.stringify({ _meta: { ui: { resourceUri: uri } }, structuredContent: { percent: 5 }, ...extra });

const TOOL = 'mcp__plugin_acme-cashback_bank__create';

describe('pluginToolOf', () => {
  it('reads the plugin and the server as the runtime does', () => {
    expect(pluginToolOf(TOOL)).toEqual({ plugin: 'acme-cashback', server: 'bank' });
    expect(pluginToolOf('mcp__plugin_acme-cashback_bank_eu__create')).toEqual({
      plugin: 'acme-cashback',
      server: 'bank_eu',
    });
  });
  it('is null for a tool that is not a plugin’s', () => {
    for (const tool of [
      'Bash',
      'mcp__bank__create',
      'mcp__plugin_acme__create',
      'mcp__plugin__x__y',
      'plugin_a_b__c',
    ])
      expect(pluginToolOf(tool)).toBeNull();
  });
});

describe('parseAppCard', () => {
  it('reads the view, the data and the tool’s origin off a result', () => {
    expect(parseAppCard(TOOL, result('ui://acme.cashback/card'))).toEqual({
      app: 'acme.cashback',
      view: 'card',
      data: { percent: 5 },
      plugin: 'acme-cashback',
      server: 'bank',
    });
  });

  it('ignores whatever is not a card', () => {
    for (const out of [null, '', 'plain text', '{', '[1]', '{"a":1}', '{"_meta":{"ui":{"resourceUri":5}}}'])
      expect(parseAppCard(TOOL, out)).toBeNull();
    for (const uri of [
      'ui://acme.cashback',
      'ui://acme.cashback/../x',
      'https://acme.cashback/card',
      'ui://Acme.cashback/card',
    ])
      expect(parseAppCard(TOOL, result(uri))).toBeNull();
    expect(parseAppCard('mcp__bank__create', result('ui://acme.cashback/card'))).toBeNull();
  });
});

describe('isOwnTool: only the app’s own server may name the app’s view', () => {
  const app = { id: 'acme.cashback', mcpServers: ['bank'] };
  const card = (tool: string, uri = 'ui://acme.cashback/card') => parseAppCard(tool, result(uri))!;

  it('takes the tool of the app’s own server', () => {
    expect(isOwnTool(card(TOOL), app)).toBe(true);
  });

  it('refuses another plugin, a prefix of the plugin name, and a server the package does not have', () => {
    expect(isOwnTool(card('mcp__plugin_acme-other_bank__create'), app)).toBe(false);
    expect(isOwnTool(card('mcp__plugin_acme-cashbackx_bank__create'), app)).toBe(false);
    expect(isOwnTool(card('mcp__plugin_acme-cashback-x_bank__create'), app)).toBe(false);
    expect(isOwnTool(card('mcp__plugin_acme-cashback_evil__create'), app)).toBe(false);
  });

  it('does not take a plugin name that two ids share for the other app', () => {
    // ab.c-d and ab-c.d are both ab-c-d to the agent; the runtime installs only one, and a card names one id.
    const other = { id: 'ab-c.d', mcpServers: ['s'] };
    expect(isOwnTool(parseAppCard('mcp__plugin_ab-c-d_s__t', result('ui://ab.c-d/card'))!, other)).toBe(
      false,
    );
  });
});
