// Shapes of what the runtime answers; the runtime itself is Go.
import type { IArtifactLink } from '@/lib/hl-artifacts';

/**
 * Links, artifacts, boards, merge requests and work paths of a whole conversation, read from its transcript
 * on the runtime instead of from the part of the timeline a browser happened to load. The index is kept per file and
 * grows with the file: a request reads only the bytes appended since the previous one.
 */

export interface IConversationSources {
  links: IArtifactLink[];
  workPaths: string[];
  /** Files its tools wrote or edited, with the time of the latest write (ms). */
  editedPaths: { path: string; at: number }[];
  /** Bytes of the transcript the answer covers; the client refetches when the timeline moves past it. */
  size: number;
}
