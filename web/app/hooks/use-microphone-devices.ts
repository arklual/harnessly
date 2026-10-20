import { useCallback, useEffect, useState } from 'react';
import { t } from '@/lib/i18n';

export interface IMicrophoneDevice {
  deviceId: string;
  label: string;
}

const listInputs = async (): Promise<IMicrophoneDevice[]> => {
  if (typeof navigator === 'undefined' || !navigator.mediaDevices?.enumerateDevices) return [];
  const devices = await navigator.mediaDevices.enumerateDevices();
  return devices
    .filter((device) => device.kind === 'audioinput')
    .map((device, index) => ({
      deviceId: device.deviceId,
      label: device.label || t('hooks.useMicrophoneDevices.microphone', { index: index + 1 }),
    }));
};

/** Mic list for the composer's device dropdown; labels stay generic until mic permission is granted once. */
const useMicrophoneDevices = (): { devices: IMicrophoneDevice[]; refresh: () => void } => {
  const [devices, setDevices] = useState<IMicrophoneDevice[]>([]);

  const refresh = useCallback(() => {
    listInputs()
      .then(setDevices)
      .catch(() => setDevices([]));
  }, []);

  useEffect(() => {
    refresh();
    if (typeof navigator === 'undefined' || !navigator.mediaDevices) return;
    navigator.mediaDevices.addEventListener('devicechange', refresh);
    return () => navigator.mediaDevices.removeEventListener('devicechange', refresh);
  }, [refresh]);

  return { devices, refresh };
};

export default useMicrophoneDevices;
