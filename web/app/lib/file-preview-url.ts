import { scopedAppPath } from '@/lib/app-path';

export const filePreviewUrl = (path: string) =>
  scopedAppPath(`/api/platform/file-preview?path=${encodeURIComponent(path)}`);

export const fileDownloadUrl = (path: string) =>
  scopedAppPath(`/api/platform/file-preview?download=1&path=${encodeURIComponent(path)}`);
