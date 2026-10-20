import { create } from 'zustand';

/**
 * Files still on their way to the machine, by the local preview URL the composer made for them: the chip in
 * the composer and the picture in the sent message read the same mark, so a message goes out at once and its pictures
 * show a loader until the upload is done.
 */
interface IUploadProgress {
  uploading: Record<string, true>;
}

export const useUploadProgress = create<IUploadProgress>(() => ({ uploading: {} }));

export const markUploading = (url: string, on: boolean): void => {
  useUploadProgress.setState((state) => {
    if (on === url in state.uploading) return state;
    const next = { ...state.uploading };
    if (on) next[url] = true;
    else delete next[url];
    return { uploading: next };
  });
};

export const useIsUploading = (url: string): boolean => useUploadProgress((state) => url in state.uploading);
