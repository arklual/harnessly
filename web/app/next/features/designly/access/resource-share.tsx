// «Поделиться» (HRN-675): one dialog for every kind, the same for everyone who may share. The field «Добавить людей
// или команды»; «У кого есть доступ» — the owner, the home team first, the people and teams; «Ссылки без входа»; at the
// foot «Копировать ссылку» and «Готово». «…» in the head: «Редакторы могут делиться» (the owner), «История доступа»,
// «Передать владение» (the owner, the lead and the admin of a team resource). One who may only look gets the address,
// «Копировать ссылку» and «Попросить правку». Views slide in inside the dialog; on the phone it is a bottom sheet whose
// head stays while its body scrolls, lifted over the keyboard. Every change waits for the gateway.
import {
  memo,
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ReactNode,
  type RefObject,
} from 'react';
import AccessLinks from '@/next/features/designly/access/access-links';
import AccessLog, { warmLog, type ILogNames } from '@/next/features/designly/access/access-log';
import { EMAIL, type IProjectAccess } from '@/next/features/designly/access/api';
import type { IScopePage } from '@/next/features/designly/access/link-scope';
import { LINK_KIND, type ILinkContext } from '@/next/features/designly/access/link-form';
import PersonField from '@/next/features/designly/access/person-field';
import RequestForm from '@/next/features/designly/access/request-form';
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
import {
  LinkScreen,
  ResourceLinks,
  useLinksScreen,
  type ILinksScreen,
} from '@/next/features/designly/access/resource-links';
import { Frame, IDLE, Still, useCovered } from '@/next/features/designly/access/share-dialog';
import { useAction } from '@/next/features/designly/access/use-action';
import { useMe } from '@/next/stores/me';
import Icon from '@/next/ui/icon';
import Popover from '@/next/ui/popover';
import { usePresence, type TPresenceState } from '@/next/ui/presence';
import { usePhone } from '@/next/ui/use-media';
import PublishSection, { PublishForm } from '@/next/features/designly/publish/publish-section';
import type { IPublishState } from '@/next/features/designly/publish/use-publish';
import type { TAccessFocus } from '@/next/routes/routes';
import { t } from '@/next/i18n';
import '@/next/styles/design-access.css';

const VIEW = { log: 'log', transfer: 'transfer', links: 'links', link: 'link', publish: 'publish' } as const;
type TView = (typeof VIEW)[keyof typeof VIEW];
const VIEW_TITLE: Record<TView, string> = {
  log: 'next.access.history',
  transfer: 'next.access.transferTitle',
  links: 'next.access.linksTitle',
  link: 'next.access.linkNew',
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
  /** Opened to ask for more (the «Запросить доступ» of the viewing mode): one who may only look gets the request form open. */
  ask?: boolean;
  /** The links without sign-in on their older routes, until they move to this dialog (stage «г»): the view the
   *  «Ссылки без входа» row opens. */
  links?: ILegacyLinks;
  /** «Опубликовать как сайт», last, where the design may be published as a site. */
  publish?: IPublishState;
  /** What the screen of a link knows of the thing shared (stage «г»): its kind, pages, slides, formats, look. */
  linkContext?: ILinkContext;
  /** A block after the links for whoever manages: a document's old public link, while it is open (stage «д»). */
  extra?: ReactNode;
  /** What the foot carries after «Копировать ссылку»: a document made of a file puts its «Отвязать» here. */
  footExtra?: ReactNode;
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

/** How long the request form takes to fold away (its closing animation in design-access.css). */
const ASK_OUT_MS = 160;

/**
 * One who may only look: the address, their role and «Попросить правку», which opens the request (the level and a word
 * to the owner) right here; sent, it goes to the owner and those who run the access, and the line says so while it is
 * open. `askFirst`: the dialog was opened to ask (the «Запросить доступ» of the viewing mode), the form is open at once.
 */
const Looker = ({
  props,
  address,
  askFirst,
}: {
  props: IResourceSection;
  address: string;
  askFirst: boolean;
}) => {
  const { target, view, onNext } = props;
  const status = view.myRequest?.status ?? null;
  const asked = status === 'open';
  const role = view.role;
  const mayAsk = role !== 'edit' && role !== 'owner';
  const [asking, setAsking] = useState(askFirst && mayAsk && !asked);
  // Opened again to ask: the form opens again (state adjusted during render, not in an effect).
  const [wasAskFirst, setWasAskFirst] = useState(askFirst);
  if (askFirst !== wasAskFirst) {
    setWasAskFirst(askFirst);
    if (askFirst && mayAsk && !asked) setAsking(true);
  }
  const form = usePresence(asking && !asked ? true : null, ASK_OUT_MS);
  return (
    <section className="hx-acc-sec" data-section="own">
      <div className="hx-acc-addr">
        <Icon name="link" size="sm" />
        <span className="hx-acc-addr__text">{address}</span>
      </div>
      <p className="hx-acc-ro">
        {role !== null && role !== 'owner' && <span>{t(`next.access.youCan.${role}`)}</span>}
        {mayAsk && (
          <>
            {role !== null && <span aria-hidden="true"> · </span>}
            {asked ? (
              <em className="hx-acc-ro__sent">{t('next.access.askedOwner')}</em>
            ) : (
              <button
                type="button"
                className="hx-acc-ro__ask"
                aria-expanded={asking}
                onClick={() => setAsking((now) => !now)}
              >
                {t('next.access.askEditShort')}
              </button>
            )}
          </>
        )}
      </p>
      {form.value && (
        <div className="hx-acc-ro__form" data-state={form.state}>
          <RequestForm
            mayComment={role === null || role === 'view'}
            status={status}
            initialLevel="edit"
            autoFocus
            onSend={async (level, message) => {
              onNext(await askFor(target, level, message));
              setAsking(false);
            }}
          />
        </div>
      )}
    </section>
  );
};

const DOC_LINKS: ILinkContext = { kind: LINK_KIND.doc };

/** The names the history shows for ids: the teams the view knows and its links. */
const logNamesOf = (view: IResourceView): ILogNames => {
  const teams = new Map<string, string>();
  if (view.homeTeam) teams.set(view.homeTeam.teamId, view.homeTeam.name);
  for (const grant of view.grants ?? []) if (grant.type === 'team') teams.set(grant.id, grant.name);
  return { teams, links: new Map((view.links ?? []).map((link) => [link.id, link.forWhom])) };
};

type TSubState = TPresenceState | typeof IDLE;

interface IMainProps extends IResourceSection {
  me: string | null;
  phone: boolean;
  ownLinks: boolean;
  legacy: boolean;
  linkContext: ILinkContext;
  links: Pick<ILinksScreen, 'start' | 'fresh'>;
  field: RefObject<HTMLInputElement | null>;
  extra?: ReactNode;
  publish?: IPublishState;
  onView: (view: TView) => void;
}

/** The main view: who has access, the links, publishing. Drawn again only when what it shows changes, not when a view
 *  slides over it — the rows' menus made each switch a long task (HRN-675). */
const Main = memo(
  ({
    target,
    view,
    action,
    onNext,
    me,
    phone,
    ownLinks,
    legacy,
    linkContext,
    links,
    field,
    extra,
    publish,
    onView,
  }: IMainProps) => {
    const props: IResourceSection = { target, view, action, onNext };
    return (
      <>
        <ResourceAdd {...props} inputRef={field} />
        <ResourceRequests {...props} />
        <ResourcePeople {...props} me={me} />
        {ownLinks && (
          <ResourceLinks
            {...props}
            ctx={linkContext}
            me={me}
            screen={links}
            onOpen={() => onView(VIEW.link)}
          />
        )}
        {!ownLinks && legacy && (
          <section className="hx-acc-sec" data-section="links">
            <h3 className="hx-acc-sec__title">{t('next.access.guestLinks')}</h3>
            <button type="button" className="hx-acc-row hx-acc-golinks" onClick={() => onView(VIEW.links)}>
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
        {extra}
        {publish && (
          <PublishSection state={publish} onPush={phone ? () => onView(VIEW.publish) : undefined} />
        )}
      </>
    );
  },
);

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
  ask = false,
  links,
  publish,
  linkContext = DOC_LINKS,
  extra,
  footExtra,
}: IResourceShareProps) => {
  const action = useAction();
  const me = useMe();
  const phone = usePhone();
  const field = useRef<HTMLInputElement>(null);
  const [body, setBody] = useState<HTMLDivElement | null>(null);
  const [sub, setSub] = useState<TView | null>(null);
  const shown = usePresence(sub, VIEW_OUT_MS);
  // The views opened since the dialog opened stay mounted while hidden: going back and there again draws nothing anew
  // and reads nothing again (the history).
  const [seen, setSeen] = useState<readonly TView[]>([]);
  if (sub !== null && !seen.includes(sub)) setSeen([...seen, sub]);
  const props: IResourceSection = { target, view, action, onNext };
  const email = me.k === 'ready' ? me.me.email : null;
  const manages = view.canManage;
  const linkForm = useId();
  const screen = useLinksScreen({ target, view, ctx: linkContext, action, onNext });
  // The gateway of stage «г» answers the links in the view; an older one keeps them on their older routes.
  const ownLinks = view.links !== undefined;
  useSheetKeyboard(body, open && phone);

  // The dialog opens again on its main view.
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) {
      setSub(null);
      setSeen([]);
    }
  }

  useEffect(() => (open ? warmLog() : undefined), [open]);

  useEffect(() => {
    if (!open || !focus || !body) return;
    const timer = setTimeout(() => {
      body.querySelector(`[data-section="${focus}"]`)?.scrollIntoView?.({ block: 'nearest' });
    }, 50);
    return () => clearTimeout(timer);
  }, [open, focus, body]);

  const { clear } = action;
  const go = useCallback(
    (next: TView | null) => {
      clear();
      setSub(next);
      // A view opens at its top, wherever the main one was scrolled to.
      body?.scrollTo?.({ top: 0 });
    },
    [clear, body],
  );
  const listScreen = useMemo(
    () => ({ start: screen.start, fresh: screen.fresh }),
    [screen.start, screen.fresh],
  );
  const names = useMemo(() => logNamesOf(view), [view]);
  const covered = useCovered(sub !== null);
  const stateOf = (v: TView): TSubState => (v === shown.value ? shown.state : IDLE);
  const headOf = (v: TView): string =>
    v === VIEW.link && screen.editing ? t('next.access.links.editTitle') : t(VIEW_TITLE[v]);

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
          <div className="hx-acc-views" data-sub={sub !== null} data-covered={covered}>
            <div className="hx-acc-view hx-acc-view--main" aria-hidden={sub !== null || undefined}>
              <Main
                target={target}
                view={view}
                action={action}
                onNext={onNext}
                me={email}
                phone={phone}
                ownLinks={ownLinks}
                legacy={links !== undefined}
                linkContext={linkContext}
                links={listScreen}
                field={field}
                extra={extra}
                publish={publish}
                onView={go}
              />
            </div>
            {seen.map((v) => (
              <div
                key={v}
                className="hx-acc-view hx-acc-view--sub"
                data-state={stateOf(v)}
                aria-hidden={sub !== v || undefined}
              >
                <Still still={sub !== v}>
                  <div className="hx-acc-back">
                    <button
                      type="button"
                      className="hx-head-btn"
                      aria-label={t('next.access.backView')}
                      onClick={() => go(null)}
                    >
                      <Icon name="arrow-left" />
                    </button>
                    <h3 className="hx-acc-back__title">{headOf(v)}</h3>
                  </div>
                  {v === VIEW.log && <AccessLog project={target.id} kind={target.kind} names={names} />}
                  {v === VIEW.transfer && <Transfer props={props} onDone={() => go(null)} />}
                  {v === VIEW.links && links && <LegacyLinks links={links} />}
                  {v === VIEW.link && (
                    <LinkScreen
                      id={linkForm}
                      screen={screen}
                      ctx={linkContext}
                      onSubmit={() =>
                        void screen.submit().then((done) => {
                          if (done) go(null);
                        })
                      }
                    />
                  )}
                  {v === VIEW.publish && publish && <PublishForm state={publish} />}
                </Still>
              </div>
            ))}
          </div>
        ) : (
          <Looker props={props} address={address} askFirst={ask} />
        )}
        {action.error && (
          <p className="hx-create__err" role="alert">
            {action.error}
          </p>
        )}
      </div>
      {sub === VIEW.link ? (
        <div className="hx-modal__foot hx-acc-foot hx-acc-foot--full">
          <button
            type="submit"
            form={linkForm}
            className="hx-btn"
            data-variant="primary"
            data-busy={action.busy === 'link-save'}
            disabled={action.busy !== null}
          >
            <Icon name="link" size="sm" />
            <span>{screen.editing ? t('next.access.links.save') : t('next.access.links.create')}</span>
            <span className="hx-gate__spin" aria-hidden="true" />
          </button>
        </div>
      ) : (
        <div className="hx-modal__foot hx-acc-foot">
          <div className="hx-acc-foot__left">
            <button type="button" className="hx-btn" data-variant="ghost" data-size="sm" onClick={onCopyLink}>
              <Icon name="link" size="sm" />
              <span>{t('next.access.copyLink')}</span>
            </button>
            {footExtra}
          </div>
          <button type="button" className="hx-btn" data-variant="primary" data-size="sm" onClick={onClose}>
            {t('next.access.done')}
          </button>
        </div>
      )}
    </Frame>
  );
};

export default ResourceShare;
