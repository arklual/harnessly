// A video's comments for the list, the composer and the timeline's markers: the team's from the video state, the guests'
// read from the gateway (deck/guest-comments.ts, shared with the deck), merged by ./comments-model.ts.
// Adding and resolving are pessimistic: the list changes with the runtime's answer, not before it.
import { useMemo } from 'react';
import { RuntimeError } from '@/next/core/net/runtime-json';
import { useGuestComments } from '@/next/features/designly/deck/guest-comments';
import { addVideoComment, resolveVideoComment } from '@/next/features/designly/video/api';
import {
  mergeVideoComments,
  unresolvedCount,
  type IVideoCommentItem,
  type TGuestVideoComment,
} from '@/next/features/designly/video/comments-model';
import type { IVideoCommentDraft, IVideoState } from '@/next/features/designly/video/types';
import { putVideoComment, putVideoGuestResolved } from '@/next/stores/video';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { toast } from '@/next/features/designly/sdk/toast';

export interface IVideoComments {
  /** The merged list: the team's show at once, the guests' join when read. */
  items: readonly IVideoCommentItem[];
  /** Unresolved. */
  open: number;
  /** Flips the resolved state; true when the runtime took it. */
  resolve: (item: IVideoCommentItem) => Promise<boolean>;
  /** Adds a team comment; true when the runtime took it. */
  add: (draft: IVideoCommentDraft) => Promise<boolean>;
}

const failed = (failure: unknown) =>
  toast(failure instanceof RuntimeError ? failure.message : t('next.failure.http'), { tone: 'error' });

export const useVideoComments = (
  machine: MachineId,
  artifact: string,
  video: IVideoState,
): IVideoComments => {
  const guests = useGuestComments(artifact);
  const items = useMemo(
    () =>
      mergeVideoComments(
        video.scenes,
        video.comments,
        guests as readonly TGuestVideoComment[] | null,
        video.guestResolved,
      ),
    [video.scenes, video.comments, video.guestResolved, guests],
  );

  const resolve = async (item: IVideoCommentItem) => {
    try {
      const done = await resolveVideoComment(machine, artifact, item.key, !item.resolved);
      if (item.guest) putVideoGuestResolved(artifact, item.key, done.resolved);
      else if (done.comment) putVideoComment(artifact, done.comment);
      return true;
    } catch (failure) {
      failed(failure);
      return false;
    }
  };

  const add = async (draft: IVideoCommentDraft) => {
    try {
      putVideoComment(artifact, await addVideoComment(machine, artifact, draft));
      return true;
    } catch (failure) {
      failed(failure);
      return false;
    }
  };

  return { items, open: unresolvedCount(items), resolve, add };
};
