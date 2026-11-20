import { describe, expect, it } from 'vitest';
import { frameSrc, fromApp, shookAlready, withNonce } from '@/next/features/app-views/app-frame';
import * as appFrame from '@/next/features/app-views/app-frame';

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

describe('remoteViewUrl: an object fills only a declared remote-view placeholder', () => {
  const entry = 'https://whiteboard.t-tech.team/app/board/{object}?isExternal=true';
  const board = '20ae6de9872bef73f5f8';
  const expected = `https://whiteboard.t-tech.team/app/board/${board}?isExternal=true`;

  it('resolves literal and URL-encoded placeholders', () => {
    expect(appFrame.remoteViewUrl(entry, { object: board })).toBe(expected);
    expect(appFrame.remoteViewUrl(entry.replace('{object}', '%7Bobject%7D'), { object: board })).toBe(
      expected,
    );
  });

  it('refuses a missing or unsafe object and leaves ordinary entries unchanged', () => {
    expect(appFrame.remoteViewUrl(entry, undefined)).toBeUndefined();
    expect(appFrame.remoteViewUrl(entry, { object: `${board}?allow_guest=true` })).toBeUndefined();
    expect(appFrame.remoteViewUrl('https://views.acme.test/main', undefined)).toBe(
      'https://views.acme.test/main',
    );
  });
});

describe('remote session recovery and editor permissions', () => {
  it('renews only the bridge session of a remote view, never its document', () => {
    let renewed = 0,
      reloaded = 0;
    appFrame.recoverViewSession(
      true,
      () => {
        renewed += 1;
      },
      () => {
        reloaded += 1;
      },
    );
    expect(renewed).toBe(1);
    expect(reloaded).toBe(0);
    appFrame.recoverViewSession(
      false,
      () => {
        renewed += 1;
      },
      () => {
        reloaded += 1;
      },
    );
    expect(reloaded).toBe(1);
  });

  it('lets official editors download, use the clipboard and open a normal tab', () => {
    expect(appFrame.REMOTE_FRAME_SANDBOX).toContain('allow-downloads');
    expect(appFrame.REMOTE_FRAME_SANDBOX).toContain('allow-same-origin');
    expect(appFrame.REMOTE_FRAME_SANDBOX).toContain('allow-popups-to-escape-sandbox');
    expect(appFrame.APP_FRAME_SANDBOX).toContain('allow-popups-to-escape-sandbox');
    expect(appFrame.REMOTE_FRAME_ALLOW).toBe('clipboard-read; clipboard-write; fullscreen');
  });
});

describe('shookAlready: a bridge made anew for a document that shook hands is dead', () => {
  const frame = { id: 'f' };
  const src = 'https://views.example.test/b/app#hl-nonce=n1';
  it('the same frame and document: the view will not shake hands again, so the frame loads anew', () => {
    expect(shookAlready({ frame, src }, frame, src)).toBe(true);
  });
  it('a new document or another frame shakes hands by itself', () => {
    expect(shookAlready(null, frame, src)).toBe(false);
    expect(shookAlready({ frame, src }, frame, 'https://views.example.test/b/app#hl-nonce=n2')).toBe(false);
    expect(shookAlready({ frame, src }, { id: 'g' }, src)).toBe(false);
    expect(shookAlready({ frame, src }, frame, null)).toBe(false);
  });
});
