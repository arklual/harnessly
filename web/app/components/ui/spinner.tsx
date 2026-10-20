import { Spinner as KitSpinner } from '@kit/spinner';

export default function Spinner({ className }: { className?: string; size?: number }) {
  return <KitSpinner className={className} />;
}
