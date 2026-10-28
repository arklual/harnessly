// A file of the chat's folder as a document of the catalog. The runtime makes the document of the file and
// keeps the two bound (internal/docshare); who may open it is the gateway's, the access dialog's (`docRef(slug)`).
import { platformPath } from '@/next/core/net/machine';
import { postJson, runtimeJson } from '@/next/core/net/runtime-json';
import type { ConvId, MachineId } from '@/next/protocol/v2';

export interface IFileDoc {
  shared: boolean;
  slug?: string;
  /** A save of the file makes a new version of the document. */
  linked: boolean;
  version?: number;
  /** The file has changed since the document was made and its new version waits. */
  pending?: boolean;
  /** The newest text holds a secret on this line and is not published. */
  refusedLine?: number;
}

/** Markdown and HTML files can be shared for now (what the runtime takes). */
export const isShareable = (path: string): boolean => /\.(md|markdown|html?)$/i.test(path);

export const fileDocState = (m: MachineId, conv: ConvId, path: string, signal?: AbortSignal) =>
  runtimeJson<IFileDoc>(
    m,
    platformPath(
      `/doc-share/state?conversation=${encodeURIComponent(conv)}&path=${encodeURIComponent(path)}`,
    ),
    { signal },
  );

/** Makes the document of the file, private; a file already shared answers with its document. A refusal comes in the
 *  runtime's words (for a secret they name the line). */
export const shareFileAsDoc = (m: MachineId, conv: ConvId, path: string, signal?: AbortSignal) =>
  runtimeJson<IFileDoc>(m, platformPath('/doc-share/publish'), {
    ...postJson({ conversation: conv, path }),
    signal,
  });

/** Freezes the document: the file's later saves do not reach it. */
export const unlinkFileDoc = (m: MachineId, conv: ConvId, path: string) =>
  runtimeJson<IFileDoc>(m, platformPath('/doc-share/unlink'), postJson({ conversation: conv, path }));
