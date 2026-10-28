// «…» in the head of the share dialog: what is needed rarely or is dangerous. «История доступа» for whoever manages;
// for the owner also «Правщики могут делиться», «Закрыть доступ всем» (the menu itself asks again before it does) and
// «Передать владение…» (its own view, with a second step).
import { useState } from 'react';
import { isDocRef, revokeAll, setEditorsCanShare } from '@/next/features/designly/access/api';
import { change, type ISectionProps } from '@/next/features/designly/access/section';
import Icon from '@/next/ui/icon';
import Popover from '@/next/ui/popover';
import { t } from '@/next/i18n';

export const SUB_VIEW = { log: 'log', links: 'links', transfer: 'transfer' } as const;
export type TSubView = (typeof SUB_VIEW)[keyof typeof SUB_VIEW];

interface IShareMenuProps extends ISectionProps {
  /** False where there are no links to set up (a board). */
  links: boolean;
  onView: (view: TSubView) => void;
}

const ShareMenu = (props: IShareMenuProps) => {
  const { project, access, action, links, onView } = props;
  const [open, setOpen] = useState(false);
  const [asking, setAsking] = useState(false);
  const owner = access.role === 'owner';
  // A document has no hand-over and no «close to all» yet: its links are revoked one by one in the link settings.
  const wholeProject = !isDocRef(project);
  const idle = action.busy === null;
  const label = t('next.access.more');
  const close = () => {
    setOpen(false);
    setAsking(false);
  };
  const go = (view: TSubView) => {
    close();
    onView(view);
  };

  return (
    <Popover
      open={open}
      onOpenChange={(next) => (next ? setOpen(true) : close())}
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
      {asking ? (
        <div className="hx-acc-ask-box" role="alertdialog" aria-label={t('next.access.revokeAllAsk')}>
          <p className="hx-acc-ask-box__q">{t('next.access.revokeAllAsk')}</p>
          <p className="hx-acc-ask-box__lead">{t('next.access.revokeAllLead')}</p>
          <div className="hx-acc-ask-box__do">
            <button
              type="button"
              className="hx-btn"
              data-variant="ghost"
              data-size="sm"
              onClick={() => setAsking(false)}
            >
              {t('next.rowMenu.cancel')}
            </button>
            <button
              type="button"
              className="hx-btn"
              data-variant="danger"
              data-size="sm"
              data-busy={action.busy === 'revokeAll'}
              disabled={!idle}
              onClick={() =>
                void action.run('revokeAll', async () => {
                  props.onNext(await revokeAll(project));
                  close();
                })
              }
            >
              <span>{t('next.access.revokeAllDo')}</span>
              <span className="hx-gate__spin" aria-hidden="true" />
            </button>
          </div>
        </div>
      ) : (
        <div role="menu" aria-label={label} className="hx-acc-menu">
          <button type="button" role="menuitem" className="hx-acc-mi" onClick={() => go(SUB_VIEW.log)}>
            <Icon name="history" size="sm" className="hx-acc-mi__icon" />
            <span className="hx-acc-mi__text">{t('next.access.history')}</span>
          </button>
          {links && (
            <button type="button" role="menuitem" className="hx-acc-mi" onClick={() => go(SUB_VIEW.links)}>
              <Icon name="link" size="sm" className="hx-acc-mi__icon" />
              <span className="hx-acc-mi__text">{t('next.access.linkSettings')}</span>
            </button>
          )}
          {owner && (
            <>
              <span className="hx-acc-mi__sep" role="separator" />
              <button
                type="button"
                role="menuitemcheckbox"
                aria-checked={access.editorsCanShare}
                className="hx-acc-mi"
                disabled={!idle}
                onClick={() =>
                  change(props, 'share', () => setEditorsCanShare(project, !access.editorsCanShare))
                }
              >
                <Icon name="users" size="sm" className="hx-acc-mi__icon" />
                <span className="hx-acc-mi__text">{t('next.access.editorsShare')}</span>
                <span className="hx-acc-mi__box" aria-hidden="true">
                  <Icon name="check" size="sm" />
                </span>
              </button>
              {wholeProject && (
                <>
                  <span className="hx-acc-mi__sep" role="separator" />
                  <button type="button" role="menuitem" className="hx-acc-mi" onClick={() => setAsking(true)}>
                    <Icon name="ban" size="sm" className="hx-acc-mi__icon" />
                    <span className="hx-acc-mi__text">{t('next.access.revokeAll')}</span>
                  </button>
                  <button
                    type="button"
                    role="menuitem"
                    className="hx-acc-mi"
                    onClick={() => go(SUB_VIEW.transfer)}
                  >
                    <Icon name="arrow-right-left" size="sm" className="hx-acc-mi__icon" />
                    <span className="hx-acc-mi__text">{t('next.access.transferItem')}</span>
                  </button>
                </>
              )}
            </>
          )}
        </div>
      )}
    </Popover>
  );
};

export default ShareMenu;
