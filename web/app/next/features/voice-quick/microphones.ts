// The browser's microphones for the quick voice settings and the «Голосовой ввод» section: the list (names come only
// after the page was allowed a microphone once), the request for that permission, and the live loudness of one device.
import { useEffect, useState, type RefObject } from 'react';

export interface IMicrophone {
  id: string;
  label: string;
}

/** Everyone who shows the list asks it again when the permission was just given (the browser sends no event for that). */
const refreshers = new Set<() => void>();

export interface IMicrophones {
  /** The microphones; null while their names are hidden (no permission yet) or there is none. */
  list: IMicrophone[] | null;
  /** The browser has answered once: before that «no names» is not yet the answer. */
  read: boolean;
}

/** The browser's microphones and whether it has been asked yet. */
export const useMicrophonesRead = (): IMicrophones => {
  // A browser that cannot list devices has nothing to wait for.
  const [found, setFound] = useState<IMicrophones>({
    list: null,
    read: typeof navigator === 'undefined' || !navigator.mediaDevices?.enumerateDevices,
  });
  useEffect(() => {
    const media = navigator.mediaDevices;
    if (!media?.enumerateDevices) return;
    let live = true;
    const read = () =>
      void media.enumerateDevices().then(
        (devices) => {
          if (!live) return;
          const inputs = devices.filter((d) => d.kind === 'audioinput' && d.deviceId && d.label);
          setFound({
            list: inputs.length ? inputs.map((d) => ({ id: d.deviceId, label: d.label })) : null,
            read: true,
          });
        },
        () => setFound((now) => ({ ...now, read: true })),
      );
    read();
    refreshers.add(read);
    media.addEventListener?.('devicechange', read);
    return () => {
      live = false;
      refreshers.delete(read);
      media.removeEventListener?.('devicechange', read);
    };
  }, []);
  return found;
};

/** The browser's microphones; null while their names are hidden (no permission yet) or there is none. */
export const useMicrophones = (): IMicrophone[] | null => useMicrophonesRead().list;

/** Asks the browser for the microphone once so the devices get their names; true when it was given. */
export const allowMicrophone = async (): Promise<boolean> => {
  const media = navigator.mediaDevices;
  if (!media?.getUserMedia) return false;
  try {
    const stream = await media.getUserMedia({ audio: true });
    for (const track of stream.getTracks()) track.stop();
  } catch {
    return false;
  }
  for (const refresh of [...refreshers]) refresh();
  return true;
};

/** How far the loudness is multiplied before it is cut to 0..1: a speaking voice fills most of the meter. */
const GAIN = 4;

/**
 * While `active`, listens to the device (the system's default for null) and writes its loudness, 0..1, into the CSS
 * variable `--level` of the element: no state per frame. It never asks for the permission: call it only once the
 * devices have their names.
 */
export const useMicLevel = (
  active: boolean,
  deviceId: string | null,
  target: RefObject<HTMLElement | null>,
): void => {
  useEffect(() => {
    const media = navigator.mediaDevices;
    if (!active || !media?.getUserMedia || typeof AudioContext === 'undefined') return;
    let live = true;
    let frame = 0;
    let stream: MediaStream | null = null;
    let ctx: AudioContext | null = null;
    const stop = () => {
      live = false;
      cancelAnimationFrame(frame);
      for (const track of stream?.getTracks() ?? []) track.stop();
      void ctx?.close().catch(() => undefined);
      target.current?.style.setProperty('--level', '0');
    };
    media.getUserMedia({ audio: deviceId ? { deviceId: { exact: deviceId } } : true }).then(
      (got) => {
        if (!live) {
          for (const track of got.getTracks()) track.stop();
          return;
        }
        stream = got;
        ctx = new AudioContext();
        void ctx.resume().catch(() => undefined);
        const analyser = ctx.createAnalyser();
        analyser.fftSize = 512;
        ctx.createMediaStreamSource(got).connect(analyser);
        const data = new Uint8Array(analyser.fftSize);
        const tick = () => {
          analyser.getByteTimeDomainData(data);
          let sum = 0;
          for (const value of data) sum += ((value - 128) / 128) ** 2;
          const level = Math.min(1, Math.sqrt(sum / data.length) * GAIN);
          target.current?.style.setProperty('--level', level.toFixed(3));
          frame = requestAnimationFrame(tick);
        };
        tick();
      },
      () => undefined,
    );
    return stop;
  }, [active, deviceId, target]);
};
