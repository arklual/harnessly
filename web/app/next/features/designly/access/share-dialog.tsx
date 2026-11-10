// «Поделиться» of Designly, the Google Docs / Figma way: simple by default, the rest on demand. A person who
// manages sees one field to add people (the role and its term at the field's end), who has access with each one's role
// menu, the requests when there are any, the team's «Общий доступ», and «Копировать ссылку» (the design's own address,
// it gives nobody access) / «Готово» at the foot.
// What is needed rarely or is dangerous sits in «…» of the head; the journal, the link settings and the hand-over are
// views that slide in inside the dialog with a way back. One who may only look sees their own role and a way to ask
// for more. Every change waits for the gateway and shows what it answered. On the phone it is a bottom sheet.
import { memo, useCallback, useEffect, useRef, useState, type ReactNode, type RefObject } from 'react';
import AccessAdd from '@/next/features/designly/access/access-add';
import AccessGeneral from '@/next/features/designly/access/access-general';
import AccessLinks from '@/next/features/designly/access/access-links';
import AccessLog, { warmLog } from '@/next/features/designly/access/access-log';
import AccessPeople from '@/next/features/designly/access/access-people';
import AccessRequests from '@/next/features/designly/access/access-requests';
import AccessTransfer from '@/next/features/designly/access/access-transfer';
import {
  askAccess,
  atLeast,
  isUnowned,
  type IProjectAccess,
  type TGuestRights,
} from '@/next/features/designly/access/api';
import { roleLabel } from '@/next/features/designly/access/labels';
import type { IScopePage } from '@/next/features/designly/access/link-scope';
import RequestForm from '@/next/features/designly/access/request-form';
import ShareMenu, { SUB_VIEW, type TSubView } from '@/next/features/designly/access/share-menu';
import { useAction, type IAction } from '@/next/features/designly/access/use-action';
import { Modal } from '@/next/features/designly/sdk/ui';
import { useMe } from '@/next/stores/me';
import Icon from '@/next/ui/icon';
import { usePresence, type TPresenceState } from '@/next/ui/presence';
import Sheet from '@/next/ui/sheet';
import Tooltip from '@/next/ui/tooltip';
import { usePhone } from '@/next/ui/use-media';
import type { TAccessFocus } from '@/next/routes/routes';
import { t } from '@/next/i18n';
import '@/next/styles/design-access.css';

export interface IShareDialogProps {
  open: boolean;
  onClose: () => void;
  project: string;
  /** The design's name, in the title. */
  title: string;
  access: IProjectAccess;
  /** `?access=` of the address: the block to bring into view. */
  focus?: TAccessFocus | null;
  onNext: (next: IProjectAccess) => void;
  /** Copies the design's own address, like Google Docs: it opens for whoever already has access (the people, the
   *  team per «Общий доступ»), anyone else lands on «запросить доступ». Invite links are made only in «Настройки
   *  ссылок». */
  onCopyLink: () => void;
  /** The runtime takes the project for the person; resolves when the gateway knows the owner. */
  onClaim: () => Promise<void>;
  /** The design's pages, for limiting a link without sign-in to some of them. */
  pages?: readonly IScopePage[];
  /** What the foot carries after the link buttons: a document made of a file puts its «Отвязать» here. */
  footExtra?: ReactNode;
  /** What a link without sign-in may allow; empty where there are no such links. Both by default. */
  guestRights?: readonly TGuestRights[];
  /** A block after «Общий доступ» for whoever manages: a document's old public link, while it is open. */
  extra?: ReactNode;
}

const ALL_RIGHTS: readonly TGuestRights[] = ['view', 'comment'];

/** A view leaves in this long (styles/design-access.css, .hx-acc-view--sub[data-state='closing']). */
const VIEW_OUT_MS = 220;

const VIEW_TITLE: Record<TSubView, string> = {
  log: 'next.access.history',
  links: 'next.access.linksTitle',
  transfer: 'next.access.transferTitle',
};

export interface IFrameProps {
  open: boolean;
  /** HRN-675: the one dialog of every kind — at most 600px tall on a wide window; on the phone the head stays and
   *  only the body scrolls. */
  fixedHead?: boolean;
  /** The first focus on a wide window (the add field); the phone's sheet keeps it on itself, no keyboard pops up. */
  initialFocus?: RefObject<HTMLElement | null>;
  onClose: () => void;
  title: string;
  actions: ReactNode;
  children: ReactNode;
}

/** The dialog on a wide window, the bottom sheet on the phone; the same head, body and foot in both. */
export const Frame = ({
  open,
  fixedHead = false,
  initialFocus,
  onClose,
  title,
  actions,
  children,
}: IFrameProps) => {
  const phone = usePhone();
  if (!phone)
    return (
      <Modal
        open={open}
        onClose={onClose}
        title={title}
        boxClass={fixedHead ? 'hx-acc hx-acc--one' : 'hx-acc'}
        actions={actions}
        initialFocus={initialFocus}
      >
        {children}
      </Modal>
    );
  return (
    <Sheet
      open={open}
      onClose={onClose}
      label={title}
      className={fixedHead ? 'hx-acc-sheet hx-acc-sheet--one' : 'hx-acc-sheet'}
    >
      <div className="hx-modal__head">
        <div>
          <h2 className="hx-modal__title">{title}</h2>
        </div>
        {actions}
        <button type="button" className="hx-head-btn" aria-label={t('next.modal.close')} onClick={onClose}>
          <Icon name="x" />
        </button>
      </div>
      {children}
    </Sheet>
  );
};

/** A view kept in the dialog while it is hidden: it was opened once since the dialog opened (HRN-675). */
export const IDLE = 'idle';

/** The main view fades out in this long before it is skipped (styles/design-access.css, .hx-acc-view--main). */
const MAIN_OUT_MS = 280;

/** True once a view has covered the main one for its whole fade, false at once when the view goes. The covered main
 *  view is skipped by the browser (content-visibility) instead of being made inert: inert restyled every row of it on
 *  each switch, the skip keeps what was computed (HRN-675). */
export const useCovered = (on: boolean): boolean => {
  const [covered, setCovered] = useState(false);
  if (!on && covered) setCovered(false);
  useEffect(() => {
    if (!on) return;
    const timer = setTimeout(() => setCovered(true), MAIN_OUT_MS);
    return () => clearTimeout(timer);
  }, [on]);
  return covered;
};

/** Holds what a view last drew while it is out of sight or on its way out: the dialog drawing again for the view that
 *  shows does not draw the hidden ones (HRN-675). */
export const Still = memo(
  ({ children }: { still: boolean; children: ReactNode }) => children,
  (before, after) => before.still && after.still,
);

interface IMainProps {
  project: string;
  access: IProjectAccess;
  action: IAction;
  onNext: (next: IProjectAccess) => void;
  me: string | null;
  field: RefObject<HTMLInputElement | null>;
  extra?: ReactNode;
}

/** The main view, drawn again only when what it shows changes, not when a view slides over it (HRN-675). */
const Main = memo(({ project, access, action, onNext, me, field, extra }: IMainProps) => {
  const section = { project, access, action, onNext };
  return (
    <>
      <AccessAdd {...section} inputRef={field} />
      <AccessRequests {...section} />
      <AccessPeople {...section} me={me} />
      <AccessGeneral {...section} />
      {extra}
    </>
  );
});

const ShareDialog = ({
  open,
  onClose,
  project,
  title,
  access,
  focus = null,
  onNext,
  onCopyLink,
  onClaim,
  pages,
  footExtra,
  guestRights = ALL_RIGHTS,
  extra,
}: IShareDialogProps) => {
  const action = useAction();
  const me = useMe();
  const body = useRef<HTMLDivElement>(null);
  const field = useRef<HTMLInputElement>(null);
  const [view, setView] = useState<TSubView | null>(null);
  const sub = usePresence(view, VIEW_OUT_MS);
  // The views opened since the dialog opened stay mounted while hidden: nothing is drawn or read again on the way back.
  const [seen, setSeen] = useState<readonly TSubView[]>([]);
  if (view !== null && !seen.includes(view)) setSeen([...seen, view]);
  const idle = action.busy === null;
  const section = { project, access, action, onNext };
  const manages = access.canManage;
  const links = guestRights.length > 0;

  // The dialog opens again on its main view (adjusted while rendering; a closing dialog keeps its view as it leaves).
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) {
      setView(null);
      setSeen([]);
    }
  }

  useEffect(() => (open ? warmLog() : undefined), [open]);

  // A push says which block it is about: the dialog opens with that one in view.
  useEffect(() => {
    if (!open || !focus) return;
    const timer = setTimeout(() => {
      body.current?.querySelector(`[data-section="${focus}"]`)?.scrollIntoView?.({ block: 'nearest' });
    }, 50);
    return () => clearTimeout(timer);
  }, [open, focus, access.canManage]);

  const { clear } = action;
  const go = useCallback(
    (next: TSubView | null) => {
      clear();
      setView(next);
    },
    [clear],
  );
  const covered = useCovered(view !== null);
  const stateOf = (v: TSubView): TPresenceState | typeof IDLE => (v === sub.value ? sub.state : IDLE);

  const shareTitle = t('next.access.shareTitle', { name: title || t('next.access.untitled') });

  return (
    <Frame
      open={open}
      onClose={onClose}
      initialFocus={manages ? field : undefined}
      title={shareTitle}
      actions={manages && <ShareMenu {...section} links={links} onView={go} />}
    >
      <div className="hx-modal__body hx-acc-body" ref={body}>
        {manages ? (
          <div className="hx-acc-views" data-sub={view !== null} data-covered={covered}>
            <div className="hx-acc-view hx-acc-view--main" aria-hidden={view !== null || undefined}>
              <Main
                project={project}
                access={access}
                action={action}
                onNext={onNext}
                me={me.k === 'ready' ? me.me.email : null}
                field={field}
                extra={extra}
              />
            </div>
            {seen.map((v) => (
              <div
                key={v}
                className="hx-acc-view hx-acc-view--sub"
                data-state={stateOf(v)}
                aria-hidden={view !== v || undefined}
              >
                <Still still={view !== v}>
                  <div className="hx-acc-back">
                    <button
                      type="button"
                      className="hx-head-btn"
                      aria-label={t('next.access.backView')}
                      onClick={() => go(null)}
                    >
                      <Icon name="arrow-left" />
                    </button>
                    <h3 className="hx-acc-back__title">{t(VIEW_TITLE[v])}</h3>
                  </div>
                  {v === SUB_VIEW.log && <AccessLog project={project} />}
                  {v === SUB_VIEW.links && (
                    <AccessLinks {...section} pages={pages} guestRights={guestRights} />
                  )}
                  {v === SUB_VIEW.transfer && <AccessTransfer {...section} onDone={() => go(null)} />}
                </Still>
              </div>
            ))}
          </div>
        ) : (
          <section className="hx-acc-sec" data-section="own">
            {access.role && (
              <p className="hx-acc-note">{t('next.access.yourRole', { role: roleLabel(access.role) })}</p>
            )}
            {(access.role === 'view' || access.role === 'comment') && (
              <RequestForm
                mayComment={access.role === 'view'}
                status={access.request?.status ?? null}
                onSend={async (level, message) => onNext(await askAccess(project, level, message))}
              />
            )}
            {isUnowned(access) && atLeast(access.role, 'edit') && (
              <div className="hx-acc-line">
                <button
                  type="button"
                  className="hx-btn"
                  data-busy={action.busy === 'claim'}
                  disabled={!idle}
                  onClick={() => void action.run('claim', onClaim)}
                >
                  <span>{t('next.access.claim')}</span>
                  <span className="hx-gate__spin" aria-hidden="true" />
                </button>
              </div>
            )}
          </section>
        )}
        {action.error && (
          <p className="hx-create__err" role="alert">
            {action.error}
          </p>
        )}
      </div>
      <div className="hx-modal__foot hx-acc-foot">
        <div className="hx-acc-foot__left">
          <button type="button" className="hx-btn" data-variant="ghost" data-size="sm" onClick={onCopyLink}>
            <Icon name="link" size="sm" />
            <span>{t('next.access.copyLink')}</span>
          </button>
          {manages && links && (
            <Tooltip label={t('next.access.linkSettings')}>
              <button
                type="button"
                className="hx-head-btn hx-acc-foot__gear"
                aria-label={t('next.access.linkSettings')}
                aria-pressed={view === SUB_VIEW.links}
                onClick={() => go(view === SUB_VIEW.links ? null : SUB_VIEW.links)}
              >
                <Icon name="settings-2" size="sm" />
              </button>
            </Tooltip>
          )}
          {footExtra}
        </div>
        <button type="button" className="hx-btn" data-variant="primary" data-size="sm" onClick={onClose}>
          {t('next.access.done')}
        </button>
      </div>
    </Frame>
  );
};

export default ShareDialog;
