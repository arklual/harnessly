// «Версии» of a project (HRN-903): the points of its history (each finished edit, by a person or an agent) and the
// versions the person named. A version opens read only (with the kit it was on), is compared with the project as it
// is (the boards that changed, the page side by side) and is restored; what is there now is saved first, so a restore
// is undone the same way. Shown in looking and in editing; saving and restoring need the right to edit.
import { useEffect, useState } from 'react';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';
import { plural, t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import {
  fetchProjectDiff,
  fetchProjectHistory,
  restoreProjectVersion,
  saveProjectVersion,
  versionPageHref,
  type IProjectDiff,
  type IProjectHistory,
  type IProjectPoint,
} from '@/next/features/designly/versions/versions-api';
import '@/next/styles/versions.css';

/** The panel's exit, as long as its animation in sinopia.css. */
const EXIT_MS = 220;
const POLL_MS = 10000;

export interface IVersionsPanelProps {
  open: boolean;
  onClose: () => void;
  machine: MachineId;
  id: string;
  origin: string | null;
  /** The page on screen, opened in a version. */
  page: string | null;
  mayEdit: boolean;
  /** The project's files changed under the view (a restore): the page is to be shown again. */
  onRestored: () => void;
}

const timeOf = (at: number) =>
  new Date(at).toLocaleString(undefined, {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  });

const useHistory = (machine: MachineId, id: string, on: boolean) => {
  const [history, setHistory] = useState<IProjectHistory | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [tick, setTick] = useState(0);
  useEffect(() => {
    if (!on) return;
    const now = new AbortController();
    const read = () =>
      fetchProjectHistory(machine, id, now.signal).then(
        (h) => {
          setHistory(h);
          setError(null);
        },
        (failure: unknown) => {
          if (!now.signal.aborted)
            setError(failure instanceof Error ? failure.message : t('next.versions.failed'));
        },
      );
    void read();
    const timer = setInterval(() => {
      if (!document.hidden) void read();
    }, POLL_MS);
    return () => {
      now.abort();
      clearInterval(timer);
    };
  }, [machine, id, on, tick]);
  return { history, error, reload: () => setTick((n) => n + 1) };
};

interface ICompareProps {
  machine: MachineId;
  id: string;
  origin: string | null;
  point: IProjectPoint;
  page: string | null;
}

/** A version against now: the boards that changed, and one of them side by side. */
const Compare = ({ machine, id, origin, point, page }: ICompareProps) => {
  const [diff, setDiff] = useState<IProjectDiff | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [board, setBoard] = useState<string | null>(null);
  useEffect(() => {
    const now = new AbortController();
    fetchProjectDiff(machine, id, point.id, now.signal).then(
      (d) => {
        setDiff(d);
        setBoard(d.boards.find((b) => b.kind !== 'added')?.path ?? null);
      },
      (failure: unknown) => {
        if (!now.signal.aborted)
          setError(failure instanceof Error ? failure.message : t('next.versions.failed'));
      },
    );
    return () => now.abort();
  }, [machine, id, point.id]);
  if (error !== null) return <div className="hx-versions__error">{error}</div>;
  if (diff === null) return <div className="hx-versions__muted">{t('next.versions.comparing')}</div>;
  const kitMoved =
    diff.kitThen !== null &&
    diff.kitNow !== null &&
    (diff.kitThen.version !== diff.kitNow.version || diff.kitNow.draft);
  const shown = board ?? page;
  return (
    <div className="hx-versions__compare">
      {diff.boards.length === 0 && diff.other === 0 && (
        <div className="hx-versions__muted">{t('next.versions.same')}</div>
      )}
      {diff.boards.length > 0 && (
        <ul className="hx-versions__boards">
          {diff.boards.map((b) => (
            <li key={b.path}>
              <button
                type="button"
                className="hx-versions__board"
                aria-pressed={b.path === board}
                data-kind={b.kind}
                disabled={b.kind === 'added'}
                onClick={() => setBoard(b.path)}
              >
                <span>{b.board ?? b.path}</span>
                <span className="hx-versions__kind">{t(`next.versions.kind.${b.kind}`)}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {diff.other > 0 && (
        <div className="hx-versions__muted">{plural(diff.other, 'next.versions.otherFiles')}</div>
      )}
      {kitMoved && diff.kitThen && (
        <div className="hx-versions__muted">
          {t('next.versions.kitThen', { version: diff.kitThen.version })}
        </div>
      )}
      {origin !== null && shown !== null && (
        <div className="hx-versions__pair">
          <figure>
            <figcaption>{t('next.versions.then')}</figcaption>
            <iframe title={t('next.versions.then')} src={versionPageHref(origin, id, diff.revision, shown)} />
          </figure>
          <figure>
            <figcaption>{t('next.versions.now')}</figcaption>
            <iframe
              title={t('next.versions.now')}
              src={`${origin.replace(/\/+$/, '')}/a/${id}/p/${shown.split('/').map(encodeURIComponent).join('/')}`}
            />
          </figure>
        </div>
      )}
    </div>
  );
};

const VersionsPanel = ({
  open,
  onClose,
  machine,
  id,
  origin,
  page,
  mayEdit,
  onRestored,
}: IVersionsPanelProps) => {
  const presence = usePresence(open ? id : null, EXIT_MS);
  const { history, error, reload } = useHistory(machine, id, open);
  const [name, setName] = useState('');
  const [saving, setSaving] = useState(false);
  const [restoring, setRestoring] = useState<string | null>(null);
  const [comparing, setComparing] = useState<string | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  if (presence.value === null) return null;

  const save = async () => {
    setSaving(true);
    setFailure(null);
    try {
      await saveProjectVersion(machine, id, name);
      setName('');
      reload();
    } catch (e) {
      setFailure(e instanceof Error ? e.message : t('next.versions.failed'));
    } finally {
      setSaving(false);
    }
  };
  const restore = async (point: IProjectPoint) => {
    setRestoring(point.id);
    setFailure(null);
    try {
      await restoreProjectVersion(machine, id, point.id);
      reload();
      onRestored();
    } catch (e) {
      setFailure(e instanceof Error ? e.message : t('next.versions.failed'));
    } finally {
      setRestoring(null);
    }
  };
  const named = new Set(history?.named.map((n) => n.id) ?? []);
  const points = history?.points ?? [];

  return (
    <aside
      className="hx-sinopia__panel hx-versions"
      data-state={presence.state}
      aria-label={t('next.versions.title')}
    >
      <div className="hx-sinopia__head">
        <span className="hx-sinopia__title">{t('next.versions.title')}</span>
        <button type="button" className="hx-head-btn" aria-label={t('next.versions.close')} onClick={onClose}>
          <Icon name="x" />
        </button>
      </div>
      {mayEdit && (
        <form
          className="hx-versions__save"
          onSubmit={(e) => {
            e.preventDefault();
            if (name.trim() !== '') void save();
          }}
        >
          <input
            className="hx-versions__name"
            value={name}
            maxLength={80}
            placeholder={t('next.versions.nameHint')}
            aria-label={t('next.versions.nameHint')}
            onChange={(e) => setName(e.target.value)}
          />
          <Button size="sm" busy={saving} disabled={name.trim() === ''} onClick={() => void save()}>
            {t('next.versions.save')}
          </Button>
        </form>
      )}
      {(failure ?? error) !== null && <div className="hx-versions__error">{failure ?? error}</div>}
      <ol className="hx-versions__list">
        {points.map((p, index) => (
          <li key={p.id} className="hx-versions__point" data-named={named.has(p.id) || undefined}>
            <div className="hx-versions__line">
              <Icon name={p.agent ? 'sparkles' : 'user'} size="sm" />
              <span className="hx-versions__what">{p.name ?? p.subject}</span>
            </div>
            <div className="hx-versions__meta">
              {timeOf(p.at)} · {p.who}
            </div>
            {index > 0 || history?.unsaved ? (
              <div className="hx-versions__acts">
                {origin !== null && page !== null && (
                  <a
                    className="hx-versions__act"
                    href={versionPageHref(origin, id, p.id, page)}
                    target="_blank"
                    rel="noreferrer"
                  >
                    {t('next.versions.open')}
                  </a>
                )}
                <button
                  type="button"
                  className="hx-versions__act"
                  aria-expanded={comparing === p.id}
                  onClick={() => setComparing(comparing === p.id ? null : p.id)}
                >
                  {t('next.versions.compare')}
                </button>
                {mayEdit && (
                  <button
                    type="button"
                    className="hx-versions__act"
                    disabled={restoring !== null}
                    onClick={() => void restore(p)}
                  >
                    {restoring === p.id ? t('next.versions.restoring') : t('next.versions.restore')}
                  </button>
                )}
              </div>
            ) : (
              <div className="hx-versions__meta">{t('next.versions.current')}</div>
            )}
            {comparing === p.id && (
              <Compare machine={machine} id={id} origin={origin} point={p} page={page} />
            )}
          </li>
        ))}
      </ol>
      {history !== null && points.length === 0 && (
        <div className="hx-versions__muted">{t('next.versions.empty')}</div>
      )}
    </aside>
  );
};

export default VersionsPanel;
