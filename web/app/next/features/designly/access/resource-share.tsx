// «Поделиться»: one dialog for every kind, the same for everyone who may share. The field «Добавить людей
// или команды»; «У кого есть доступ» — the owner, the home team first, the people and teams; «Ссылки без входа»; at the
// foot «Копировать ссылку» and «Готово». «…» in the head: «Редакторы могут делиться» (the owner), «История доступа»,
// «Передать владение» (the owner, the lead and the admin of a team resource). One who may only look gets the address,
// «Копировать ссылку» and «Попросить правку». Views slide in inside the dialog; on the phone it is a bottom sheet whose
// head stays while its body scrolls, lifted over the keyboard. Every change waits for the gateway.
import { useEffect, useRef, useState } from 'react';
import AccessLinks from '@/next/features/designly/access/access-links';
import AccessLog from '@/next/features/designly/access/access-log';
import { EMAIL, type IProjectAccess } from '@/next/features/designly/access/api';
import type { IScopePage } from '@/next/features/designly/access/link-scope';
import PersonField from '@/next/features/designly/access/person-field';
import ResourceAdd from '@/next/features/designly/access/resource-add';
import {
  askFor,
  handOver,
  putEditorsCanShare,
  type IResourceRef,
  type IResourceView,
} from '@/next/features/designly/access/resource-api';
import ResourcePeople, {
  ResourceRequests,
  type IResourceSection,
} from '@/next/features/designly/access/resource-people';
import { Frame } from '@/next/features/designly/access/share-dialog';
import { useAction } from '@/next/features/designly/access/use-action';
import { useMe } from '@/next/stores/me';
import Icon from '@/next/ui/icon';
import Popover from '@/next/ui/popover';
import { usePresence } from '@/next/ui/presence';
import { usePhone } from '@/next/ui/use-media';
import PublishSection, { PublishForm } from '@/next/features/designly/publish/publish-section';
import type { IPublishState } from '@/next/features/designly/publish/use-publish';
import type { TAccessFocus } from '@/next/routes/routes';
import { t } from '@/next/i18n';
import '@/next/styles/design-access.css';

const VIEW = { log: 'log', transfer: 'transfer', links: 'links', publish: 'publish' } as const;
type TView = (typeof VIEW)[keyof typeof VIEW];
const VIEW_TITLE: Record<TView, string> = {
  log: 'next.access.history',
  transfer: 'next.access.transferTitle',
  links: 'next.access.linksTitle',
  publish: 'next.publish.section',
};
/** A view leaves in this long (styles/design-access.css, .hx-acc-view--sub[data-state='closing']). */
const VIEW_OUT_MS = 220;

export interface IResourceShareProps {
  open: boolean;
  onClose: () => void;
  target: IResourceRef;
  /** The resource's name, in the title. */
  title: string;
  view: IResourceView;
  onNext: (next: IResourceView) => void;
  /** The resource's own address: it opens for whoever already has access. */
  address: string;
  onCopyLink: () => void;
  focus?: TAccessFocus | null;
  /** The links without sign-in on their older routes, until they move to this dialog (stage «г»): the view the
   *  «Ссылки без входа» row opens. */
  links?: ILegacyLinks;
  /** «Опубликовать как сайт», last, where the design may be published as a site. */
  publish?: IPublishState;
}

export interface ILegacyLinks {
  project: string;
  access: IProjectAccess;
  onNext: (next: IProjectAccess) => void;
  pages?: readonly IScopePage[];
}

const LegacyLinks = ({ links }: { links: ILegacyLinks }) => {
  const action = useAction();
  return (
    <>
      <AccessLinks
        project={links.project}
        access={links.access}
        action={action}
        onNext={links.onNext}
        pages={links.pages}
      />
      {action.error && (
        <p className="hx-create__err" role="alert">
          {action.error}
        </p>
      )}
    </>
  );
};

/** Lifts the phone's sheet over the keyboard: the part of the screen the keyboard covers goes to --hx-kb on the sheet. */
const useSheetKeyboard = (node: HTMLElement | null, on: boolean) => {
  useEffect(() => {
    const viewport = window.visualViewport;
    const sheet = node?.closest<HTMLElement>('.hx-sheet') ?? null;
    if (!on || !viewport || !sheet) return;
    let frame = 0;
    const update = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const covered = Math.max(0, window.innerHeight - viewport.height - viewport.offsetTop);
        sheet.style.setProperty('--hx-kb', `${Math.round(covered)}px`);
      });
    };
    update();
    viewport.addEventListener('resize', update);
    viewport.addEventListener('scroll', update);
    return () => {
      cancelAnimationFrame(frame);
      viewport.removeEventListener('resize', update);
      viewport.removeEventListener('scroll', update);
      sheet.style.removeProperty('--hx-kb');
    };
  }, [node, on]);
};

const ShareMore = ({ props, onView }: { props: IResourceSection; onView: (view: TView) => void }) => {
  const { target, view, action, onNext } = props;
  const [open, setOpen] = useState(false);
  const owner = view.role === 'owner';
  const mayHand = owner || view.steward;
  const label = t('next.access.more');
  const go = (next: TView) => {
    setOpen(false);
    onView(next);
  };
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      label={label}
      side="bottom"
      align="end"
      className="hx-acc-pop hx-acc-pop--more"
      trigger={
        <button type="button" className="hx-head-btn" aria-label={label}>
          <Icon name="ellipsis" />
        </button>
      }
    >
      <div role="menu" aria-label={label} className="hx-acc-menu">
        {owner && (
          <>
            <button
              type="button"
              role="menuitemcheckbox"
              aria-checked={view.editorsCanShare}
              className="hx-acc-mi"
              disabled={action.busy !== null}
              onClick={() =>
                void action.run('share', async () =>
                  onNext(await putEditorsCanShare(target, !view.editorsCanShare)),
                )
              }
            >
              <Icon name="users" size="sm" className="hx-acc-mi__icon" />
              <span className="hx-acc-mi__text">{t('next.access.editorsShare')}</span>
              <span className="hx-acc-mi__box" aria-hidden="true">
                <Icon name="check" size="sm" />
              </span>
            </button>
            <span className="hx-acc-mi__sep" role="separator" />
          </>
        )}
        <button type="button" role="menuitem" className="hx-acc-mi" onClick={() => go(VIEW.log)}>
          <Icon name="history" size="sm" className="hx-acc-mi__icon" />
          <span className="hx-acc-mi__text">{t('next.access.history')}</span>
        </button>
        <button
          type="button"
          role="menuitem"
          className="hx-acc-mi"
          aria-disabled={!mayHand || undefined}
          onClick={() => {
            if (mayHand) go(VIEW.transfer);
          }}
        >
          <Icon name="arrow-right-left" size="sm" className="hx-acc-mi__icon" />
          <span className="hx-acc-mi__text">
            <span>{t('next.access.transferItem')}</span>
            {!mayHand && (
              <small className="hx-acc-mi__desc">
                {t('next.access.transferOnlyOwner', { name: view.owner?.name ?? '' })}
              </small>
            )}
          </span>
        </button>
      </div>
    </Popover>
  );
};

const Transfer = ({ props, onDone }: { props: IResourceSection; onDone: () => void }) => {
  const { target, action, onNext } = props;
  const [heir, setHeir] = useState('');
  const [sure, setSure] = useState(false);
  const email = heir.trim().toLowerCase();
  const idle = action.busy === null;
  return (
    <div className="hx-acc-stack" data-section="transfer">
      <p className="hx-acc-note">{t('next.access.transferLead')}</p>
      <PersonField
        label={t('next.access.heir')}
        placeholder={t('next.access.addPlaceholder')}
        value={heir}
        disabled={!idle || sure}
        onChange={setHeir}
      />
      {sure ? (
        <div className="hx-acc-ask-box" role="group" aria-label={t('next.access.transferTitle')}>
          <p className="hx-acc-ask-box__q">{t('next.access.transferAsk', { who: email })}</p>
          <div className="hx-acc-ask-box__do">
            <button
              type="button"
              className="hx-btn"
              data-variant="ghost"
              data-size="sm"
              onClick={() => setSure(false)}
            >
              {t('next.rowMenu.cancel')}
            </button>
            <button
              type="button"
              className="hx-btn"
              data-variant="danger"
              data-size="sm"
              data-busy={action.busy === 'transfer'}
              disabled={!idle}
              onClick={() =>
                void action.run('transfer', async () => {
                  onNext(await handOver(target, email));
                  onDone();
                })
              }
            >
              <span>{t('next.access.transferDo')}</span>
              <span className="hx-gate__spin" aria-hidden="true" />
            </button>
          </div>
        </div>
      ) : (
        <div className="hx-acc-ask-box__do">
          <button
            type="button"
            className="hx-btn"
            data-size="sm"
            disabled={!idle || !EMAIL.test(email)}
            onClick={() => setSure(true)}
          >
            {t('next.access.transferNext')}
          </button>
        </div>
      )}
    </div>
  );
};

/** One who may only look: the address, their role and «Попросить правку». */
const Looker = ({ props, address }: { props: IResourceSection; address: string }) => {
  const { target, view, action, onNext } = props;
  const asked = view.myRequest?.status === 'open';
  const role = view.role;
  return (
    <section className="hx-acc-sec" data-section="own">
      <div className="hx-acc-addr">
        <Icon name="link" size="sm" />
        <span className="hx-acc-addr__text">{address}</span>
      </div>
      <p className="hx-acc-ro">
        {role !== null && role !== 'owner' && <span>{t(`next.access.youCan.${role}`)}</span>}
        {role !== 'edit' && role !== 'owner' && (
          <>
            {role !== null && <span aria-hidden="true"> · </span>}
            {asked ? (
              <em className="hx-acc-ro__sent">{t('next.access.askedOwner')}</em>
            ) : (
              <button
                type="button"
                className="hx-acc-ro__ask"
                data-busy={action.busy === 'ask'}
                disabled={action.busy !== null}
                onClick={() => void action.run('ask', async () => onNext(await askFor(target, 'edit')))}
              >
                {t('next.access.askEditShort')}
              </button>
            )}
          </>
        )}
      </p>
    </section>
  );
};

const ResourceShare = ({
  open,
  onClose,
  target,
  title,
  view,
  onNext,
  address,
  onCopyLink,
  focus = null,
  links,
  publish,
}: IResourceShareProps) => {
  const action = useAction();
  const me = useMe();
  const phone = usePhone();
  const field = useRef<HTMLInputElement>(null);
  const [body, setBody] = useState<HTMLDivElement | null>(null);
  const [sub, setSub] = useState<TView | null>(null);
  const shown = usePresence(sub, VIEW_OUT_MS);
  const props: IResourceSection = { target, view, action, onNext };
  const manages = view.canManage;
  useSheetKeyboard(body, open && phone);

  // The dialog opens again on its main view.
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) setSub(null);
  }

  useEffect(() => {
    if (!open || !focus || !body) return;
    const timer = setTimeout(() => {
      body.querySelector(`[data-section="${focus}"]`)?.scrollIntoView?.({ block: 'nearest' });
    }, 50);
    return () => clearTimeout(timer);
  }, [open, focus, body]);

  const go = (next: TView | null) => {
    action.clear();
    setSub(next);
    // A view opens at its top, wherever the main one was scrolled to.
    body?.scrollTo?.({ top: 0 });
  };

  return (
    <Frame
      open={open}
      fixedHead
      onClose={onClose}
      initialFocus={manages ? field : undefined}
      title={t('next.access.shareTitle', { name: title || t('next.access.untitled') })}
      actions={manages && <ShareMore props={props} onView={go} />}
    >
      <div className="hx-modal__body hx-acc-body" ref={setBody}>
        {manages ? (
          <div className="hx-acc-views" data-sub={sub !== null}>
            <div className="hx-acc-view hx-acc-view--main" inert={sub !== null}>
              <ResourceAdd {...props} inputRef={field} />
              <ResourceRequests {...props} />
              <ResourcePeople {...props} me={me.k === 'ready' ? me.me.email : null} />
              {links && (
                <section className="hx-acc-sec" data-section="links">
                  <h3 className="hx-acc-sec__title">{t('next.access.guestLinks')}</h3>
                  <button type="button" className="hx-acc-row hx-acc-golinks" onClick={() => go(VIEW.links)}>
                    <span className="hx-acc-av" aria-hidden="true">
                      <Icon name="link" size="sm" />
                    </span>
                    <span className="hx-acc-row__who">
                      <span className="hx-acc-row__nm">{t('next.access.guestLinksOpen')}</span>
                      <span className="hx-acc-row__sub">{t('next.access.guestLinksLead')}</span>
                    </span>
                    <Icon name="chevron-right" size="sm" />
                  </button>
                </section>
              )}
              {publish && (
                <PublishSection state={publish} onPush={phone ? () => go(VIEW.publish) : undefined} />
              )}
            </div>
            {shown.value && (
              <div className="hx-acc-view hx-acc-view--sub" data-state={shown.state} inert={sub === null}>
                <div className="hx-acc-back">
                  <button
                    type="button"
                    className="hx-head-btn"
                    aria-label={t('next.access.backView')}
                    onClick={() => go(null)}
                  >
                    <Icon name="arrow-left" />
                  </button>
                  <h3 className="hx-acc-back__title">{t(VIEW_TITLE[shown.value])}</h3>
                </div>
                {shown.value === VIEW.log && <AccessLog project={target.id} kind={target.kind} />}
                {shown.value === VIEW.transfer && <Transfer props={props} onDone={() => go(null)} />}
                {shown.value === VIEW.links && links && <LegacyLinks links={links} />}
                {shown.value === VIEW.publish && publish && <PublishForm state={publish} />}
              </div>
            )}
          </div>
        ) : (
          <Looker props={props} address={address} />
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
        </div>
        <button type="button" className="hx-btn" data-variant="primary" data-size="sm" onClick={onClose}>
          {t('next.access.done')}
        </button>
      </div>
    </Frame>
  );
};

export default ResourceShare;
