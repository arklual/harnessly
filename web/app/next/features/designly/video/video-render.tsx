// «Собрать MP4» (a popover of the bar): per enabled format the published file's badge — current while its inputs key is
// today's, outdated otherwise — the render of the whole video (final, or a quick draft that is never published), the
// running job's progress and the download. One render runs at a time, so every button waits for it. Starting is
// pessimistic: the job shows once the runtime has accepted it. A running job is followed here every second.
import { useEffect, useState } from 'react';
import { RuntimeError } from '@/next/core/net/runtime-json';
import { DECK_JOB } from '@/next/features/designly/deck/types';
import { startVideoRender, videoFileHref, videoRenderJob } from '@/next/features/designly/video/api';
import { RENDER_BADGE, jobActive, renderBadge, renderOf } from '@/next/features/designly/video/video-model';
import type { IVideoState, TVideoFormat } from '@/next/features/designly/video/types';
import type { MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import { putVideoJob } from '@/next/stores/video';
import { PRESENCE, type TPresenceState } from '@/next/ui/presence';
import { toast } from '@/next/features/designly/sdk/toast';
import Icon from '@/next/ui/icon';

const POLL_MS = 1000;

export interface IVideoRenderProps {
  machine: MachineId;
  id: string;
  video: IVideoState;
  state: TPresenceState;
}

const BADGE_KEY = {
  current: 'next.video.shell.current',
  outdated: 'next.video.shell.outdated',
} as const;

const failedText = (failure: unknown) =>
  failure instanceof RuntimeError ? failure.message : t('next.failure.http');

/** Why nothing can be rendered on this machine; null when it can. */
const blockedBy = (video: IVideoState): string | null => {
  if (!video.chrome) return t('next.video.shell.needsChrome');
  if (!video.ffmpeg) return t('next.video.shell.needsFfmpeg');
  return null;
};

/** Follows the running job until it ends; a failed one is said once. */
const useJobFollow = (machine: MachineId, id: string, job: IVideoState['job']) => {
  const jobId = jobActive(job) ? job.id : null;
  useEffect(() => {
    if (jobId === null) return;
    const abort = new AbortController();
    const timer = setInterval(() => {
      videoRenderJob(machine, id, jobId, abort.signal).then(
        (next) => {
          if (abort.signal.aborted) return;
          putVideoJob(id, next);
          if (next.state === DECK_JOB.failed) {
            toast(t('next.video.shell.renderFailed'), {
              tone: 'error',
              description: next.error ?? undefined,
            });
          }
        },
        () => undefined,
      );
    }, POLL_MS);
    return () => {
      abort.abort();
      clearInterval(timer);
    };
  }, [machine, id, jobId]);
};

const VideoRender = ({ machine, id, video, state }: IVideoRenderProps) => {
  const [starting, setStarting] = useState(false);
  useJobFollow(machine, id, video.job);
  const running = jobActive(video.job) ? video.job : null;
  const reason = blockedBy(video);
  const idle = !starting && running === null && reason === null;

  const start = (format: TVideoFormat, draft: boolean) => {
    if (!idle) return;
    setStarting(true);
    startVideoRender(machine, id, format, draft)
      .then((job) => putVideoJob(id, job))
      .catch((failure: unknown) =>
        toast(t('next.video.shell.renderFailed'), { tone: 'error', description: failedText(failure) }),
      )
      .finally(() => setStarting(false));
  };

  return (
    <div
      className="hx-deck-pop hx-video-render"
      data-closing={state === PRESENCE.closing ? '1' : undefined}
      role="dialog"
      aria-label={t('next.video.shell.renderLabel')}
      aria-busy={running !== null || undefined}
    >
      {reason !== null && <p className="hx-video-render__note">{reason}</p>}
      {video.settings.formats.map((format) => {
        const info = renderOf(video, format);
        const badge = renderBadge(info);
        const mine = running !== null && running.format === format ? running : null;
        return (
          <section key={format} className="hx-video-render__row" data-format={format}>
            <div className="hx-video-render__head">
              <b>{format}</b>
              {badge !== RENDER_BADGE.none && (
                <span className="hx-video-render__badge" data-badge={badge}>
                  {t(BADGE_KEY[badge])}
                </span>
              )}
            </div>
            {mine !== null && (
              <div className="hx-video-render__job">
                <progress max={1} value={mine.progress} aria-label={t('next.video.shell.progress')} />
                <span>{mine.draft ? t('next.video.shell.draftRunning') : t('next.video.shell.running')}</span>
              </div>
            )}
            <div className="hx-video-render__acts">
              <button
                type="button"
                className="hx-btn"
                data-variant="primary"
                data-size="sm"
                disabled={!idle}
                data-busy={starting || undefined}
                onClick={() => start(format, false)}
              >
                {t('next.video.shell.render')}
              </button>
              <button
                type="button"
                className="hx-btn"
                data-variant="ghost"
                data-size="sm"
                disabled={!idle}
                onClick={() => start(format, true)}
              >
                {t('next.video.shell.draft')}
              </button>
              {info !== null && info.published && (
                <a
                  className="hx-btn"
                  data-variant="ghost"
                  data-size="sm"
                  href={videoFileHref(machine, id, format)}
                  download
                >
                  <Icon name="download" size="sm" />
                  {t('next.video.shell.download')}
                </a>
              )}
            </div>
          </section>
        );
      })}
    </div>
  );
};

export default VideoRender;
