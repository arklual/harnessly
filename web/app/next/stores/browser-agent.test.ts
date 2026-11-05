// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest';
import { browserAgentSnapshot, setSiteBlocked } from '@/next/stores/browser-agent';

afterEach(() => {
  for (const host of browserAgentSnapshot().blocked) setSiteBlocked(host, false);
  window.localStorage.clear();
});

describe('the sites blocked for the agent', () => {
  it('start empty, take a site and let it go', () => {
    expect(browserAgentSnapshot().blocked).toEqual([]);
    setSiteBlocked('b.example.com', true);
    setSiteBlocked('a.example.com', true);
    expect(browserAgentSnapshot().blocked).toEqual(['a.example.com', 'b.example.com']);
    expect(JSON.parse(window.localStorage.getItem('hl:browser-agent') ?? '{}')).toEqual({
      blocked: ['a.example.com', 'b.example.com'],
    });
    setSiteBlocked('a.example.com', false);
    expect(browserAgentSnapshot().blocked).toEqual(['b.example.com']);
    setSiteBlocked('b.example.com', false);
    expect(window.localStorage.getItem('hl:browser-agent')).toBeNull();
  });
});
