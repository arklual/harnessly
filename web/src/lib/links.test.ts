import { describe, expect, it } from 'vitest';

import { linkTitle, recognize, themedEmbed } from './links';

const services = {
  boards: { url: 'https://boards.example.com' },
  artifacts: { url: 'http://localhost:8091' },
};

describe('recognize', () => {
  it('reads a board link with its key', () => {
    expect(
      recognize(
        'https://boards.example.com/b/checkout-architecture-k3j2h1g0f9e8?k=abcdefghijklmnopqrstuv',
        services,
      ),
    ).toEqual({
      kind: 'board',
      slug: 'checkout-architecture-k3j2h1g0f9e8',
      key: 'abcdefghijklmnopqrstuv',
      path: '/b/checkout-architecture-k3j2h1g0f9e8?k=abcdefghijklmnopqrstuv',
      url: 'https://boards.example.com/b/checkout-architecture-k3j2h1g0f9e8?k=abcdefghijklmnopqrstuv',
    });
  });

  it('keeps a checkpoint and drops the embed flag and other parameters', () => {
    const link = recognize(
      'https://boards.example.com/b/plan-abc123def456?k=key&embed=1&at=cp1&utm=x',
      services,
    );
    expect(link?.kind === 'board' && link.path).toBe('/b/plan-abc123def456?k=key&at=cp1');
  });

  it('reads documents from the reader and the embed address, with a version', () => {
    expect(recognize('http://localhost:8091/a/api-rate-limits-4k2m9x', services)).toMatchObject({
      kind: 'document',
      slug: 'api-rate-limits-4k2m9x',
      version: null,
      embedUrl: 'http://localhost:8091/embed/api-rate-limits-4k2m9x',
    });
    expect(recognize('http://localhost:8091/embed/api-rate-limits-4k2m9x?v=3', services)).toMatchObject({
      version: '3',
      url: 'http://localhost:8091/a/api-rate-limits-4k2m9x?v=3',
      embedUrl: 'http://localhost:8091/embed/api-rate-limits-4k2m9x?v=3',
    });
  });

  it('ignores other hosts, other paths and services that are not configured', () => {
    for (const href of [
      'https://evil.example/b/plan-abc123def456?k=key',
      'https://boards.example.com.evil.example/b/plan-abc?k=k',
      'https://boards.example.com/b/api/boards',
      'https://boards.example.com/b/api',
      'https://boards.example.com/b/auth?next=/b/x',
      'https://boards.example.com/b/plan-abc/scene.excalidraw?k=k',
      'https://boards.example.com/b/A_B?k=k',
      'http://localhost:8091/a/slug/raw',
      'http://localhost:8092/a/plan-4k2m9x',
      'javascript:alert(1)',
      'not a url',
    ])
      expect(recognize(href, services)).toBeNull();
    expect(
      recognize('https://boards.example.com/b/plan-abc123def456', { boards: null, artifacts: null }),
    ).toBeNull();
  });
});

describe('linkTitle', () => {
  it('drops the random suffix of a slug', () => {
    expect(
      linkTitle({ kind: 'board', slug: 'checkout-architecture-k3j2h1g0f9e8', key: null, path: '', url: '' }),
    ).toBe('Checkout architecture');
    expect(linkTitle({ kind: 'document', slug: 'roadmap', version: null, url: '', embedUrl: '' })).toBe(
      'Roadmap',
    );
  });
});

describe('themedEmbed', () => {
  it('sets the theme next to a version', () => {
    expect(themedEmbed('http://localhost:8091/embed/a-1x?v=2', 'dark')).toBe(
      'http://localhost:8091/embed/a-1x?v=2&theme=dark',
    );
  });
});
