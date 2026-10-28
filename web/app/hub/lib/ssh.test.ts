import { describe, expect, it } from 'vitest';

import { hostKeyFingerprint, sshFailure } from './ssh';
import type { Job } from './types';

const FP = 'SHA256:' + 'a'.repeat(43);
const job = (code: string | null, detail: string | null, message = ''): Job => ({
  id: 'j1',
  kind: 'ssh-install',
  provider: null,
  state: 'failed',
  steps: [{ id: 'ssh', state: 'fail', detail }],
  error: code ? { code, message } : null,
  check: null,
});

describe('ssh failures', () => {
  it('reads the fingerprint from the step detail, else from the message', () => {
    expect(hostKeyFingerprint(job('host_key_unconfirmed', `ssh-ed25519 ${FP}`))).toBe(FP);
    expect(hostKeyFingerprint(job('host_key_unconfirmed', null, `key ${FP}`))).toBe(FP);
    expect(hostKeyFingerprint(job('host_key_unconfirmed', 'nothing here'))).toBeNull();
  });

  it('tells the codes of the ssh step apart', () => {
    for (const code of [
      'host_key_unconfirmed',
      'host_key_mismatch',
      'ssh_blocked',
      'ssh_unreachable',
      'ssh_auth',
    ] as const) {
      expect(sshFailure(job(code, null))).toBe(code);
    }
    expect(sshFailure(job('internal', null))).toBe('other');
    expect(sshFailure(job(null, null))).toBe('other');
  });
});
