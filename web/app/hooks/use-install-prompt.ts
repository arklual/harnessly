import { create } from 'zustand';

/** The install offer Chromium browsers make once the page qualifies; Safari has none and shows the Home Screen steps instead. */
interface IInstallPromptEvent extends Event {
  prompt: () => Promise<void>;
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>;
}

interface IInstallStore {
  offer: IInstallPromptEvent | null;
}

export const useInstallStore = create<IInstallStore>(() => ({ offer: null }));

/** The browser fires `beforeinstallprompt` once, early: the event is kept until the person asks for it. */
export const captureInstallPrompt = (): (() => void) => {
  const keep = (event: Event) => {
    event.preventDefault();
    useInstallStore.setState({ offer: event as IInstallPromptEvent });
  };
  const forget = () => useInstallStore.setState({ offer: null });
  window.addEventListener('beforeinstallprompt', keep);
  window.addEventListener('appinstalled', forget);
  return () => {
    window.removeEventListener('beforeinstallprompt', keep);
    window.removeEventListener('appinstalled', forget);
  };
};

export const installApp = async (): Promise<boolean> => {
  const { offer } = useInstallStore.getState();
  if (!offer) return false;
  await offer.prompt();
  const { outcome } = await offer.userChoice;
  useInstallStore.setState({ offer: null });
  return outcome === 'accepted';
};
