import { useHotkeys } from 'react-hotkeys-hook';
import { useResolvedKey } from '@/hooks/use-keybindings-store';

/** Push-to-talk: held key starts recording, releasing it stops — `app.voice_hold` in the shortcuts UI. */
const useVoiceHoldHotkey = (onPress: () => void, onRelease: () => void, enabled: boolean) => {
  const key = useResolvedKey('app.voice_hold');
  useHotkeys(
    key ?? '',
    (event) => {
      if (event.repeat) return;
      if (event.type === 'keydown') onPress();
      else onRelease();
    },
    {
      keydown: true,
      keyup: true,
      preventDefault: true,
      enableOnFormTags: true,
      enabled: enabled && !!key,
    },
    [onPress, onRelease, enabled, key],
  );
};

export default useVoiceHoldHotkey;
