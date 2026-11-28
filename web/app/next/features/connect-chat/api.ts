// «Где работает» of one chat: on this machine or on one of the person's computers through Harnessly Connect. The
// runtime knows the computers, checks the folder on the one picked and starts the chat's agent there once its turn is
// over; the page only asks and shows.
import { runtimePath } from '@/next/core/net/machine';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';

export interface IConnectDevice {
  id: string;
  name: string;
  os: string;
  online: boolean;
  /** A folder on it that looks like the chat's project. */
  suggest?: string;
}

export interface IConnectChat {
  /** The computer the chat was switched to; null: it runs here. */
  on: { device: string; dir: string } | null;
  /** The chat's own folder is one of a computer: it runs there whatever is picked. */
  folder?: boolean;
  /** The chat's folder on this machine, where it goes back to. */
  cwd: string;
  claude: boolean;
  busy: boolean;
  devices: readonly IConnectDevice[] | null;
}

const answer = async <T>(res: Response): Promise<T> => {
  const body = (await res.json().catch(() => null)) as (T & { error?: string }) | null;
  if (!res.ok) throw new Error(body?.error || t('next.connectChat.failed'));
  return body as T;
};

/** Where the chat runs, and the computers it could run on; null when this machine has no Connect. */
export const fetchConnectChat = async (m: MachineId, conv: string): Promise<IConnectChat | null> => {
  const res = await fetch(runtimePath(`/api/cli/connect/chat?conv=${encodeURIComponent(conv)}`, m), {
    credentials: 'same-origin',
  });
  if (res.status === 404 && !res.headers.get('content-type')?.includes('json')) return null;
  return answer<IConnectChat>(res);
};

/** Switches the chat to a computer's folder, or back here (device null). */
export const postConnectChat = async (
  m: MachineId,
  conv: string,
  to: { device: string; dir: string } | null,
): Promise<void> => {
  const res = await fetch(runtimePath('/api/cli/connect/chat', m), {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ conv, device: to?.device ?? '', dir: to?.dir ?? '' }),
  });
  await answer<{ ok: boolean }>(res);
};
