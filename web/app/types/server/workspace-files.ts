// Shapes of what the runtime answers; the runtime itself is Go.

export interface WorkspaceFileEntry {
  name: string;
  path: string;
  kind: 'directory' | 'file';
}

export interface WorkspaceFile {
  path: string;
  content: string;
  revision: string;
  bytes: number;
}
