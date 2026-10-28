// «Проверка машины» of «General»: only what is not in order on the machine, each with what was seen, the command that
// puts it right and a button that copies it; «Исправить» where the runtime can do it itself without root. Nothing is
// drawn while everything is in order.
import { Group, SetButton, Spin } from '@/next/features/settings/rows';
import { isKnownCheck, type IMachineCheck } from '@/next/features/settings/machine-checks';
import { t } from '@/next/i18n';

export interface IChecksGroupProps {
  checks: readonly IMachineCheck[];
  /** The check whose fix runs now. */
  fixing: string | null;
  onCopy: (command: string) => void;
  onFix: (id: string) => void;
}

const label = (c: IMachineCheck) => (isKnownCheck(c.id) ? t(`next.settings.checks.${c.id}`) : c.title);
const hint = (c: IMachineCheck) => (isKnownCheck(c.id) ? t(`next.settings.checks.${c.id}.hint`) : null);

const CheckRow = ({
  check,
  fixing,
  onCopy,
  onFix,
}: { check: IMachineCheck } & Omit<IChecksGroupProps, 'checks'>) => {
  const busy = fixing === check.id;
  const why = hint(check);
  return (
    <div className="hx-set__row hx-check" data-busy={busy || undefined} aria-busy={busy || undefined}>
      <div className="hx-set__row-text">
        <span className="hx-set__row-label">{label(check)}</span>
        {why && <span className="hx-set__row-hint">{why}</span>}
        {check.detail && <span className="hx-set__row-hint hx-check__seen">{check.detail}</span>}
        {check.fixCommand && <code className="hx-check__cmd">{check.fixCommand}</code>}
      </div>
      <Spin />
      <div className="hx-check__acts">
        {check.fixable && (
          <SetButton tone="primary" busy={busy} disabled={fixing !== null} onClick={() => onFix(check.id)}>
            {t('next.settings.checks.fix')}
          </SetButton>
        )}
        {check.fixCommand && (
          <SetButton tone="ghost" icon="copy" onClick={() => onCopy(check.fixCommand)}>
            {t('next.settings.checks.copy')}
          </SetButton>
        )}
      </div>
    </div>
  );
};

const ChecksGroup = ({ checks, fixing, onCopy, onFix }: IChecksGroupProps) => {
  const failing = checks.filter((c) => !c.ok);
  if (failing.length === 0) return null;
  return (
    <Group title={t('next.settings.checks.title')} foot={t('next.settings.checks.foot')}>
      {failing.map((check) => (
        <CheckRow key={check.id} check={check} fixing={fixing} onCopy={onCopy} onFix={onFix} />
      ))}
    </Group>
  );
};

export default ChecksGroup;
