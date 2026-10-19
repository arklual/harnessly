export const dataDir = () => '/home/agent/.runtime';
export const dataPath = (...parts) => [dataDir(), ...parts].join('/');
