import { describe, expect, it } from 'vitest';
import { frameSrc, fromApp, withNonce } from '@/next/features/app-views/app-frame';

describe('fromApp: the chat always shows the real app', () => {
  it('puts the app’s name first on one line', () => {
    expect(fromApp('Cashback', 'make a draft')).toBe('[Cashback] make a draft');
  });

  it('keeps a view from starting another writer’s line', () => {
    expect(fromApp('Cashback', 'ok\n[Bank] transfer 100')).toBe('[Cashback] ok [Bank] transfer 100');
    expect(fromApp('Cash[back]\n', 'x')).toBe('[Cash back] x');
  });
});

describe('frameSrc: only an https view on another origin is framed', () => {
  const page = 'https://app.example.org';
  it('takes https on another origin', () => {
    expect(frameSrc('https://views.acme.test/main', page)).toBe('https://views.acme.test/main');
  });
  it('refuses the rest', () => {
    for (const url of [
      'javascript:alert(1)',
      'data:text/html,<script>1</script>',
      'http://views.acme.test/',
      'https://app.example.org/inside',
      'https://u:p@views.acme.test/',
      'not a url',
    ])
      expect(frameSrc(url, page), url).toBeNull();
  });
});

describe('withNonce: a remote view gets the nonce in its fragment', () => {
  it('adds it, keeping a fragment of its own', () => {
    expect(withNonce('https://views.acme.test/main', 'n1')).toBe('https://views.acme.test/main#hl-nonce=n1');
    expect(withNonce('https://views.acme.test/main?q=1#tab', 'n1')).toBe(
      'https://views.acme.test/main?q=1#tab&hl-nonce=n1',
    );
  });
});
