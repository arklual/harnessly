import { describe, expect, it } from 'vitest';
import { defaultSites, LINK_PLACE, linkPlace, opensOutside, siteOf } from '@/next/features/chat/pane-sites';
import { appConfig, type IAppConfig } from '@/platform/config';

const config = (over: Partial<IAppConfig>): IAppConfig => ({ ...appConfig(), ...over });

describe('a site as typed', () => {
  it('has one written form', () => {
    expect(siteOf('https://PostHog.Example.com/')).toBe('posthog.example.com');
    expect(siteOf('example.com/adminpanel/')).toBe('example.com/adminpanel');
    expect(siteOf('*.preview.example.com')).toBe('*.preview.example.com');
    expect(siteOf('example.com/admin?x=1')).toBe('example.com/admin');
  });

  it('is no site when it is not an address', () => {
    expect(siteOf('')).toBeNull();
    expect(siteOf('not a site')).toBeNull();
    expect(siteOf('*.example.com/path')).toBeNull();
    expect(siteOf('javascript:alert(1)')).toBeNull();
  });
});

describe('where a link opens', () => {
  const sites = [
    'posthog.example.com',
    '*.preview.example.com',
    'example.com/adminpanel',
    'ai.example.com/agents/docs',
  ];

  it('a site of the list opens in the pane', () => {
    expect(linkPlace('https://posthog.example.com/project/1/dashboard/21', sites)).toBe(LINK_PLACE.pane);
    expect(linkPlace('https://stand-1.preview.example.com/x', sites)).toBe(LINK_PLACE.pane);
    expect(linkPlace('https://example.com/adminpanel/events', sites)).toBe(LINK_PLACE.pane);
    expect(linkPlace('https://example.com/adminpanel', sites)).toBe(LINK_PLACE.pane);
    expect(linkPlace('https://ai.example.com/agents/docs/plan', sites)).toBe(LINK_PLACE.pane);
  });

  it('any other site opens outside', () => {
    expect(linkPlace('https://github.com/a/b', sites)).toBe(LINK_PLACE.outside);
    expect(linkPlace('https://example.com/', sites)).toBe(LINK_PLACE.outside);
    expect(linkPlace('https://example.com/adminpanelx', sites)).toBe(LINK_PLACE.outside);
    expect(linkPlace('https://preview.example.com/', sites)).toBe(LINK_PLACE.outside);
    expect(linkPlace('https://ai.example.com/agents/chat/1', sites)).toBe(LINK_PLACE.outside);
    expect(linkPlace('https://evil-posthog.example.com/', sites)).toBe(LINK_PLACE.outside);
  });

  it("a server of the machine is the pane's: the person's browser cannot reach it", () => {
    expect(linkPlace('http://localhost:4400/x', [])).toBe(LINK_PLACE.pane);
    expect(opensOutside('http://localhost:4400/x')).toBe(false);
    expect(opensOutside('https://example.com')).toBe(true);
    expect(opensOutside('javascript:alert(1)')).toBe(false);
  });

  it('mail opens outside', () => {
    expect(linkPlace('mailto:a@example.com', sites)).toBe(LINK_PLACE.outside);
  });
});

describe("the installation's sites", () => {
  it('are its documents, boards, reader, stands, tracker, own GitLab and the extra hosts', () => {
    expect(
      defaultSites(
        config({
          artifactsUrl: 'https://a.example.com',
          boardsUrl: 'https://a.example.com',
          readerUrl: 'https://ai.example.com/agents/docs/{slug}',
          standDomain: '.preview.example.com',
          trackerHosts: ['tracker.example.com'],
          gitlabHosts: ['gitlab.example.com', 'gitlab.com'],
          paneHosts: ['posthog.example.com', 'example.com/adminpanel'],
          defaultPins: [{ text: 'my.test.example.com', name: 'Админка' }],
        }),
      ),
    ).toEqual([
      'a.example.com',
      'ai.example.com/agents/docs',
      '*.preview.example.com',
      'tracker.example.com',
      'gitlab.example.com',
      'posthog.example.com',
      'example.com/adminpanel',
      'my.test.example.com',
    ]);
  });
});
