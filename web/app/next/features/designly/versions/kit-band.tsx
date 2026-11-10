// The project's kit over its view (HRN-903): a project on the kit's draft says so («Показан черновик кита») and goes
// back to its version; a project on an older version gets a quiet «Есть версия X» with what changed, a look at its
// pages on that version, «Обновить» and «Остаться»; a project that is the kit's draft publishes it («Опубликовать
// версию») or puts the kit back. «Править в ките» switches the project onto the draft. Nothing is updated without
// the person.
import { useEffect, useState } from 'react';
import Popover from '@/next/ui/popover';
import Segmented from '@/next/ui/segmented';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { plural, t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import {
  changeProjectKit,
  discardKitDraft,
  fetchKitState,
  fetchProjectKit,
  KIT_ACTION,
  KIT_PART,
  kitPreviewHref,
  nextVersion,
  publishKitDraft,
  type IKitState,
  type IProjectKit,
  type TKitAction,
  type TKitPart,
} from '@/next/features/designly/versions/versions-api';
import '@/next/styles/versions.css';

/** How often the kit's state is read again while the project is on screen, ms. */
const POLL_MS = 15000;
const SHOWN_CHANGES = 8;

export interface IKitBandProps {
  machine: MachineId;
  id: string;
  /** The project's kit, read by useProjectKit. */
  info: IProjectKitInfo;
  /** The library's origin the pages are shown from; null while there is none. */
  origin: string | null;
  /** The page on screen. */
  page: string | null;
  mayEdit: boolean;
  /** Opens another project of the library (the kit's draft). */
  onOpenProject: (id: string) => void;
  /** Opens the kit's screen in its editing mode (the draft with its chat). */
  onOpenKit?: (kit: string) => void;
  /** The kit changed what the project shows: the page is to be shown again. */
  onKitChanged: () => void;
}

export interface IProjectKitInfo {
  kit: IProjectKit | null;
  state: IKitState | null;
  reload: () => void;
  setKit: (kit: IProjectKit) => void;
}

/** Whether the band has anything to show for the project: it is on a kit or is a kit's draft. */
export const hasKitBand = (info: IProjectKitInfo): boolean =>
  info.kit !== null && (info.kit.pin !== null || (info.kit.kitDraftOf !== undefined && info.state !== null));

/** The project's kit and, for the kit's draft, the kit's state; read again while on screen. */
export const useProjectKit = (machine: MachineId, id: string): IProjectKitInfo => {
  const [kit, setKit] = useState<IProjectKit | null>(null);
  const [state, setState] = useState<IKitState | null>(null);
  const [tick, setTick] = useState(0);
  useEffect(() => {
    const now = new AbortController();
    const read = async () => {
      try {
        const next = await fetchProjectKit(machine, id, now.signal);
        setKit(next);
        const kitId = next.kitDraftOf ?? next.pin?.id ?? '';
        setState(
          kitId === ''
            ? null
            : await fetchKitState(machine, kitId, now.signal, next.kitDraftOf !== undefined),
        );
      } catch {
        // A machine that does not know kit versions shows no band.
      }
    };
    void read();
    const timer = setInterval(() => {
      if (!document.hidden) void read();
    }, POLL_MS);
    return () => {
      now.abort();
      clearInterval(timer);
    };
  }, [machine, id, tick]);
  return { kit, state, reload: () => setTick((n) => n + 1), setKit };
};

interface IPublishProps {
  machine: MachineId;
  state: IKitState;
  onDone: () => void;
}

/** How long a publish whose answer did not come is looked for, and how often, ms. */
const PUBLISH_WAIT_MS = 180_000;
const PUBLISH_LOOK_MS = 4000;

/** Whether the kit has a newer version than `was` within the wait (a publish that outlasted its answer). */
const versionCame = async (machine: MachineId, kit: string, was: string): Promise<boolean> => {
  for (let waited = 0; waited < PUBLISH_WAIT_MS; waited += PUBLISH_LOOK_MS) {
    await new Promise((done) => setTimeout(done, PUBLISH_LOOK_MS));
    const st = await fetchKitState(machine, kit, undefined, false).catch(() => null);
    if (st && st.latest !== was) return true;
    if (st && st.draft === null) return false;
  }
  return false;
};

export const PublishPop = ({ machine, state, onDone }: IPublishProps) => {
  const [open, setOpen] = useState(false);
  const [part, setPart] = useState<TKitPart>(KIT_PART.minor);
  const [notes, setNotes] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const draft = state.draft;
  const count = draft?.changedCount ?? 0;
  const publish = async () => {
    setBusy(true);
    setError(null);
    try {
      await publishKitDraft(machine, state.id, part, notes);
      setOpen(false);
      setNotes('');
      onDone();
    } catch (failure) {
      // A large kit is copied longer than an answer is waited for: the version is there once the copy ends.
      if (await versionCame(machine, state.id, state.latest)) {
        setOpen(false);
        setNotes('');
        onDone();
        return;
      }
      setError(failure instanceof Error ? failure.message : t('next.versions.failed'));
    } finally {
      setBusy(false);
    }
  };
  const parts = [KIT_PART.patch, KIT_PART.minor, KIT_PART.major].map((p) => ({
    value: p,
    label: nextVersion(state.latest, p),
  }));
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      label={t('next.versions.publish')}
      className="hx-kitpop"
      side="bottom"
      align="end"
      trigger={
        <Button size="sm" variant="primary" disabled={count === 0}>
          {t('next.versions.publish')}
        </Button>
      }
    >
      <div className="hx-kitpop__body">
        <div className="hx-kitpop__title">{t('next.versions.publishTitle', { kit: state.id })}</div>
        <Segmented label={t('next.versions.number')} value={part} options={parts} onChange={setPart} />
        <textarea
          className="hx-kitpop__notes"
          rows={3}
          maxLength={500}
          placeholder={t('next.versions.notesHint')}
          value={notes}
          onChange={(e) => setNotes(e.target.value)}
        />
        {draft && (
          <ul className="hx-kitpop__changes">
            {(draft.changes ?? []).slice(0, SHOWN_CHANGES).map((c) => (
              <li key={c.path} data-kind={c.kind}>
                {c.path}
              </li>
            ))}
            {count > SHOWN_CHANGES && <li>{plural(count - SHOWN_CHANGES, 'next.versions.more')}</li>}
          </ul>
        )}
        {error !== null && <div className="hx-kitpop__error">{error}</div>}
        <div className="hx-kitpop__actions">
          <Button size="sm" variant="primary" busy={busy} onClick={() => void publish()}>
            {t('next.versions.publishAs', { version: nextVersion(state.latest, part) })}
          </Button>
        </div>
      </div>
    </Popover>
  );
};

export const DiscardPop = ({ machine, state, onDone }: IPublishProps) => {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const discard = async () => {
    setBusy(true);
    setError(null);
    try {
      await discardKitDraft(machine, state.id);
      setOpen(false);
      onDone();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : t('next.versions.failed'));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      label={t('next.versions.discard')}
      className="hx-kitpop"
      side="bottom"
      align="end"
      trigger={
        <Button size="sm" variant="ghost">
          {t('next.versions.discard')}
        </Button>
      }
    >
      <div className="hx-kitpop__body">
        <div className="hx-kitpop__text">{t('next.versions.discardText', { version: state.latest })}</div>
        {error !== null && <div className="hx-kitpop__error">{error}</div>}
        <div className="hx-kitpop__actions">
          <Button size="sm" variant="danger" busy={busy} onClick={() => void discard()}>
            {t('next.versions.discard')}
          </Button>
        </div>
      </div>
    </Popover>
  );
};

const KitBand = ({
  machine,
  id,
  info,
  origin,
  page,
  mayEdit,
  onOpenProject,
  onOpenKit,
  onKitChanged,
}: IKitBandProps) => {
  const { kit, state, reload, setKit } = info;
  // A kit for viewing only (its maker set it) is not edited from here: no «Править в ките».
  const mayEditKit = mayEdit && state?.mayEdit !== false;
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState<TKitAction | null>(null);
  const [error, setError] = useState<string | null>(null);
  if (kit === null) return null;
  const act = async (action: TKitAction, version?: string) => {
    setBusy(action);
    setError(null);
    try {
      const next = await changeProjectKit(machine, id, action, version);
      setKit(next);
      setOpen(false);
      if (action === KIT_ACTION.edit && next.pin && onOpenKit) onOpenKit(next.pin.id);
      else if (action === KIT_ACTION.edit && next.draftProject) onOpenProject(next.draftProject);
      if (action !== KIT_ACTION.stay) onKitChanged();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : t('next.versions.failed'));
    } finally {
      setBusy(null);
    }
  };

  // The kit's draft itself: publish it or put the kit back.
  if (kit.kitDraftOf && state !== null) {
    const count = state.draft?.changedCount ?? 0;
    return (
      <div className="hx-kitband" data-tone="draft" role="status">
        <Icon name="pencil" size="sm" />
        <span className="hx-kitband__text">
          {t('next.versions.draftOf', { kit: state.id, version: state.draft?.base ?? state.latest })}
          {count > 0 && <span className="hx-kitband__muted">{plural(count, 'next.versions.edits')}</span>}
        </span>
        {mayEditKit && (
          <span className="hx-kitband__actions">
            <DiscardPop machine={machine} state={state} onDone={reload} />
            <PublishPop machine={machine} state={state} onDone={reload} />
          </span>
        )}
      </div>
    );
  }
  const pin = kit.pin;
  if (!pin) return null;

  // On the draft for now.
  if (pin.draft) {
    return (
      <div className="hx-kitband" data-tone="draft" role="status">
        <Icon name="pencil" size="sm" />
        <span className="hx-kitband__text">{t('next.versions.onDraft', { kit: pin.id })}</span>
        <span className="hx-kitband__actions">
          {kit.draftProject && (
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                if (onOpenKit) onOpenKit(pin.id);
                else if (kit.draftProject) onOpenProject(kit.draftProject);
              }}
            >
              {t('next.versions.openDraft')}
            </Button>
          )}
          {mayEdit && (
            <Button
              size="sm"
              busy={busy === KIT_ACTION.use}
              onClick={() => void act(KIT_ACTION.use, pin.version)}
            >
              {t('next.versions.backTo', { version: pin.version })}
            </Button>
          )}
        </span>
        {error !== null && <span className="hx-kitband__error">{error}</span>}
      </div>
    );
  }

  const update = kit.update;
  if (!mayEdit && update === null) return null;
  return (
    <div className="hx-kitband" data-tone="quiet">
      <span className="hx-kitband__actions">
        {update !== null && (
          <Popover
            open={open}
            onOpenChange={setOpen}
            label={t('next.versions.hasVersion', { version: update.version })}
            className="hx-kitpop"
            side="bottom"
            align="end"
            trigger={
              <button type="button" className="hx-kitband__chip">
                {t('next.versions.hasVersion', { version: update.version })}
              </button>
            }
          >
            <div className="hx-kitpop__body">
              <div className="hx-kitpop__title">
                {t('next.versions.hasVersion', { version: update.version })}
              </div>
              {update.notes && <div className="hx-kitpop__text">{update.notes}</div>}
              {(update.changedCount ?? 0) > 0 && (
                <div className="hx-kitpop__muted">
                  {plural(update.changedCount ?? 0, 'next.versions.files')}
                </div>
              )}
              {origin !== null && page !== null && (
                <a
                  className="hx-kitpop__link"
                  href={kitPreviewHref(origin, id, update.version, page)}
                  target="_blank"
                  rel="noreferrer"
                >
                  <Icon name="eye" size="sm" />
                  {t('next.versions.preview')}
                </a>
              )}
              {error !== null && <div className="hx-kitpop__error">{error}</div>}
              {mayEdit && (
                <div className="hx-kitpop__actions">
                  <Button
                    size="sm"
                    variant="ghost"
                    busy={busy === KIT_ACTION.stay}
                    onClick={() => void act(KIT_ACTION.stay, update.version)}
                  >
                    {t('next.versions.stay')}
                  </Button>
                  <Button
                    size="sm"
                    variant="primary"
                    busy={busy === KIT_ACTION.use}
                    onClick={() => void act(KIT_ACTION.use, update.version)}
                  >
                    {t('next.versions.update')}
                  </Button>
                </div>
              )}
            </div>
          </Popover>
        )}
        {mayEditKit && (
          <Button
            size="sm"
            variant="ghost"
            icon="pencil"
            busy={busy === KIT_ACTION.edit}
            onClick={() => void act(KIT_ACTION.edit)}
          >
            {t('next.versions.editInKit')}
          </Button>
        )}
      </span>
      {error !== null && !open && <span className="hx-kitband__error">{error}</span>}
    </div>
  );
};

export default KitBand;
