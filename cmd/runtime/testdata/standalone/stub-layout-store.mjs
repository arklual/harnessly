// The workspace folders, as the layout store names them; the store itself is not needed.
import path from 'node:path';
import { dataDir } from '@/lib/data-dir';

export const resolveLayoutDir = (wsId) => path.join(dataDir(), 'workspaces', wsId);
