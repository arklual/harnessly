// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { inlineAssets, isRelative, joinRelative } from '@/next/features/pane/editor/html-assets';

describe('an html page gets the files beside it', () => {
  it('reads only names beside the page, and joins them to its folder', () => {
    expect(isRelative('css/site.css')).toBe(true);
    expect(isRelative('./a.js')).toBe(true);
    expect(isRelative('https://cdn.example/x.js')).toBe(false);
    expect(isRelative('//cdn.example/x.js')).toBe(false);
    expect(isRelative('/abs.css')).toBe(false);
    expect(isRelative('data:image/png;base64,AA')).toBe(false);
    expect(isRelative('#top')).toBe(false);
    expect(joinRelative('site/pages', '../css/a.css?v=2')).toBe('site/css/a.css');
    expect(joinRelative('/tmp/out', './img/b.png#x')).toBe('/tmp/out/img/b.png');
    expect(joinRelative('', 'a.css')).toBe('a.css');
  });

  it('never climbs above the chat folder, or above its own folder for a page elsewhere', () => {
    expect(joinRelative('site', '../../secret.txt')).toBeNull();
    expect(joinRelative('/tmp/out', '../x.txt', '/tmp/out')).toBeNull();
    expect(joinRelative('/tmp/out/a', '../x.css', '/tmp/out')).toBe('/tmp/out/x.css');
  });

  it('cuts a page that got files off the network; a page that got none keeps its own addresses', async () => {
    const read = async () => new Blob(['x']);
    const sealed = await inlineAssets('<img src="a.png">', '/home/me/page.html', read);
    expect(sealed).toContain('http-equiv="Content-Security-Policy"');
    expect(sealed).toContain("connect-src 'none'");
    const fenced: string[] = [];
    const open = await inlineAssets('<img src="../../etc/hosts">', '/home/me/page.html', async (p) => {
      fenced.push(p);
      return null;
    });
    expect(fenced).toEqual([]);
    expect(open).not.toContain('Content-Security-Policy');
  });

  it('puts styles, scripts, pictures and the fonts of a stylesheet into the page', async () => {
    const files: Record<string, string> = {
      'site/css/a.css': 'body{background:url(../img/bg.png)}',
      'site/img/bg.png': 'PNG',
      'site/app.js': 'window.ok=1',
      'site/logo.svg': '<svg/>',
    };
    const asked: string[] = [];
    const read = async (path: string) => {
      asked.push(path);
      return path in files ? new Blob([files[path]]) : null;
    };
    const html =
      '<html><head><link rel="stylesheet" href="css/a.css"><script src="https://cdn.example/x.js"></script></head>' +
      '<body><img src="logo.svg"><img src="missing.png"><script src="app.js"></script></body></html>';
    const page = await inlineAssets(html, 'site/index.html', read);
    expect(page).toContain('<style>body{background:url("data:');
    expect(page).toContain('window.ok=1');
    expect(page).toContain('src="data:');
    expect(page).toContain('src="missing.png"');
    expect(page).toContain('https://cdn.example/x.js');
    expect(asked).not.toContain('https://cdn.example/x.js');
  });
});
