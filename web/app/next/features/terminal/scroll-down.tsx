// "Down" over a terminal scrolled back into its history: new output does not pull the screen down while the person
// reads, and this brings it back to the live screen. It fades in and out instead of popping.
import { useEffect, useRef } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

export interface IScrollDownProps {
  shown: boolean;
  onClick: () => void;
  /** The wheel over the button goes on to the terminal under it. */
  onWheel: (event: WheelEvent) => void;
}

const ScrollDown = ({ shown, onClick, onWheel }: IScrollDownProps) => {
  const ref = useRef<HTMLButtonElement>(null);
  // Not passive: the page itself must not scroll under the terminal.
  useEffect(() => {
    const button = ref.current;
    if (!button) return;
    button.addEventListener('wheel', onWheel, { passive: false });
    return () => button.removeEventListener('wheel', onWheel);
  }, [onWheel]);
  return (
    <button
      ref={ref}
      type="button"
      className="hx-term-down"
      data-shown={shown}
      aria-hidden={!shown}
      tabIndex={shown ? 0 : -1}
      onClick={onClick}
    >
      <Icon name="arrow-down" size="sm" />
      {t('next.term.down')}
    </button>
  );
};

export default ScrollDown;
