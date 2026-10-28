// Settings → «Записи»: the person's recordings from every machine — play, download, delete. Where the cluster
// keeps none (standalone) the list is this browser's own, and the page says so.
import { useTakes } from '@/next/bindings/use-takes';
import { Group } from '@/next/features/settings/rows';
import TakesList from '@/next/features/composer/takes-list';
import { t } from '@/next/i18n';

export const TakesPage = ({ now }: { now: number }) => {
  const { rows, loading, remove, shared } = useTakes(true);
  return (
    <Group foot={t(shared === false ? 'next.takes.local' : 'next.takes.keep')} card="hx-takes-card">
      <TakesList rows={rows} now={now} loading={loading} onDelete={(id) => void remove(id)} />
    </Group>
  );
};
