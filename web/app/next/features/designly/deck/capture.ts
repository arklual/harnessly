// The camera and the microphone for a recording. With the camera refused or missing the recording is made with the
// microphone alone, which the runtime is told (`camera: false`); with no microphone there is nothing to record.

import type { IRecorderLike } from '@/next/features/designly/deck/recorder';

export interface ICapture {
  stream: MediaStream;
  camera: boolean;
}

export const canCapture = (): boolean =>
  typeof MediaRecorder !== 'undefined' && !!navigator.mediaDevices?.getUserMedia;

const AUDIO: MediaTrackConstraints = { echoCancellation: true, noiseSuppression: true };

export const openCapture = async (): Promise<ICapture> => {
  try {
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: AUDIO,
      video: { width: { ideal: 1280 }, height: { ideal: 720 }, facingMode: 'user' },
    });
    return { stream, camera: true };
  } catch {
    // no camera, or it was refused: the voice alone still makes a talk; no microphone throws to the caller
    const stream = await navigator.mediaDevices.getUserMedia({ audio: AUDIO });
    return { stream, camera: false };
  }
};

export const stopStream = (stream: MediaStream | null): void =>
  stream?.getTracks().forEach((track) => track.stop());

/** A BlobEvent is an `{ data }` and more; the recorder code reads only the data, so it is typed as the narrower. */
export const makeRecorder = (stream: MediaStream, mime: string): IRecorderLike =>
  new MediaRecorder(stream, mime ? { mimeType: mime } : undefined) as unknown as IRecorderLike;
