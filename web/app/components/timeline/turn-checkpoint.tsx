import { memo, useEffect, useMemo, useState, useSyncExternalStore } from 'react';
import dayjs from 'dayjs';
import dynamic from 'next/dynamic';
import { History, Undo2 } from 'lucide-react';
import {
  getCheckpointsState,
  refreshCheckpoints,
  stopWatchingCheckpoint,
  subscribeCheckpoints,
  watchForCheckpoint,
} from '@/lib/checkpoint-client';
import {
  checkpointForTurn,
  rewindNotices,
  type IRewindNotice,
  type TCheckpointView,
} from '@/lib/hl-checkpoint-match';
import { plural, t } from '@/lib/i18n';

const CheckpointRewindDialog = dynamic(() => import('@/components/timeline/checkpoint-rewind-dialog'), {
  ssr: false,
});

/** A turn older than this has either got its snapshot already or never will; it is not worth asking again. */
const WATCH_WINDOW_MS = 60_000;

interface ITarget {
  checkpoint: TCheckpointView;
  mode: 'rewind' | 'undo';
}

const Notice = ({ notice, onUndo }: { notice: IRewindNotice; onUndo: () => void }) => {
  const time = dayjs(notice.record.at).format('HH:mm');
  const { changed, skipped } = notice.record;
  return (
    <div className="tw-checkpoint-notice" role="status" data-undone={notice.undone || undefined}>
      <History size={13} aria-hidden="true" />
      <span>
        {notice.undone
          ? t('timeline.turnCheckpoint.undoneAt', { time })
          : t('timeline.turnCheckpoint.restoredAt', { time })}
        {!notice.undone && ` · ${plural(changed, 'timeline.turnCheckpoint.files')}`}
        {!notice.undone && skipped > 0 && ` · ${t('timeline.turnCheckpoint.skipped', { count: skipped })}`}
      </span>
      {notice.canUndo && (
        <button type="button" onClick={onUndo}>
          {t('timeline.turnCheckpoint.undo')}
        </button>
      )}
    </div>
  );
};

interface ITurnCheckpointProps {
  tabId?: string;
  sessionName?: string;
  endedAt: number;
  /** The turn is over: its snapshot exists, or is on its way. */
  settled: boolean;
}

/** The end of a finished turn: «Rewind to this point» and the messages about rewinds made to it. */
const TurnCheckpoint = memo(({ tabId, sessionName, endedAt, settled }: ITurnCheckpointProps) => {
  const state = useSyncExternalStore(
    subscribeCheckpoints,
    () => getCheckpointsState(tabId ?? ''),
    () => getCheckpointsState(tabId ?? ''),
  );
  const checkpoint = useMemo(
    () => checkpointForTurn(endedAt, state.checkpoints),
    [endedAt, state.checkpoints],
  );
  const notices = useMemo(
    () => rewindNotices(checkpoint, state.rewinds, state.checkpoints),
    [checkpoint, state],
  );
  const [target, setTarget] = useState<ITarget | null>(null);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [opened, setOpened] = useState(0);

  const hasCheckpoint = checkpoint !== null;
  useEffect(() => {
    if (!tabId || !settled) return;
    void refreshCheckpoints(tabId);
    if (hasCheckpoint || Date.now() - endedAt >= WATCH_WINDOW_MS) return;
    watchForCheckpoint(tabId);
    return () => stopWatchingCheckpoint(tabId);
  }, [tabId, settled, hasCheckpoint, endedAt]);

  if (!tabId || !settled || !checkpoint) return null;
  const open = (next: ITarget) => {
    setTarget(next);
    setOpened((count) => count + 1);
    setDialogOpen(true);
  };
  return (
    <div className="tw-checkpoint">
      <button
        type="button"
        className="tw-checkpoint-button"
        onClick={() => open({ checkpoint, mode: 'rewind' })}
      >
        <Undo2 size={13} aria-hidden="true" />
        {t('timeline.turnCheckpoint.rewindHere')}
      </button>
      {notices.map((notice) => (
        <Notice
          key={notice.record.at}
          notice={notice}
          onUndo={() => {
            const before = state.checkpoints.find((item) => item.id === notice.record.beforeId);
            if (before) open({ checkpoint: before, mode: 'undo' });
          }}
        />
      ))}
      {target && (
        <CheckpointRewindDialog
          key={opened}
          tabId={tabId}
          sessionName={sessionName}
          checkpoint={target.checkpoint}
          mode={target.mode}
          open={dialogOpen}
          onOpenChange={setDialogOpen}
        />
      )}
    </div>
  );
});
TurnCheckpoint.displayName = 'TurnCheckpoint';

export default TurnCheckpoint;
