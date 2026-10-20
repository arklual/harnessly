import { nanoid } from 'nanoid';

const DEVICE_ID_KEY = 'hl-device-id';

/** One id per browser profile: the server tells the device a person is looking at from the others. */
export const getDeviceId = (): string => {
  try {
    let id = localStorage.getItem(DEVICE_ID_KEY);
    if (!id) id = nanoid();
    localStorage.setItem(DEVICE_ID_KEY, id);
    return id;
  } catch {
    return 'volatile';
  }
};

/** A phone or a tablet; iPadOS asks for the desktop site and reports itself as a Mac with a touch screen. */
export const isMobileDevice = (): boolean =>
  /Android|iPhone|iPad|iPod|Mobi/i.test(navigator.userAgent) ||
  (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
