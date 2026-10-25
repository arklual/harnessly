import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  bindLaunch,
  claimEdit,
  claimOrigin,
  FLIGHT_EASE,
  flightFrames,
  flyCopy,
  forgetFlights,
  hasOrigin,
  launchFrom,
  markEdit,
  noteExit,
  ORIGIN_TTL_MS,
  retargetFlights,
  rowStands,
} from '@/next/ui/flight';

const BOX = { left: 10, top: 20, width: 300, height: 26 };

beforeEach(() => {
  vi.useFakeTimers();
  forgetFlights();
});
afterEach(() => vi.useRealTimers());

describe('the place a sent message flies from', () => {
  it("binds the field's launch to the message it made, once", () => {
    launchFrom(BOX, 'hello');
    bindLaunch('c1');
    expect(hasOrigin('c1')).toBe(true);
    expect(claimOrigin('c1')).toEqual(BOX);
    expect(claimOrigin('c1')).toBeNull();
  });

  it('forgets a launch that no message took, and one that is old', () => {
    launchFrom(BOX, 'refused');
    bindLaunch('c1');
    bindLaunch('c2');
    expect(hasOrigin('c2')).toBe(false);
    launchFrom(BOX, 'late');
    vi.advanceTimersByTime(ORIGIN_TTL_MS + 1);
    bindLaunch('c3');
    expect(hasOrigin('c3')).toBe(false);
  });

  it("takes a queue row's exit in place of the launch", () => {
    launchFrom(BOX, 'text');
    bindLaunch('c1');
    const row = { left: 0, top: 500, width: 200, height: 20 };
    noteExit('c1', row, 'text');
    expect(claimOrigin('c1')).toEqual(row);
  });

  it('counts a message with a row in the queue as one that will fly, until the row is gone', () => {
    const gone = rowStands('c1');
    expect(hasOrigin('c1')).toBe(true);
    gone();
    expect(hasOrigin('c1')).toBe(false);
  });
});

describe('the text that comes back from the queue', () => {
  it('goes to the field that shows it, once', () => {
    markEdit(BOX, 'again please');
    expect(claimEdit('something else')).toBeNull();
    expect(claimEdit('again please\n\nmore')).toEqual(BOX);
    expect(claimEdit('again please')).toBeNull();
  });

  it('is not claimed after a long wait', () => {
    markEdit(BOX, 'again');
    vi.advanceTimersByTime(6000);
    expect(claimEdit('again')).toBeNull();
  });
});

describe('the landing of a sent bubble (no overshoot)', () => {
  const FROM = { left: 40, top: 800, width: 600, height: 120 };
  const TO = { left: 300, top: 500, width: 340, height: 60 };

  it('starts on the text it left and ends with no transform, exactly on the bubble', () => {
    const frames = flightFrames(FROM, TO);
    expect(frames[0].transform).toBe('translate(-260px, 330px) scale(0.96)');
    expect(frames[frames.length - 1]).toMatchObject({ transform: 'none', offset: 1 });
  });

  it('eases out without going past the end (no spring, no back curve)', () => {
    const points = /cubic-bezier\(([^)]+)\)/.exec(FLIGHT_EASE)?.[1].split(',').map(Number) ?? [];
    expect(points).toHaveLength(4);
    expect(points[1]).toBeGreaterThanOrEqual(0);
    expect(points[1]).toBeLessThanOrEqual(1);
    expect(points[3]).toBeGreaterThanOrEqual(0);
    expect(points[3]).toBeLessThanOrEqual(1);
  });

  describe('a place that moves while the copy flies', () => {
    interface IFake {
      style: { values: Record<string, string>; setProperty: (name: string, value: string) => void };
      keyframes: Keyframe[][];
    }
    const fakeCopy = (): IFake & Record<string, unknown> => {
      const keyframes: Keyframe[][] = [];
      const values: Record<string, string> = {};
      return {
        keyframes,
        style: { values, setProperty: (name: string, value: string) => void (values[name] = value) },
        querySelectorAll: () => [],
        setAttribute: () => {},
        classList: { add: () => {} },
        remove: () => {},
        animate: (frames: Keyframe[]) => {
          keyframes.push(frames);
          return {
            effect: { setKeyframes: (next: Keyframe[]) => keyframes.push(next) },
            finished: new Promise(() => {}),
          };
        },
      };
    };
    let frames: FrameRequestCallback[] = [];

    beforeEach(() => {
      frames = [];
      vi.stubGlobal('window', { matchMedia: () => ({ matches: false }) });
      vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => frames.push(callback));
      vi.stubGlobal('cancelAnimationFrame', () => {});
    });
    afterEach(() => vi.unstubAllGlobals());

    it('aims at where the bubble stands now, on every frame and on a layout change', () => {
      const copy = fakeCopy();
      const root = { appendChild: () => {} };
      const source = {
        isConnected: true,
        closest: () => root,
        cloneNode: () => copy,
        animate: () => ({ finished: new Promise(() => {}) }),
      } as unknown as HTMLElement;
      let place = TO;
      expect(flyCopy(source, FROM, () => place)).toBe(true);
      expect(copy.style.values['--fly-y']).toBe('500px');

      // The field shrank and the feed kept its bottom: the bubble went down by 116px under the copy.
      place = { ...TO, top: 616 };
      retargetFlights();
      expect(copy.style.values['--fly-y']).toBe('616px');
      expect(copy.keyframes.at(-1)?.[0].transform).toBe('translate(-260px, 214px) scale(0.96)');
      expect(copy.keyframes.at(-1)?.at(-1)?.transform).toBe('none');

      place = { ...TO, top: 620 };
      frames.shift()?.(0);
      expect(copy.style.values['--fly-y']).toBe('620px');
      const count = copy.keyframes.length;
      frames.shift()?.(16);
      expect(copy.keyframes).toHaveLength(count);
    });
  });
});
