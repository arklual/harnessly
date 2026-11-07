import type { IFolder, ITrashItem } from '@/next/features/designly/api';

/** What the library screen is given to lay projects out and to delete them; every call resolves once the machine did. */
export interface ILibraryOrganizer {
  folders: readonly IFolder[];
  onFolderSave: (input: { id?: string; name: string; parent?: string }) => Promise<void>;
  onFolderDelete: (id: string, projects: 'keep' | 'trash') => Promise<void>;
  /** Lays projects in a folder; '' is the top. */
  onMove: (ids: string[], folder: string) => Promise<void>;
  /** Sends projects to the trash, with a notice that takes them back. */
  onDeleteMany: (ids: string[]) => Promise<void>;
  /** The trash: null while it loads. */
  trash: readonly ITrashItem[] | null;
  loadTrash: () => Promise<void>;
  onRestore: (id: string) => Promise<void>;
  onPurge: (id: string) => Promise<void>;
}
