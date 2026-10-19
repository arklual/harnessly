// Ids in the order they are asked for: id0001, id0002, … (the Go test asks in the same order).
let n = 0;
export const nanoid = (size = 21) => {
  n += 1;
  return `id${n.toString(36).padStart(4, '0')}`.slice(-size);
};
