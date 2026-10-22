import usePushBridge from '@/hooks/use-push-bridge';

/** Nothing to see: keeps the page and its service worker in step (see `usePushBridge`). */
const PushBridge = () => {
  usePushBridge();
  return null;
};

export default PushBridge;
