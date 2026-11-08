import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ITourEnv } from '@/next/features/tour/model';
import { TOUR_ORDER, TOUR_TRACKS } from '@/next/features/tour/tracks';
import { dictionary } from '@/next/i18n/dictionary';

const env = (facts: string[], names: Record<string, string> = {}): ITourEnv => ({
  path: '/designly',
  find: (selector) => {
    const text = names[selector];
    return text === undefined ? null : ({ textContent: text } as Element);
  },
  count: () => 0,
  signals: new Set(),
  facts: new Set(facts),
});

const openOf = (track: string) => {
  const step = TOUR_TRACKS[track]?.steps.find((s) => s.id === 'open');
  if (!step?.stage) throw new Error(`${track} has no stage on its open step`);
  return step;
};

describe('the lessons about the demo projects', () => {
  it('say what they show and what they ask, in both languages', () => {
    for (const lang of ['ru', 'en'] as const) {
      const words = dictionary(lang);
      for (const track of Object.values(TOUR_TRACKS)) {
        expect(words[`next.tour.${track.id}.name`], `${lang} ${track.id}.name`).toBeTruthy();
        for (const step of track.steps) {
          expect(
            words[`next.tour.${track.id}.${step.id}.title`],
            `${lang} ${track.id}.${step.id}.title`,
          ).toBeTruthy();
          expect(
            words[`next.tour.${track.id}.${step.id}.text`],
            `${lang} ${track.id}.${step.id}.text`,
          ).toBeTruthy();
          if (step.needs) {
            expect(
              words[`next.tour.${track.id}.${step.id}.needs`],
              `${lang} ${track.id}.${step.id}.needs`,
            ).toBeTruthy();
          }
        }
      }
      for (const id of ['folder', 'up', 'reset', 'gone', 'drawer']) {
        expect(words[`next.tour.stage.${id}`], `${lang} stage.${id}`).toBeTruthy();
      }
      expect(words['next.tour.stage.reset.action']).toBeTruthy();
      expect(words['next.tour.stage.gone.action']).toBeTruthy();
    }
  });

  it('send every Sinopia lesson to its project through the library, step by step', () => {
    const ids = { site: 'relay-site', slides: 'relay-deck', video: 'relay-video', ds: 'relay-ds' } as const;
    for (const track of TOUR_ORDER) {
      const id = ids[track];
      const step = openOf(track);
      // The card is in sight: the step is the card's own.
      expect(step.stage?.(env([`lib:route:${id}:card`]))).toBeNull();
      expect(step.stage?.(env([]))).toBeNull();
      const folder = step.stage?.(
        env([`lib:route:${id}:folder:f-0000e0a1`], {
          '.hx-dl-folder[data-drop-folder="f-0000e0a1"] .hx-dl-folder__name': 'Демо',
        }),
      );
      expect(folder).toMatchObject({
        id: 'folder',
        target: ['.hx-dl-folder[data-drop-folder="f-0000e0a1"] .hx-dl-folder__open'],
        vars: { folder: 'Демо' },
      });
      expect(step.stage?.(env([`lib:route:${id}:up`]))?.target).toEqual([
        '.hx-dl__crumb[data-drop-folder=""]',
      ]);
      expect(step.stage?.(env([`lib:route:${id}:reset`]))).toMatchObject({ id: 'reset', action: 'clear' });
      expect(step.stage?.(env([`lib:route:${id}:gone`]))).toMatchObject({
        id: 'gone',
        action: 'restoreDemo',
      });
    }
  });

  it("read only their own project's way", () => {
    const step = openOf('video');
    expect(step.stage?.(env(['lib:route:relay-site:gone']))).toBeNull();
  });

  describe('on a phone, whose sidebar is a drawer that lies shut', () => {
    afterEach(() => vi.unstubAllGlobals());
    const phone = (matches: boolean) => vi.stubGlobal('window', { matchMedia: () => ({ matches }) });
    const shut = {
      ...env([]),
      find: (s: string) => (s === '.hx-app[data-drawer="closed"]' ? ({} as Element) : null),
    };
    const sidebarStep = () => {
      const step = TOUR_TRACKS.site?.steps.find((s) => s.id === 'sinopia');
      if (!step?.stage) throw new Error('the step about the sidebar has no stage');
      return step;
    };

    it('ring the button that opens it before an item of it', () => {
      phone(true);
      expect(sidebarStep().stage?.(shut)).toMatchObject({ id: 'drawer' });
    });

    it('say nothing of it where the sidebar stands open or the window is wide', () => {
      phone(true);
      expect(sidebarStep().stage?.(env([]))).toBeNull();
      phone(false);
      expect(sidebarStep().stage?.(shut)).toBeNull();
    });
  });
});
