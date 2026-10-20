export const DUPLICATE_SEND_MS = 3000;

export interface ILastSend {
  text: string;
  at: number;
}

export const isDuplicateSend = (last: ILastSend | null, text: string, now: number): boolean =>
  last !== null && last.text === text.trim() && now - last.at <= DUPLICATE_SEND_MS;
