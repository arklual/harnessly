// The board's reload (internal/design/texts/client.js, hardReload): a board on an opaque origin (a slot by path) does not
// navigate itself, which a proxy in front of the platform refuses with a page that may not be framed; it asks
// the framing page and reloads itself only when nobody answered. The function is cut out of the script and run with
// stubs for the page around it.
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const cut = (file: string) => {
  const source = readFileSync(resolve(process.cwd(), `../internal/design/texts/${file}`), 'utf8');
  const start = source.indexOf('  var HOST_RELOAD_WAIT');
  const end = source.indexOf('  var applied = function');
  if (start < 0 || end < start)
    throw new Error(`the reload part of ${file} is not where the test looks for it`);
  return source.slice(start, end);
};

interface IPage {
  origin: string;
  framed: boolean;
}

const run = (part: string, page: IPage) => {
  const reload = vi.fn();
  const posted: [unknown, string][] = [];
  const parent = { postMessage: (data: unknown, target: string) => void posted.push([data, target]) };
  const self = { scrollY: 120 } as { scrollY: number; parent?: unknown };
  self.parent = page.framed ? parent : self;
  const make = new Function(
    'location',
    'window',
    'scrollKey',
    'sessionStorage',
    `${part}\nreturn hardReload;`,
  );
  const hardReload = make({ origin: page.origin, reload }, self, () => 'k', {
    setItem: () => undefined,
  }) as () => void;
  return { hardReload, reload, posted };
};

beforeEach(() => {
  vi.useFakeTimers();
});
afterEach(() => {
  vi.useRealTimers();
});

describe.each(['client.js', 'client.ru.js'])('the board’s reload (%s)', (file) => {
  const part = cut(file);

  it('on an opaque origin in a frame asks the host and does not navigate itself', () => {
    const page = run(part, { origin: 'null', framed: true });
    page.hardReload();
    expect(page.posted).toEqual([[{ type: 'tk:reload' }, '*']]);
    expect(page.reload).not.toHaveBeenCalled();
    page.hardReload();
    expect(page.posted).toHaveLength(1);
  });

  it('reloads itself when no host took the frame away in time', () => {
    const page = run(part, { origin: 'null', framed: true });
    page.hardReload();
    vi.advanceTimersByTime(1499);
    expect(page.reload).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(page.reload).toHaveBeenCalledTimes(1);
  });

  it('on an origin of its own reloads at once, as before', () => {
    const page = run(part, { origin: 'http://localhost:4800', framed: true });
    page.hardReload();
    expect(page.posted).toEqual([]);
    expect(page.reload).toHaveBeenCalledTimes(1);
  });

  it('opened on its own (not framed) reloads at once', () => {
    const page = run(part, { origin: 'null', framed: false });
    page.hardReload();
    expect(page.posted).toEqual([]);
    expect(page.reload).toHaveBeenCalledTimes(1);
  });
});
