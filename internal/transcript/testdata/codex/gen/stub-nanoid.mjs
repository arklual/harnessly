let n = 0;
export const nanoid = () => `id${String(++n).padStart(4, '0')}`;
export const resetIds = () => {
  n = 0;
};
