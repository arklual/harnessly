// «Поделиться» of Designly, the Google Docs / Figma way: simple by default, the rest on demand. A person who
// manages sees one field to add people (the role and its term at the field's end), who has access with each one's role
// menu, the requests when there are any, the team's «Общий доступ», and «Копировать ссылку» (the design's own address,
// it gives nobody access) / «Готово» at the foot.
// What is needed rarely or is dangerous sits in «…» of the head; the journal, the link settings and the hand-over are
// views that slide in inside the dialog with a way back. One who may only look sees their own role and a way to ask
// for more. Every change waits for the gateway and shows what it answered. On the phone it is a bottom sheet.
import { useEffect, useRef, useState, type ReactNode, type RefObject } from 'react';
import AccessAdd from '@/next/features/designly/access/access-add';
import AccessGeneral from '@/next/features/designly/access/access-general';
import AccessLinks from '@/next/features/designly/access/access-links';
import AccessLog from '@/next/features/designly/access/access-log';
import AccessPeople from '@/next/features/designly/access/access-people';
import AccessRequests from '@/next/features/designly/access/access-requests';
import AccessTransfer from '@/next/features/designly/access/access-transfer';
import { atLeast, isUnowned, type IProjectAccess } from '@/next/features/designly/access/api';
import { roleLabel } from '@/next/features/designly/access/labels';
import RequestForm from '@/next/features/designly/access/request-form';
import ShareMenu, { SUB_VIEW, type TSubView } from '@/next/features/designly/access/share-menu';
import { useAction } from '@/next/features/designly/access/use-action';
import Modal from '@/next/features/shell/modal';
import { useMe } from '@/next/stores/me';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';
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
}

/** A view leaves in this long (styles/design-access.css, .hx-acc-view--sub[data-state='closing']). */
const VIEW_OUT_MS = 220;

const VIEW_TITLE: Record<TSubView, string> = {
  log: 'next.access.history',
  links: 'next.access.linksTitle',
  transfer: 'next.access.transferTitle',
};

interface IFrameProps {
  open: boolean;
  /** The first focus on a wide window (the add field); the phone's sheet keeps it on itself, no keyboard pops up. */
  initialFocus?: RefObject<HTMLElement | null>;
  onClose: () => void;
  title: string;
  actions: ReactNode;
  children: ReactNode;
}

/** The dialog on a wide window, the bottom sheet on the phone; the same head, body and foot in both. */
const Frame = ({ open, initialFocus, onClose, title, actions, children }: IFrameProps) => {
  const phone = usePhone();
  if (!phone)
    return (
      <Modal
        open={open}
        onClose={onClose}
        title={title}
        boxClass="hx-acc"
        actions={actions}
        initialFocus={initialFocus}
      >
        {children}
      </Modal>
    );
  return (
    <Sheet open={open} onClose={onClose} label={title} className="hx-acc-sheet">
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
}: IShareDialogProps) => {
  const action = useAction();
  const me = useMe();
  const body = useRef<HTMLDivElement>(null);
  const field = useRef<HTMLInputElement>(null);
  const [view, setView] = useState<TSubView | null>(null);
  const sub = usePresence(view, VIEW_OUT_MS);
  const idle = action.busy === null;
  const section = { project, access, action, onNext };
  const manages = access.canManage;

  // The dialog opens again on its main view (adjusted while rendering; a closing dialog keeps its view as it leaves).
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) setView(null);
  }

  // A push says which block it is about: the dialog opens with that one in view.
  useEffect(() => {
    if (!open || !focus) return;
    const timer = setTimeout(() => {
      body.current?.querySelector(`[data-section="${focus}"]`)?.scrollIntoView?.({ block: 'nearest' });
    }, 50);
    return () => clearTimeout(timer);
  }, [open, focus, access.canManage]);

  const go = (next: TSubView | null) => {
    action.clear();
    setView(next);
  };

  const shareTitle = t('next.access.shareTitle', { name: title || t('next.access.untitled') });

  return (
    <Frame
      open={open}
      onClose={onClose}
      initialFocus={manages ? field : undefined}
      title={shareTitle}
      actions={manages && <ShareMenu {...section} onView={go} />}
    >
      <div className="hx-modal__body hx-acc-body" ref={body}>
        {manages ? (
          <div className="hx-acc-views" data-sub={view !== null}>
            <div className="hx-acc-view hx-acc-view--main" inert={view !== null}>
              <AccessAdd {...section} inputRef={field} />
              <AccessRequests {...section} />
              <AccessPeople {...section} me={me.k === 'ready' ? me.me.email : null} />
              <AccessGeneral {...section} />
            </div>
            {sub.value && (
              <div className="hx-acc-view hx-acc-view--sub" data-state={sub.state} inert={view === null}>
                <div className="hx-acc-back">
                  <button
                    type="button"
                    className="hx-head-btn"
                    aria-label={t('next.access.backView')}
                    onClick={() => go(null)}
                  >
                    <Icon name="arrow-left" />
                  </button>
                  <h3 className="hx-acc-back__title">{t(VIEW_TITLE[sub.value])}</h3>
                </div>
                {sub.value === SUB_VIEW.log && <AccessLog project={project} />}
                {sub.value === SUB_VIEW.links && <AccessLinks {...section} />}
                {sub.value === SUB_VIEW.transfer && <AccessTransfer {...section} onDone={() => go(null)} />}
              </div>
            )}
          </div>
        ) : (
          <section className="hx-acc-sec" data-section="own">
            {access.role && (
              <p className="hx-acc-note">{t('next.access.yourRole', { role: roleLabel(access.role) })}</p>
            )}
            {(access.role === 'view' || access.role === 'comment') && (
              <RequestForm project={project} role={access.role} request={access.request} onAsked={onNext} />
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
          {manages && (
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
        </div>
        <button type="button" className="hx-btn" data-variant="primary" data-size="sm" onClick={onClose}>
          {t('next.access.done')}
        </button>
      </div>
    </Frame>
  );
};

export default ShareDialog;
