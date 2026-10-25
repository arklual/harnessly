// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import {
  cleanHtml,
  frameDocument,
  FRAME_SANDBOX,
  isFrameHeight,
  policyOf,
} from '@/next/features/chat/html-doc';

describe('the sandbox of an HTML answer', () => {
  it('never shares the page origin and grants no way out', () => {
    const grants = FRAME_SANDBOX.split(/\s+/);
    expect(grants).toEqual(['allow-scripts']);
    for (const bad of [
      'allow-same-origin',
      'allow-top-navigation',
      'allow-popups',
      'allow-forms',
      'allow-modals',
    ])
      expect(grants).not.toContain(bad);
  });

  it("allows no network: nothing but inline styles, data pictures and the page's own script by nonce", () => {
    const policy = policyOf('abc');
    expect(policy).toContain("default-src 'none'");
    expect(policy).toContain("script-src 'nonce-abc'");
    expect(policy).not.toMatch(/connect-src|http|\*/);
    expect(policy).toContain("form-action 'none'");
  });

  it('puts the policy before anything the agent wrote', () => {
    const doc = frameDocument('<script>fetch("/api")</script><p>hi</p>', {
      key: 'k',
      nonce: 'n0nce',
      fit: true,
    });
    // The only script left is ours, with the nonce; the agent's is gone.
    expect(doc.match(/<script/g)).toHaveLength(1);
    expect(doc).toContain('<script nonce="n0nce">');
    expect(doc).not.toContain('fetch("/api")');
    expect(doc).toContain('<p>hi</p>');
  });

  it('reports no height when the frame fills the viewer', () => {
    const doc = frameDocument('<p>x</p>', { key: 'k', nonce: 'n', fit: false });
    expect(doc).not.toContain('postMessage');
  });

  it("listens only to its own frame's height message", () => {
    expect(isFrameHeight({ hxFrame: 'k', height: 120 }, 'k')).toBe(true);
    expect(isFrameHeight({ hxFrame: 'other', height: 120 }, 'k')).toBe(false);
    expect(isFrameHeight({ hxFrame: 'k', height: '120' }, 'k')).toBe(false);
    expect(isFrameHeight({ hxFrame: 'k', height: Infinity }, 'k')).toBe(false);
    expect(isFrameHeight(null, 'k')).toBe(false);
  });

  it("strips what could steer the frame out of the agent's page", () => {
    const out = cleanHtml(
      '<meta http-equiv="refresh" content="0;url=https://x.example"><base href="https://x.example/">' +
        '<link rel="stylesheet" href="https://x.example/a.css"><style>p{color:red}</style>' +
        '<form action="https://x.example"><button formaction="https://y.example">go</button></form>' +
        '<iframe src="https://x.example"></iframe><object data="x"></object><embed src="x">' +
        '<a href="https://x.example" target="_blank" ping="https://y.example">link</a><script>1</script><p>kept</p>',
    );
    for (const gone of [
      '<meta',
      '<base',
      '<link',
      '<form',
      '<iframe',
      '<object',
      '<embed',
      '<script',
      'target=',
      'ping=',
      'formaction',
    ])
      expect(out).not.toContain(gone);
    expect(out).toContain('<style>p{color:red}</style>');
    expect(out).toContain('<p>kept</p>');
    expect(out).toContain('>link</a>');
  });

  it('puts the cleaned page in the document, not the raw one', () => {
    const doc = frameDocument('<meta http-equiv="refresh" content="0;url=https://x.example"><p>hi</p>', {
      key: 'k',
      nonce: 'n',
      fit: false,
    });
    expect(doc).not.toContain('refresh');
    expect(doc).toContain('<p>hi</p>');
  });
});
