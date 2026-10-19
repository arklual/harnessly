import { Spinner } from '@kit';

import { Logo } from './logo';

export function Splash({ message }: { message?: string }) {
  return (
    <div className="splash">
      <Logo size={40} />
      <Spinner size="m" label="Loading" />
      {message && <p className="splash__text">{message}</p>}
    </div>
  );
}
