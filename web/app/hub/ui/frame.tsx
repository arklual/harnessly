// The two page shapes of the hub: the onboarding window (a step on the left, a picture on the right) and the plain
// page (a column) for the space, its settings and the invitation.
import { type ReactNode } from 'react';

import { Button, Menu, type MenuEntry } from '@kit';

import { t, type Key } from '../lib/i18n';
import { Link, navigate } from '../lib/router';
import {
  ArrowUp,
  ChevronDown,
  CircleCheck,
  FilePen,
  FileSearch,
  GitBranch,
  GitPullRequest,
  Server,
  Cpu,
  TestTubeDiagonal,
} from './icons';

export const STEP_COUNT = 6;

const Brand = ({ to = '/' }: { to?: string }) => (
  <Link to={to} className="hub-brand">
    <span className="hub-orb" />
    harnessly
  </Link>
);

export interface StepFrameProps {
  /** 0..5: Аккаунт, Пространство, Машины, Агенты, Доступы, Готово. */
  step: number;
  icon: ReactNode;
  title: string;
  lead?: string;
  children: ReactNode;
  foot?: ReactNode;
  /** Where a finished step's segment leads; none: the segments are not buttons. */
  goStep?: (index: number) => void;
  /** The sign-in screens have no step bar. */
  bare?: boolean;
  /** The caption of the picture when it is not the step's own. */
  caption?: 'login';
  /** The right side of the head: the person's menu once signed in. */
  headSide?: ReactNode;
}

export const StepFrame = ({
  step,
  icon,
  title,
  lead,
  children,
  foot,
  goStep,
  bare,
  caption,
  headSide,
}: StepFrameProps) => (
  <div className="hub">
    <div className="hub-win">
      <div className="hub-form">
        <header className="hub-head">
          <Brand />
          {headSide ? (
            <span className="hub-headside">{headSide}</span>
          ) : (
            !bare && (
              <span className="hub-stepn">{t('frame.stepOf', { n: step + 1, total: STEP_COUNT })}</span>
            )
          )}
        </header>
        {!bare && (
          <nav className="hub-steps" aria-label={t('frame.steps')}>
            {Array.from({ length: STEP_COUNT }, (_, i) => {
              const st = i < step ? 'done' : i === step ? 'now' : 'next';
              const name = t(`step.${i}` as Key);
              return (
                <button
                  key={i}
                  type="button"
                  className="hub-seg"
                  data-st={st}
                  aria-label={t('frame.stepName', { n: i + 1, name })}
                  aria-current={st === 'now' ? 'step' : undefined}
                  tabIndex={st === 'done' && goStep ? 0 : -1}
                  onClick={() => st === 'done' && goStep?.(i)}
                >
                  <i />
                </button>
              );
            })}
          </nav>
        )}
        <div className="hub-col">
          <section className="hub-body" key={bare ? `bare-${title}` : step}>
            <span className="hub-badge" aria-hidden="true">
              {icon}
            </span>
            <h1 className="hub-title">{title}</h1>
            {lead && <p className="hub-lead">{lead}</p>}
            {children}
          </section>
          {foot && <div className="hub-foot">{foot}</div>}
        </div>
      </div>
      <Viz step={step} caption={caption} />
    </div>
  </div>
);

/** The product, drawn once per step: a chat that fills up with what was connected. A picture, not data. */
const Viz = ({ step, caption }: { step: number; caption?: 'login' }) => {
  const capT = caption === 'login' ? t('viz.login.t') : t(`viz.cap.${step}.t` as Key);
  const capS = caption === 'login' ? t('viz.login.s') : t(`viz.cap.${step}.s` as Key);
  const hasMachine = step >= 3;
  const hasAgent = step >= 4;
  const hasGit = step >= 5;
  return (
    <aside className="hub-viz" aria-label={t('viz.label')}>
      <div className="hub-scene" aria-hidden="true">
        <div className="hub-app">
          <div className="hub-app__bar" data-hl={step === 1 || undefined}>
            <span className="hub-orb" />
            <span className="hub-ph">{t('viz.space')}</span>
            <span className="hub-app__av">
              <span>С</span>
              {step >= 2 && (
                <>
                  <span>Р</span>
                  <span>К</span>
                </>
              )}
            </span>
          </div>
          <div className="hub-app__body">
            <div className="hub-ask">{t('viz.ask')}</div>
            <div className="hub-run">
              <div className="hub-run__head">
                <span className="hub-orb" />
                <span>{t('viz.agent')}</span>
                <small>· {t('viz.yourMachine')}</small>
              </div>
              <div className="hub-line">
                <FileSearch size={14} />
                <span>{t('viz.line1')}</span>
              </div>
              <div className="hub-line">
                <TestTubeDiagonal size={14} />
                <span>{t('viz.line2')}</span>
              </div>
              <div className="hub-line">
                <FilePen size={14} />
                <span>{t('viz.line3')}</span>
              </div>
              <div className="hub-line">
                <CircleCheck size={14} />
                <span>{t('viz.line4')}</span>
              </div>
              <div className="hub-line" data-final>
                {hasGit ? <GitPullRequest size={14} /> : <GitBranch size={14} />}
                <span>{hasGit ? t('viz.finalMr') : t('viz.finalBranch')}</span>
              </div>
            </div>
          </div>
          <div className="hub-app__foot">
            <span className="hub-app__ph">{t('viz.placeholder')}</span>
            <span className="hub-chip" data-off={!hasMachine || undefined} data-hl={step === 2 || undefined}>
              <Server size={13} />
              {t('viz.machine')}
            </span>
            <span className="hub-chip" data-off={!hasAgent || undefined} data-hl={step === 3 || undefined}>
              <Cpu size={13} />
              {t('viz.agent')}
            </span>
            <span className="hub-chip" data-off={!hasGit || undefined} data-hl={step === 4 || undefined}>
              <GitPullRequest size={13} />
              Git
            </span>
            <span className="hub-send">
              <ArrowUp size={14} />
            </span>
          </div>
        </div>
      </div>
      <div className="hub-cap" key={caption ?? step}>
        <p className="hub-cap__t">{capT}</p>
        <p className="hub-cap__s">{capS}</p>
      </div>
    </aside>
  );
};

export interface PlainPageProps {
  children: ReactNode;
  title?: string;
  /** The right side of the head. */
  side?: ReactNode;
}

export const PlainPage = ({ children, title, side }: PlainPageProps) => (
  <div className="hub">
    <div className="hub-page">
      <header className="hub-head">
        <Brand />
        <span className="hub-headside">{side}</span>
      </header>
      <div className="hub-page__body">
        {title && <h1 className="hub-page__title">{title}</h1>}
        {children}
      </div>
    </div>
  </div>
);

export interface AccountMenuProps {
  name: string;
  spaces: { slug: string; name: string; current?: boolean }[];
  onSignOut: () => void;
}

/** The menu in the head of the signed-in pages: the spaces, a new one, the account, sign out. */
export const AccountMenu = ({ name, spaces, onSignOut }: AccountMenuProps) => {
  const items: MenuEntry[] = [
    ...spaces.map<MenuEntry>((s) => ({
      id: `space-${s.slug}`,
      label: s.current ? <b>{s.name}</b> : s.name,
      onSelect: () => navigate(`/s/${s.slug}`),
    })),
    'separator',
    { id: 'new', label: t('menu.newSpace'), onSelect: () => navigate('/new') },
    { id: 'account', label: t('menu.account'), onSelect: () => navigate('/settings') },
    { id: 'out', label: t('menu.signOut'), onSelect: onSignOut },
  ];
  return (
    <Menu
      items={items}
      label={t('menu.label')}
      placement={{ side: 'bottom', align: 'end' }}
      trigger={(p) => (
        <Button {...p} appearance="outline" size="s" iconEnd={<ChevronDown size={14} />}>
          {name}
        </Button>
      )}
    />
  );
};
