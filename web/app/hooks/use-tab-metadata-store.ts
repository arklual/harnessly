// What the page knows about a terminal tab beyond the layout: its current directory.
import { create } from 'zustand';

interface ITabMetadata {
  cwd?: string;
  title?: string;
}

interface ITabMetadataStore {
  metadata: Record<string, ITabMetadata>;
  set: (tabId: string, meta: ITabMetadata) => void;
}

const useTabMetadataStore = create<ITabMetadataStore>((set) => ({
  metadata: {},
  set: (tabId, meta) =>
    set((s) => ({ metadata: { ...s.metadata, [tabId]: { ...s.metadata[tabId], ...meta } } })),
}));

export default useTabMetadataStore;
