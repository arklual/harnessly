import { describe, expect, it } from 'vitest';
import {
  KIT_PART,
  kitPreviewHref,
  nextVersion,
  versionPageHref,
} from '@/next/features/designly/versions/versions-api';

describe('versions-api', () => {
  it('counts the next version as the runtime does', () => {
    expect(nextVersion('0.5.1', KIT_PART.minor)).toBe('0.6.0');
    expect(nextVersion('0.5.1', KIT_PART.patch)).toBe('0.5.2');
    expect(nextVersion('0.5.1', KIT_PART.major)).toBe('1.0.0');
    expect(nextVersion('1.2.3-rc.1', KIT_PART.patch)).toBe('1.2.4');
  });

  it('addresses a page at a version and on another kit version', () => {
    const rev = 'a'.repeat(40);
    expect(versionPageHref('http://127.0.0.1:7/', 'deck', rev, 'Слайд 1.dc.html?x=1')).toBe(
      `http://127.0.0.1:7/a/deck/r/${rev}/${encodeURIComponent('Слайд 1.dc.html')}`,
    );
    expect(kitPreviewHref('http://127.0.0.1:7', 'deck', '0.6.0', 'lib/page.html')).toBe(
      'http://127.0.0.1:7/a/deck/k/0.6.0/lib/page.html',
    );
  });
});
