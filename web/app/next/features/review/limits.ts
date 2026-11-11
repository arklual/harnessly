// How much the review screen shows at once. A file with more than ~5000 changed lines or ~1 MB of patch or content is a
// placeholder with «show anyway»; a change of more than 300 files shows its tree folded, to be searched rather than
// scrolled.
import type { IFileContent, IReviewFile } from '@/next/features/review/model';

export const REVIEW_LIMITS = {
  fileDiffLines: 5000,
  fileBytes: 1_000_000,
  manyFiles: 300,
} as const;

/** A file too heavy to draw without being asked. */
export const isHeavyFile = (file: IReviewFile): boolean =>
  file.additions + file.deletions > REVIEW_LIMITS.fileDiffLines ||
  file.patch.length > REVIEW_LIMITS.fileBytes;

/** Whole versions too big to draw without being asked. */
export const isHeavyContent = (content: IFileContent): boolean =>
  content.before.length > REVIEW_LIMITS.fileBytes || content.after.length > REVIEW_LIMITS.fileBytes;

/** Whether the tree opens all its folders by itself. */
export const autoExpandTree = (files: number): boolean => files <= REVIEW_LIMITS.manyFiles;
