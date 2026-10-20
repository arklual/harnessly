import { useMemo, useState } from 'react';
import { History } from 'lucide-react';
import {
  LAYER_ALIGN,
  TW_MENU_KIND,
  TwConfirm,
  TwMenuButton,
  TwTooltip,
  type TTwMenuEntry,
} from '@/components/agents/ui';
import useMinuteClock from '@/components/agents/sidebar/use-minute-clock';
import usePoller from '@/hooks/use-poller';
import { DesignRequestError, fetchBoardHistory, restoreBoardVersion } from '@/lib/hl-design-library-client';
import { relativeWhen } from '@/lib/hl-design-library/home-model';
import type { ILibraryProject } from '@/lib/hl-design-library/fleet';
import type { IBoardSnapshot, ILibraryBoard } from '@/lib/hl-design-library/types';
import { t } from '@/lib/i18n';

/** `null` while the first answer is on its way, `[]` for a board nobody has saved a version of yet. */
type TSnapshots = IBoardSnapshot[] | null;

const FAILED = 'failed' as const;

/** New versions appear on their own a little after each edit; the menu also asks again when it is reached for. */
const HISTORY_POLL = { intervalMs: 20_000, maxBackoffMs: 60_000 } as const;

interface IBoardHistoryProps {
  project: ILibraryProject;
  board: ILibraryBoard;
  /** The board is back as it was: the page shows it again. */
  onRestored: () => void;
}

/**
 * "History" of the board page: the latest saved versions of the board file and the way back to one of them.
 * Pessimistic: the board is reloaded only after the runtime has put the old version in place, and the version it had
 * stays in the history. Keyed by the board: another board starts from nothing.
 */
export const BoardHistory = ({ project, board, onRestored }: IBoardHistoryProps) => {
  const [snapshots, setSnapshots] = useState<TSnapshots | typeof FAILED>(null);
  const [chosen, setChosen] = useState<IBoardSnapshot | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const now = useMinuteClock();
  const machine = project.machineId;

  const { refresh, poke } = usePoller(
    `design-history:${machine ?? ''}:${project.id}:${board.path}`,
    async (signal) => {
      try {
        const { snapshots: found } = await fetchBoardHistory(machine, project.id, board.path, signal);
        if (!signal.aborted) setSnapshots(found);
      } catch (error) {
        if (signal.aborted) return;
        setSnapshots((known) => (Array.isArray(known) ? known : FAILED));
        throw error;
      }
    },
    HISTORY_POLL,
  );

  const items = useMemo<TTwMenuEntry[]>(() => {
    if (snapshots === null)
      return [
        {
          kind: TW_MENU_KIND.action,
          id: 'loading',
          label: t('design.boardHistory.loading'),
          disabled: true,
          onSelect: () => undefined,
        },
      ];
    if (snapshots === FAILED)
      return [
        {
          kind: TW_MENU_KIND.action,
          id: 'failed',
          label: t('design.boardHistory.unavailable'),
          disabled: true,
          onSelect: () => undefined,
        },
      ];
    if (snapshots.length === 0)
      return [
        {
          kind: TW_MENU_KIND.action,
          id: 'empty',
          label: t('design.boardHistory.noVersions'),
          hint: t('design.boardHistory.noVersionsHint'),
          disabled: true,
          onSelect: () => undefined,
        },
      ];
    return [
      { kind: TW_MENU_KIND.label, id: 'title', label: t('design.boardHistory.restore') },
      ...snapshots.map((snapshot): TTwMenuEntry => ({
        kind: TW_MENU_KIND.action,
        id: snapshot.id,
        label: snapshot.current
          ? t('design.boardHistory.currentVersion', { when: relativeWhen(snapshot.at, now) })
          : relativeWhen(snapshot.at, now),
        hint: snapshot.who,
        disabled: snapshot.current,
        onSelect: () => {
          setError(null);
          setChosen(snapshot);
        },
      })),
    ];
  }, [snapshots, now]);

  const restore = async () => {
    if (!chosen) return;
    setPending(true);
    setError(null);
    try {
      await restoreBoardVersion(machine, project.id, board.path, chosen.id);
      setChosen(null);
      onRestored();
      void refresh();
    } catch (failure) {
      setError(
        failure instanceof DesignRequestError ? failure.message : t('design.boardHistory.restoreFailed'),
      );
    } finally {
      setPending(false);
    }
  };

  return (
    <>
      <TwTooltip content={t('design.boardHistory.title')}>
        <TwMenuButton
          items={items}
          menuLabel={t('design.boardHistory.title')}
          align={LAYER_ALIGN.end}
          className="tw-icon-button"
          aria-label={t('design.boardHistory.title')}
          onPointerDown={() => poke()}
          onFocus={() => poke()}
        >
          <History />
        </TwMenuButton>
      </TwTooltip>
      <TwConfirm
        open={chosen !== null}
        title={t('design.boardHistory.restoreConfirm')}
        description={
          chosen
            ? t('design.boardHistory.restoreNote', { when: relativeWhen(chosen.at, now), who: chosen.who })
            : undefined
        }
        confirmLabel={t('design.boardHistory.restore')}
        pending={pending}
        error={error}
        onConfirm={() => void restore()}
        onCancel={() => {
          if (!pending) setChosen(null);
        }}
      />
    </>
  );
};
