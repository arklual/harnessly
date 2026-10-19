import type { CSSProperties, HTMLAttributes } from 'react';

import { cx } from './cx';

/** Steps of the spacing scale (--k-space-N). */
export type Space = 0 | 1 | 2 | 3 | 4 | 5 | 6 | 8 | 10 | 12;

const ALIGN = {
  start: 'flex-start',
  center: 'center',
  end: 'flex-end',
  stretch: 'stretch',
  baseline: 'baseline',
} as const;
const JUSTIFY = { start: 'flex-start', center: 'center', end: 'flex-end', between: 'space-between' } as const;

export interface FlexProps extends HTMLAttributes<HTMLDivElement> {
  direction?: 'row' | 'column';
  gap?: Space;
  align?: keyof typeof ALIGN;
  justify?: keyof typeof JUSTIFY;
  wrap?: boolean;
}

/** A flex box whose gap is a step of the spacing scale. */
export function Flex({
  direction = 'row',
  gap = 0,
  align,
  justify,
  wrap,
  className,
  style,
  ...rest
}: FlexProps) {
  const layout: CSSProperties = {
    flexDirection: direction,
    gap: gap ? `var(--k-space-${gap})` : undefined,
    alignItems: align && ALIGN[align],
    justifyContent: justify && JUSTIFY[justify],
    flexWrap: wrap ? 'wrap' : undefined,
  };
  return <div {...rest} className={cx('k-flex', className)} style={{ ...layout, ...style }} />;
}

/** Children one under another. */
export function Stack(props: Omit<FlexProps, 'direction'>) {
  return <Flex gap={4} {...props} direction="column" />;
}

/** Children side by side, centred across, wrapping onto new lines. */
export function Row(props: Omit<FlexProps, 'direction'>) {
  return <Flex gap={2} align="center" wrap {...props} direction="row" />;
}
