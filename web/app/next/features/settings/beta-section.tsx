// «Бета»: features that are not ready for everyone. Each is off until the person turns it on, in this browser.
import { Group, SwitchRow } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import { BETA_FLAGS, type BetaFlag, type BetaFlags } from '@/next/stores/beta';

export interface IBetaSectionProps {
  flags: BetaFlags;
  onChange: (flag: BetaFlag, on: boolean) => void;
}

const BetaSection = ({ flags, onChange }: IBetaSectionProps) => (
  <Group title={t('next.settings.beta.group')} foot={t('next.settings.beta.foot')}>
    {BETA_FLAGS.map((flag) => (
      <SwitchRow
        key={flag}
        label={t(`next.settings.beta.${flag}`)}
        hint={t(`next.settings.beta.${flag}.hint`)}
        on={flags[flag]}
        onChange={(on) => onChange(flag, on)}
      />
    ))}
  </Group>
);

export default BetaSection;
