import { useState } from 'react';
import dayjs from 'dayjs';
import { ScanSearch } from 'lucide-react';
import { BUTTON_SIZE, BUTTON_VARIANT, TwButton, TwSpinner, cx } from '@/components/agents/ui';
import useDesignCheck from '@/hooks/use-design-check';
import { currentMachineId, scopedAppPath } from '@/lib/app-path';
import type { ICheckBoard, ICheckReport, TCheckStatus } from '@/types/server/check';
import { BOARD_STATUS_LABEL } from '@/lib/hl-design-library/home-model';
import { libraryBoardAddress, type ILibraryArtifact } from '@/lib/hl-design-library/types';
import { t } from '@/lib/i18n';

const STATUS_RANK = { error: 0, blank: 1, warning: 2, ok: 3 } as const satisfies Record<TCheckStatus, number>;

const VIEW_LABEL = {
  desktop: t('pane.designCheckPanel.desktop'),
  phone: t('pane.designCheckPanel.phone'),
} as const;

const shotUrl = (report: ICheckReport, file: string) =>
  scopedAppPath(
    `/api/platform/design/library/shot?artifact=${encodeURIComponent(report.artifact)}&file=${encodeURIComponent(file)}&v=${report.startedAt}`,
    currentMachineId(),
  );

interface IBoardResultProps {
  report: ICheckReport;
  board: ICheckBoard;
  onOpen: () => void;
}

const BoardResult = ({ report, board, onOpen }: IBoardResultProps) => {
  const problems = [...new Set(board.shots.flatMap((shot) => [...shot.errors, ...shot.failedRequests]))];
  return (
    <li className="tw-dcheck-board" data-status={board.status}>
      <button
        type="button"
        className="tw-dcheck-shots"
        onClick={onOpen}
        aria-label={t('pane.designCheckPanel.open', { title: board.title })}
      >
        {board.shots.map((shot) => (
          <span key={shot.view} className="tw-dcheck-shot" data-view={shot.view}>
            {shot.screenshot ? (
              <img
                src={shotUrl(report, shot.screenshot)}
                alt={`${board.title}, ${VIEW_LABEL[shot.view]}`}
                loading="lazy"
              />
            ) : (
              <span>{t('pane.designCheckPanel.noShot')}</span>
            )}
          </span>
        ))}
      </button>
      <div className="tw-dcheck-text">
        <span className="tw-dcheck-title" title={board.title}>
          {board.title}
        </span>
        <span className="tw-dcheck-status">{BOARD_STATUS_LABEL[board.status]}</span>
        {problems.slice(0, 3).map((problem) => (
          <code key={problem} className="tw-dcheck-problem">
            {problem}
          </code>
        ))}
        {problems.length > 3 && (
          <span className="tw-dcheck-more">
            {t('pane.designCheckPanel.moreProblems', { n: problems.length - 3 })}
          </span>
        )}
      </div>
    </li>
  );
};

interface IDesignCheckPanelProps {
  artifact: ILibraryArtifact;
  port: number;
  onOpenBoard: (address: string, title: string) => void;
  /** Starts a check as soon as the panel opens: the card's «Check boards». */
  autoStart: boolean;
}

/**
 * The check of a design: every board opened in a headless Chrome of the machine as a desktop and as a
 * phone, with screenshots, console errors, failed requests and blank screens. Broken boards come first; a thumbnail
 * opens the board in the panel's browser.
 */
export const DesignCheckPanel = ({ artifact, port, onOpenBoard, autoStart }: IDesignCheckPanelProps) => {
  const { report, error, running, start } = useDesignCheck(artifact.id, autoStart);
  const [showAll, setShowAll] = useState(false);

  if (report === null)
    return (
      <div className="tw-dcheck" aria-busy="true">
        <TwSpinner />
      </div>
    );

  const boards = report
    ? [...report.boards].sort((a, b) => STATUS_RANK[a.status] - STATUS_RANK[b.status])
    : [];
  const broken = boards.filter((board) => board.status !== 'ok');
  const shown = showAll ? boards : broken;

  return (
    <section className="tw-dcheck" aria-label={t('pane.designCheckPanel.check')}>
      <header className="tw-dcheck-head">
        <TwButton
          size={BUTTON_SIZE.sm}
          variant={BUTTON_VARIANT.secondary}
          icon={running ? <TwSpinner /> : <ScanSearch />}
          disabled={running}
          onClick={() => void start()}
        >
          {running
            ? t('pane.designCheckPanel.checking', {
                done: report ? report.done : 0,
                total: report ? report.total : 0,
              })
            : t('pane.designCheckPanel.checkBoards')}
        </TwButton>
        {report && report.finishedAt !== null && (
          <span className="tw-dcheck-summary">
            {broken.length > 0
              ? t('pane.designCheckPanel.broken', { broken: broken.length, total: boards.length })
              : t('pane.designCheckPanel.allFine', { n: boards.length })}{' '}
            · {dayjs(report.finishedAt).format('DD.MM HH:mm')}
          </span>
        )}
        {!report && <span className="tw-dcheck-summary">{t('pane.designCheckPanel.explain')}</span>}
      </header>
      {running && report && (
        <progress className="tw-dcheck-progress" max={report.total} value={report.done} />
      )}
      {error && (
        <p className="tw-dlib-note tw-dlib-note-error" role="alert">
          {error}
        </p>
      )}
      {report && report.failure && (
        <p className="tw-dlib-note tw-dlib-note-error" role="alert">
          {report.failure}
        </p>
      )}
      {shown.length > 0 && (
        <ul className={cx('tw-dcheck-list')}>
          {shown.map((board) => (
            <BoardResult
              key={board.path}
              report={report as ICheckReport}
              board={board}
              onOpen={() => onOpenBoard(libraryBoardAddress(port, artifact.id, board.path), board.title)}
            />
          ))}
        </ul>
      )}
      {boards.length > broken.length && (
        <button type="button" className="tw-dcheck-toggle" onClick={() => setShowAll((all) => !all)}>
          {showAll
            ? t('pane.designCheckPanel.onlyBroken')
            : t('pane.designCheckPanel.showAll', { n: boards.length })}
        </button>
      )}
    </section>
  );
};
