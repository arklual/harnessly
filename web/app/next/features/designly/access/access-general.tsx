// «Общий доступ», Google's General access: one row — a lock or the team's mark, the level from a menu («Только
// приглашённые», «Команда может смотреть / комментировать / править») and one quiet line of what it means.
import { useState } from 'react';
import { putTeam, type TTeamLevel } from '@/next/features/designly/access/api';
import { TEAM_LEVELS, teamHint } from '@/next/features/designly/access/labels';
import { change, type ISectionProps } from '@/next/features/designly/access/section';
import Icon from '@/next/ui/icon';
import Popover from '@/next/ui/popover';
import { t } from '@/next/i18n';

const levelLabel = (level: TTeamLevel): string => t(`next.access.general.${level}`);

const AccessGeneral = (props: ISectionProps) => {
  const { project, access, action } = props;
  const [open, setOpen] = useState(false);
  const label = t('next.access.general');
  const pick = (level: TTeamLevel) => {
    setOpen(false);
    if (level !== access.team) change(props, 'team', () => putTeam(project, level));
  };
  return (
    <section className="hx-acc-sec" data-section="team">
      <h3 className="hx-acc-sec__title">{label}</h3>
      <div className="hx-acc-row hx-acc-gen">
        <span className="hx-acc-av" data-tone={access.team === 'none' ? 'quiet' : 'team'} aria-hidden="true">
          <Icon name={access.team === 'none' ? 'lock' : 'users'} size="sm" />
        </span>
        <span className="hx-acc-row__who">
          <Popover
            open={open}
            onOpenChange={setOpen}
            label={label}
            side="bottom"
            align="start"
            className="hx-acc-pop hx-acc-pop--wide"
            trigger={
              <button
                type="button"
                className="hx-acc-role hx-acc-gen__pick"
                aria-label={`${label}: ${levelLabel(access.team)}`}
                data-busy={action.busy === 'team'}
                disabled={action.busy !== null}
              >
                <span className="hx-acc-gen__text">{levelLabel(access.team)}</span>
                <Icon name="chevron-down" size="sm" />
              </button>
            }
          >
            <div role="menu" aria-label={label} className="hx-acc-menu">
              {TEAM_LEVELS.map((level) => (
                <button
                  key={level}
                  type="button"
                  role="menuitemradio"
                  aria-checked={level === access.team}
                  className="hx-acc-mi hx-acc-mi--two"
                  onClick={() => pick(level)}
                >
                  <span className="hx-acc-mi__text">
                    {levelLabel(level)}
                    <span className="hx-acc-mi__desc">{teamHint(level)}</span>
                  </span>
                  <Icon name="check" size="sm" className="hx-acc-mi__check" />
                </button>
              ))}
            </div>
          </Popover>
          <span className="hx-acc-row__sub">{teamHint(access.team)}</span>
        </span>
      </div>
    </section>
  );
};

export default AccessGeneral;
