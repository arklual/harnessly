import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

// The tests run without CSS and jsdom lays nothing out, so the rail's one axis is checked in its rules. The geometry
// itself (every mark, icon, the bell and the avatar centred ±0.5px) is measured on ?dev=shell&rail=1.
const css = (name: string): string =>
  readFileSync(join(__dirname, `../../styles/${name}`), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
const shell = css('shell.css');

/** The declarations of the rule whose selector is exactly `selector`. */
const ruleOf = (selector: string): string => {
  const at = shell.indexOf(`${selector} {`);
  expect(at, selector).toBeGreaterThanOrEqual(0);
  return shell.slice(shell.indexOf('{', at) + 1, shell.indexOf('}', at));
};

describe('the folded sidebar keeps one axis', () => {
  it('scrolls its list without a bar, which would narrow the rows and move their marks off the axis', () => {
    expect(ruleOf(".hx-app[data-side='closed'] .hx-sidebar__list")).toMatch(/scrollbar-width:\s*none/);
    expect(ruleOf(".hx-root.hx-app[data-side='closed'] .hx-sidebar__list::-webkit-scrollbar")).toMatch(
      /display:\s*none/,
    );
  });

  it('drops the nested lists’ indent, so a chat of any depth stands in the same column', () => {
    // The rail sets the indent on the lists, not on their `> div`: a tag at the end of a selector under [data-side]
    // restyled every div of the page on each fold. The lists' own rules read it.
    expect(ruleOf(".hx-app[data-side='closed'] :is(.hx-nav__kids, .hx-tree__kids)")).toMatch(
      /--hx-kids-indent:\s*0/,
    );
    for (const [file, selector] of [
      ['sidebar.css', '.hx-nav__kids > div'],
      ['delegation.css', '.hx-tree__kids > div'],
    ] as const) {
      const sheet = css(file);
      const at = sheet.indexOf(`${selector} {`);
      expect(at, `${file} ${selector}`).toBeGreaterThanOrEqual(0);
      const rule = sheet.slice(sheet.indexOf('{', at) + 1, sheet.indexOf('}', at));
      for (const prop of ['margin-left', 'padding-left', 'border-left'])
        expect(rule, `${file} ${prop}`).toMatch(new RegExp(`${prop}:[^;]*var\\(--hx-kids-indent`));
    }
    expect(ruleOf(".hx-app[data-side='closed'] .hx-item[data-launch]")).toMatch(/margin-left:\s*0/);
  });

  it('puts a chat’s mark in the middle of its row, so the open chat’s light is centred round it', () => {
    const mark = ruleOf(".hx-app[data-side='closed'] .hx-item__mark");
    expect(mark).toMatch(/left:\s*50%/);
    expect(mark).toMatch(/margin-left:\s*-9px/);
  });
});
