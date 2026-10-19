// The data folder is the generator's temporary one.
import path from 'node:path';

export const dataDir = () => process.env.GOLDEN_DATA_DIR;
export const dataPath = (...parts) => path.join(process.env.GOLDEN_DATA_DIR, ...parts);
