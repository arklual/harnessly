// What the SSH way of «Машины» reads out of a failed `ssh-install` job (docs/hub/api.md, «Error codes of the ssh step»).
import type { Job } from './types';

const FINGERPRINT = /SHA256:[A-Za-z0-9+/]{43}/;

/** The server's fingerprint the hub shows for confirmation: in the detail of the `ssh` step or the job's message. */
export const hostKeyFingerprint = (job: Job): string | null => {
  const detail = job.steps.find((s) => s.id === 'ssh')?.detail ?? '';
  return FINGERPRINT.exec(detail)?.[0] ?? FINGERPRINT.exec(job.error?.message ?? '')?.[0] ?? null;
};

export type SshFailure =
  'host_key_unconfirmed' | 'host_key_mismatch' | 'ssh_blocked' | 'ssh_unreachable' | 'ssh_auth' | 'other';

export const sshFailure = (job: Job): SshFailure => {
  switch (job.error?.code) {
    case 'host_key_unconfirmed':
    case 'host_key_mismatch':
    case 'ssh_blocked':
    case 'ssh_unreachable':
    case 'ssh_auth':
      return job.error.code;
    default:
      return 'other';
  }
};
