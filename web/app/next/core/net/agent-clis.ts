// On a page open the runtime of each machine is told to look at its agent CLIs and model lists: the
// runtime checks for a new Claude Code or Codex at most once in half an hour and refreshes the lists of models, in
// the background; the page waits for nothing and reads no answer.
import { sendBody } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;

export function pokeAgentCLIs(machine: MachineId): void {
  void sendBody(
    'POST',
    runtimePath(platformPath('/agents/refresh'), machine),
    {},
    () => null,
    TIMEOUT_MS,
  ).catch(() => undefined);
}
