import { ChartColumnBig, Palette, TrendingUp } from 'lucide-react';
import { cx } from '@/components/agents/ui';
import { t } from '@/lib/i18n';

interface ICentreNavProps {
  statsOpen?: boolean;
  onOpenStats?: () => void;
  efficiencyOpen?: boolean;
  efficiencyAlert?: boolean;
  onOpenEfficiency?: () => void;
  designOpen?: boolean;
  /** «Claude Design» opens the team's library of the runtime the page is on; it needs no machine feature and works on a phone. */
  onOpenDesign?: () => void;
}

/** The views of the centre column the left menu leads to, under «New conversation». */
const CentreNav = ({
  statsOpen = false,
  onOpenStats,
  efficiencyOpen = false,
  efficiencyAlert = false,
  onOpenEfficiency,
  designOpen = false,
  onOpenDesign,
}: ICentreNavProps) => (
  <>
    {onOpenStats && (
      <button
        type="button"
        className={cx('tw-row tw-nav-link tw-nav-desktop', statsOpen && 'tw-nav-link-on')}
        aria-current={statsOpen ? 'page' : undefined}
        onClick={onOpenStats}
      >
        <span className="tw-srow-lead">
          <ChartColumnBig aria-hidden="true" />
        </span>
        <span className="tw-row-title">{t('sidebar.centreNav.stats')}</span>
      </button>
    )}
    {onOpenEfficiency && (
      <button
        type="button"
        className={cx('tw-row tw-nav-link tw-nav-desktop', efficiencyOpen && 'tw-nav-link-on')}
        aria-current={efficiencyOpen ? 'page' : undefined}
        onClick={onOpenEfficiency}
      >
        <span className="tw-srow-lead">
          <TrendingUp aria-hidden="true" />
        </span>
        <span className="tw-row-title">{t('sidebar.centreNav.efficiency')}</span>
        {efficiencyAlert && (
          <span
            className="size-2 flex-none rounded-full bg-(--t-status-danger)"
            role="img"
            aria-label={t('sidebar.centreNav.alert')}
            title={t('sidebar.centreNav.alertTitle')}
          />
        )}
      </button>
    )}
    {onOpenDesign && (
      <button
        type="button"
        className={cx('tw-row tw-nav-link', designOpen && 'tw-nav-link-on')}
        aria-current={designOpen ? 'page' : undefined}
        onClick={onOpenDesign}
      >
        <span className="tw-srow-lead">
          <Palette aria-hidden="true" />
        </span>
        <span className="tw-row-title">Claude Design</span>
      </button>
    )}
  </>
);

export default CentreNav;
