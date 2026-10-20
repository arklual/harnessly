import type { ReactNode } from 'react';
import { Hourglass } from 'lucide-react';
import type { IEfficiencyResource } from '@/hooks/use-efficiency';
import { LoadFailed } from '@/components/agents/efficiency/primitives';
import { t } from '@/lib/i18n';

export const Pending = () => (
  <div className="ts-state flex-col text-center">
    <Hourglass className="size-5 text-(--t-text-muted)" aria-hidden="true" />
    <strong className="text-(--t-text)">{t('efficiency.gate.pendingTitle')}</strong>
    <span className="max-w-md text-[13px]">{t('efficiency.gate.pendingText')}</span>
  </div>
);

interface IGateProps<T> {
  resource: IEfficiencyResource<T>;
  skeleton: ReactNode;
  children: (data: T) => ReactNode;
}

/** One place for the four states every efficiency screen has: loading, not deployed yet, failed, ready. */
const Gate = <T,>({ resource, skeleton, children }: IGateProps<T>) => {
  if (resource.status === 'loading') return <div aria-busy="true">{skeleton}</div>;
  if (resource.status === 'pending') return <Pending />;
  if (resource.status === 'error' || !resource.data)
    return <LoadFailed text={resource.error || t('efficiency.gate.loadFailed')} onRetry={resource.refresh} />;
  return <>{children(resource.data)}</>;
};

export default Gate;
