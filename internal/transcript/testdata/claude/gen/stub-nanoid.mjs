// Ids in the order they are drawn, from a counter the generator resets per case: id0001, id0002, ...
export const nanoid = (size = 21) => {
  globalThis.__ids = (globalThis.__ids ?? 0) + 1;
  return `id${globalThis.__ids.toString(36).padStart(4, '0')}`.slice(-size);
};
