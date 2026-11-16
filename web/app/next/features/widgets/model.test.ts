import { describe, expect, it } from 'vitest';
import {
  MAX_WIDGETS,
  NO_WIDGETS,
  WIDGET_KIND,
  clockText,
  parseWidgets,
  serializeWidgets,
  shownWidgets,
  smartDue,
  smartEveryMs,
  timerLeft,
  withAdded,
  canAdd,
  withFolded,
  withMoved,
  withNewApps,
  withNote,
  withRemoved,
  withTimer,
} from '@/next/features/widgets/model';

describe('the widgets under the right column (HRN-1510)', () => {
  it('adds, folds, writes a note, starts a timer, moves and removes, and reads back what it wrote', () => {
    let s = withAdded(NO_WIDGETS, WIDGET_KIND.limits) ?? NO_WIDGETS;
    s = withAdded(s, WIDGET_KIND.note) ?? s;
    s = withAdded(s, WIDGET_KIND.clock) ?? s;
    s = withAdded(s, WIDGET_KIND.note) ?? s;
    expect(s.items.map((x) => x.id)).toEqual(['limits', 'note', 'clock', 'note-2']);
    // The limits show the same in every copy: there is one, and the gallery no longer offers them; a note may repeat.
    expect(withAdded(s, WIDGET_KIND.limits)).toBeNull();
    expect(canAdd(s, WIDGET_KIND.limits)).toBe(false);
    expect(canAdd(s, WIDGET_KIND.note)).toBe(true);
    s = withFolded(s, 'limits', true);
    s = withNote(s, 'note', 'купить хлеб');
    s = withTimer(s, 'clock', 1_000_000);
    s = withMoved(s, 'clock', 'limits', false) ?? s;
    expect(s.items.map((x) => x.id)).toEqual(['clock', 'limits', 'note', 'note-2']);
    expect(withMoved(s, 'clock', 'limits', false)).toBeNull();
    s = withRemoved(s, 'note-2');
    const back = parseWidgets(serializeWidgets(s));
    expect(back).toEqual(s);
    expect(back.items[1]).toEqual({ id: 'limits', kind: 'limits', folded: true });
    expect(withFolded(back, 'limits', false).items[1]).toEqual({ id: 'limits', kind: 'limits' });
  });

  it('reads a broken record as nothing and leaves broken items out', () => {
    expect(parseWidgets('{oops')).toEqual(NO_WIDGETS);
    expect(parseWidgets(null)).toEqual(NO_WIDGETS);
    const s = parseWidgets(
      JSON.stringify({
        items: [
          { id: 'a', kind: 'nope' },
          { id: 'b', kind: 'app' },
          { id: 'c', kind: 'machine' },
        ],
      }),
    );
    expect(s.items).toEqual([{ id: 'c', kind: 'machine' }]);
  });

  it('holds at most MAX_WIDGETS', () => {
    let s = NO_WIDGETS;
    for (let i = 0; i < MAX_WIDGETS + 3; i++) s = withAdded(s, WIDGET_KIND.note) ?? s;
    expect(s.items).toHaveLength(MAX_WIDGETS);
  });
});

describe('the widgets an agent makes (HRN-1511)', () => {
  it('adds an app widget met for the first time, once: one the person removed stays removed', () => {
    const found = [{ app: 'local.widget-weather', view: 'side' }];
    const s = withNewApps(NO_WIDGETS, found);
    expect(s?.items).toEqual([
      { id: 'app:local.widget-weather/side', kind: 'app', app: 'local.widget-weather', view: 'side' },
    ]);
    const removed = withRemoved(s ?? NO_WIDGETS, 'app:local.widget-weather/side');
    expect(withNewApps(removed, found)).toBeNull();
    expect(shownWidgets(s ?? NO_WIDGETS, () => false)).toEqual([]);
    expect(shownWidgets(s ?? NO_WIDGETS, null)).toEqual([]);
  });

  it('asks the model no more often than every five minutes', () => {
    expect(smartEveryMs(1)).toBe(5 * 60_000);
    expect(smartEveryMs(undefined)).toBe(30 * 60_000);
    expect(smartDue(null, smartEveryMs(5), 0)).toBe(true);
    expect(smartDue(0, smartEveryMs(5), 4 * 60_000)).toBe(false);
    expect(smartDue(0, smartEveryMs(5), 5 * 60_000)).toBe(true);
  });

  it('tells a timer', () => {
    expect(timerLeft(10_500, 0)).toBe(11);
    expect(timerLeft(0, 5)).toBe(0);
    expect(clockText(65)).toBe('1:05');
    expect(clockText(3723)).toBe('1:02:03');
  });
});
