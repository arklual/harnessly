import { describe, expect, it } from 'vitest';
import { entryScriptOf, entryScriptOfPage, isNewerBuild, moduleScriptSources } from '@/lib/app-update';

const page = (entry: string) => `<!doctype html><html><head>
<base href="/agents/" />
<script>var theme = 1;</script>
<script type="module" crossorigin src="${entry}"></script>
<link rel="stylesheet" crossorigin href="./assets/index-a.css">
</head><body><div id="root"></div></body></html>`;

describe('app update check', () => {
  it('reads the module script and skips inline and classic scripts', () => {
    const html = `${page('./assets/index-abc.js')}<script src="/classic.js"></script>`;
    expect(moduleScriptSources(html)).toEqual(['./assets/index-abc.js']);
  });

  it('reads attributes in any order and quoting', () => {
    expect(moduleScriptSources(`<script src='/a.js' type=module></script>`)).toEqual(['/a.js']);
    expect(moduleScriptSources(`<script crossorigin type="module" src="/b.js"></script>`)).toEqual(['/b.js']);
  });

  it('gives a relative and an absolute spelling the same address', () => {
    const base = 'https://hl.example/agents/';
    expect(entryScriptOfPage(page('./assets/index-abc.js'), base)).toBe('/agents/assets/index-abc.js');
    expect(entryScriptOfPage(page('/agents/assets/index-abc.js'), base)).toBe('/agents/assets/index-abc.js');
    expect(entryScriptOf(['https://hl.example/agents/assets/index-abc.js'], base)).toBe(
      '/agents/assets/index-abc.js',
    );
  });

  it('has no entry in a page without a module script', () => {
    expect(entryScriptOfPage('<html><body>sign in</body></html>', 'https://hl.example/')).toBeNull();
    expect(entryScriptOf([], 'https://hl.example/')).toBeNull();
  });

  it('is newer only when both are known and differ', () => {
    const base = 'https://hl.example/agents/';
    const running = entryScriptOfPage(page('./assets/index-abc.js'), base);
    expect(isNewerBuild(running, entryScriptOfPage(page('./assets/index-abc.js'), base))).toBe(false);
    expect(isNewerBuild(running, entryScriptOfPage(page('./assets/index-def.js'), base))).toBe(true);
    expect(isNewerBuild(null, '/agents/assets/index-def.js')).toBe(false);
    expect(isNewerBuild(running, null)).toBe(false);
  });
});
