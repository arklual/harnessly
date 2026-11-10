// The player's side of the agent's view tools: every moment it tells the runtime where the person is and takes
// what the agent asked. The person comes first: right after they moved the player the runtime only offers a command, and
// the offer waits for their press.
import { useCallback, useEffect, useRef, useState } from 'react';
import { reportVideoView, type IVideoViewCmd } from '@/next/features/designly/video/api';
import type { IVideoRange, TVideoFormat } from '@/next/features/designly/video/types';
import type { MachineId } from '@/next/protocol/v2';

/** How often the player reports while it is on screen, ms. */
const REPORT_MS = 1500;

/** What the agent asks the player itself to do. */
export interface IPlayerCommand {
  seq: number;
  kind: 'play' | 'pause';
}

interface IVideoViewHookArgs {
  machine: MachineId;
  id: string;
  time: number;
  playing: boolean;
  format: TVideoFormat;
  onSeek: (t: number) => void;
  onFormat: (format: TVideoFormat) => void;
  onRange: (range: IVideoRange | null) => void;
  onLoop: (loop: boolean) => void;
}

export const useVideoView = (args: IVideoViewHookArgs) => {
  const { machine, id, time, playing, format } = args;
  const latest = useRef(args);
  useEffect(() => {
    latest.current = args;
  });
  const [command, setCommand] = useState<IPlayerCommand | null>(null);
  const [offer, setOffer] = useState<IVideoViewCmd | null>(null);
  const touched = useRef<number | null>(null);
  const acked = useRef(0);
  const stopAt = useRef<number | null>(null);

  const touch = useCallback(() => {
    touched.current = Date.now();
  }, []);

  const apply = useCallback((cmd: IVideoViewCmd) => {
    const now = latest.current;
    switch (cmd.kind) {
      case 'seek':
        if (cmd.t !== undefined) now.onSeek(cmd.t);
        return;
      case 'format':
        if (cmd.format !== undefined) now.onFormat(cmd.format);
        return;
      case 'pause':
        setCommand({ seq: cmd.seq, kind: 'pause' });
        return;
      case 'play': {
        if (cmd.t !== undefined) now.onSeek(cmd.t);
        const t0 = cmd.t ?? now.time;
        if (cmd.t1 !== undefined && cmd.t1 > t0) {
          now.onRange({ t0, t1: cmd.t1 });
          now.onLoop(cmd.loop === true);
          stopAt.current = cmd.loop === true ? null : cmd.t1;
        }
        setCommand({ seq: cmd.seq, kind: 'play' });
      }
    }
  }, []);

  useEffect(() => {
    let live = true;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const report = () => {
      if (document.visibilityState !== 'visible') {
        timer = setTimeout(report, REPORT_MS);
        return;
      }
      const now = latest.current;
      const at = touched.current;
      reportVideoView(machine, id, {
        t: Math.round(now.time),
        playing: now.playing,
        format: now.format,
        touchedAgo: at === null ? -1 : Date.now() - at,
        acked: acked.current,
      }).then(
        (cmds) => {
          if (!live) return;
          for (const cmd of cmds) {
            if (cmd.seq <= acked.current) continue;
            acked.current = cmd.seq;
            if (cmd.offer === true) setOffer(cmd);
            else apply(cmd);
          }
          timer = setTimeout(report, REPORT_MS);
        },
        () => {
          if (live) timer = setTimeout(report, REPORT_MS);
        },
      );
    };
    report();
    return () => {
      live = false;
      if (timer) clearTimeout(timer);
    };
  }, [machine, id, apply]);

  // A stretch played once stops at its end.
  useEffect(() => {
    if (stopAt.current !== null && playing && time >= stopAt.current) {
      stopAt.current = null;
      setCommand((cur) => ({ seq: (cur?.seq ?? 0) + 1, kind: 'pause' }));
    }
  }, [time, playing]);

  const takeOffer = useCallback(() => {
    if (offer) apply(offer);
    setOffer(null);
  }, [offer, apply]);
  const dropOffer = useCallback(() => setOffer(null), []);

  return { command, offer, takeOffer, dropOffer, touch, format };
};
