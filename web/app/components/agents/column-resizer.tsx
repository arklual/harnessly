import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';

interface IColumnResizerProps {
  side: 'left' | 'right';
  width: number;
  min: number;
  /** Upper bound; a function is resolved when dragging starts so it can follow the viewport. */
  max: number | (() => number);
  defaultWidth: number;
  cssVar: string;
  onCommit: (width: number) => void;
}

/** Drag handle for a shell column; writes a CSS variable while dragging and commits once on release. */
const ColumnResizer = ({ side, width, min, max, defaultWidth, cssVar, onCommit }: IColumnResizerProps) => {
  const apply = (value: number) =>
    document.querySelector<HTMLElement>('.tw-root')?.style.setProperty(cssVar, `${value}px`);

  const resolveMax = () => Math.max(min, typeof max === 'function' ? max() : max);

  const startDrag = (startX: number) => {
    const limit = resolveMax();
    let last = width;
    const move = (event: PointerEvent) => {
      const delta = side === 'left' ? event.clientX - startX : startX - event.clientX;
      last = Math.round(Math.min(limit, Math.max(min, width + delta)));
      apply(last);
    };
    const up = () => {
      document.body.classList.remove('tw-resizing');
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      if (last !== width) onCommit(last);
    };
    document.body.classList.add('tw-resizing');
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  };

  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={side === 'left' ? t('agents.columnResizer.listWidth') : t('agents.columnResizer.paneWidth')}
      aria-valuenow={width}
      aria-valuemin={min}
      aria-valuemax={typeof max === 'number' ? max : undefined}
      tabIndex={0}
      title={t('agents.columnResizer.hint')}
      className={cn('tw-resizer', `tw-resizer-${side}`)}
      onPointerDown={(event) => {
        event.preventDefault();
        startDrag(event.clientX);
      }}
      onDoubleClick={() => {
        apply(defaultWidth);
        onCommit(defaultWidth);
      }}
      onKeyDown={(event) => {
        if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
        event.preventDefault();
        const grow = (event.key === 'ArrowRight') === (side === 'left');
        const next = Math.min(resolveMax(), Math.max(min, width + (grow ? 16 : -16)));
        apply(next);
        onCommit(next);
      }}
    />
  );
};

export default ColumnResizer;
