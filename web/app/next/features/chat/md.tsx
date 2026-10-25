// The agent's text in the feed. Markdown arrives in its own chunk; until it does, the same text shows as plain
// paragraphs in the same box, so nothing jumps or spins while it loads. The pictures the text names stand under it.
import { lazy, memo, Suspense } from 'react';
import { PictureCards, useNamedPictures, withoutPaths } from '@/next/features/chat/pictures';

const Markdown = lazy(() => import('@/next/features/chat/markdown'));

const Plain = ({ md }: { md: string }) => (
  <div className="hx-md" data-plain="true">
    {md.split(/\n{2,}/).map((part, index) => (
      <p key={index}>{part}</p>
    ))}
  </div>
);

/** `paths`: paths in inline code open as files, for another agent's report. A picture's path is not repeated above it. */
const Md = ({ md, paths = false }: { md: string; paths?: boolean }) => {
  const pictures = useNamedPictures(md);
  const text = pictures.named.length > 0 ? withoutPaths(md, pictures.named) : md;
  return (
    <>
      <Suspense fallback={<Plain md={text} />}>
        <Markdown md={text} paths={paths} />
      </Suspense>
      <PictureCards pictures={pictures.named} srcOf={pictures.srcOf} />
    </>
  );
};

export default memo(Md);
