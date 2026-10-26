import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

// order.ts is the cascade of the interface's one stylesheet; a sheet missing from it lands wherever the bundler puts
// it, and a narrow-screen rule of it can lose to a base rule. The sheets of the views that load later have files of
// their own and stay out, and so does the guest page's own sheet (guest/main.tsx, outside the interface).
const LATER = new Set(['term.css', 'charts.css', 'guest.css']);

describe('the order of the interface styles', () => {
  it('lists every stylesheet of the interface once', () => {
    const listed = [
      ...readFileSync(join(__dirname, 'order.ts'), 'utf8').matchAll(/styles\/([a-z-]+\.css)'/g),
    ].map((m) => m[1]);
    const sheets = readdirSync(__dirname).filter((name) => name.endsWith('.css') && !LATER.has(name));
    expect(new Set(listed).size).toBe(listed.length);
    expect([...listed].sort()).toEqual(sheets.sort());
  });
});
