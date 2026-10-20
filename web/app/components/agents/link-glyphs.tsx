import { useId } from 'react';
import { cn } from '@/lib/utils';

interface IGlyphProps {
  className?: string;
}

/** Hl artifact: a page with a folded corner and a spark, in brand purple. */
export const ArtifactGlyph = ({ className }: IGlyphProps) => {
  const id = useId();
  return (
    <svg viewBox="0 0 16 16" className={cn('tw-glyph', className)} aria-hidden="true">
      <defs>
        <linearGradient id={id} x1="2" y1="1" x2="14" y2="15" gradientUnits="userSpaceOnUse">
          <stop offset="0" stopColor="#a78bfa" />
          <stop offset="1" stopColor="#6b3ff0" />
        </linearGradient>
      </defs>
      <path
        d="M4 1.25h5.6l4.15 4.15v8.1A1.25 1.25 0 0 1 12.5 14.75h-8.5A1.75 1.75 0 0 1 2.25 13V3A1.75 1.75 0 0 1 4 1.25Z"
        fill={`url(#${id})`}
      />
      <path d="M9.6 1.25v2.9c0 .69.56 1.25 1.25 1.25h2.9Z" fill="#fff" fillOpacity=".5" />
      <path
        d="M7 6.4c.3 2.05.95 2.7 3 3-2.05.3-2.7.95-3 3-.3-2.05-.95-2.7-3-3 2.05-.3 2.7-.95 3-3Z"
        fill="#fff"
      />
      <circle cx="10.6" cy="12.1" r=".75" fill="#fff" fillOpacity=".85" />
    </svg>
  );
};

/** YouTrack mark. */
export const TrackerGlyph = ({ className }: IGlyphProps) => {
  const id = useId();
  return (
    <svg viewBox="0 0 32 32" className={cn('tw-glyph', className)} aria-hidden="true">
      <defs>
        <linearGradient id={id} x1="-.05" x2="32.03" y1="16" y2="16" gradientUnits="userSpaceOnUse">
          <stop stopColor="#FB43FF" />
          <stop offset=".97" stopColor="#FB406D" />
        </linearGradient>
      </defs>
      <path
        fill={`url(#${id})`}
        d="M.653 25.622a.125.125 0 0 1-.038-.155l3.818-7.863-4.404-5.259a.125.125 0 0 1 .016-.176L12.889 1.466a6.332 6.332 0 0 1 7.942-.13 6.296 6.296 0 0 1 1.798 7.718l-1.4 2.883c.547-.183 1.087-.35 1.622-.5l6.336-1.82c.07-.02.141.023.157.093l2.653 11.793a.127.127 0 0 1-.108.153c-.84.106-5.429.765-11.14 3.165-6.472 2.718-10.742 6.581-11.347 7.146a.123.123 0 0 1-.16.007L.653 25.622Z"
      />
    </svg>
  );
};
