// One way to save files of a chat: the runtime's .../download sends a file as it is on disk (in ranges, so the
// browser resumes it), a folder or several paths as one zip made while it is sent. Paths are the chat folder's
// (relative) or absolute ones the chat touched; the runtime keeps secrets and what lies outside out.
import { machineApi } from '@/next/core/net/machine';
import type { ConvId, MachineId } from '@/next/protocol/v2';

/** Where the files are saved from: one path is a file or a folder's zip; several are one zip, named `name`. */
export const downloadHref = (m: MachineId, conv: ConvId, paths: readonly string[], name?: string) => {
  const query = paths.map((p) => `path=${encodeURIComponent(p)}`).join('&');
  const named = name ? `&name=${encodeURIComponent(name)}` : '';
  return machineApi(m).http(`/conversations/${encodeURIComponent(conv)}/download?${query}${named}`);
};

/** Starts saving what the address sends: the browser's own download, no page left. */
export const saveFrom = (href: string) => {
  const a = document.createElement('a');
  a.href = href;
  a.download = '';
  a.rel = 'noopener';
  document.body.append(a);
  a.click();
  a.remove();
};
