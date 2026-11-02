// What is selected in the file open beside a chat, for the chat's field to show as a chip and send with its message. The
// view reports it; the field takes it away once it is sent or dismissed, and the next selection brings a new one.
import { createKeyedStore, useKey } from '@/next/core/store';
import type { IFileSelection } from '@/next/features/pane/file-selection';
import type { ConvId } from '@/next/protocol/v2';

export interface IFileSelectionChip {
  path: string;
  selection: IFileSelection;
}

export const fileSelectionStore = createKeyedStore<ConvId, IFileSelectionChip>();

export const useFileSelectionChip = (conv: ConvId | null): IFileSelectionChip | null =>
  useKey(fileSelectionStore, conv) ?? null;

export const putFileSelectionChip = (conv: ConvId, chip: IFileSelectionChip | null): void =>
  fileSelectionStore.patch(conv, () => chip ?? undefined);
