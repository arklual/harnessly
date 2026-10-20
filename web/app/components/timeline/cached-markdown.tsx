import Markdown, { type Options } from 'react-markdown';
import type { ReactElement } from 'react';

const LIMIT = 800;
const cache = new Map<string, ReactElement>();

interface ICachedMarkdownProps extends Omit<Options, 'children'> {
  /** Names the plugins, components and anything they read; the text alone does not tell two renderings apart. */
  cacheKey: string;
  children: string;
}

/**
 * Markdown parsed once per text. Opening a conversation ran remark and rehype over every message again, and
 * that was most of the time between the click and the conversation on screen. The same element also lets React skip
 * an unchanged message when the timeline re-renders.
 */
const CachedMarkdown = ({ cacheKey, children, ...options }: ICachedMarkdownProps) => {
  const key = `${cacheKey}\u0000${children}`;
  const cached = cache.get(key);
  if (cached) {
    cache.delete(key);
    cache.set(key, cached);
    return cached;
  }
  const element = Markdown({ ...options, children });
  cache.set(key, element);
  if (cache.size > LIMIT) cache.delete(cache.keys().next().value!);
  return element;
};

export default CachedMarkdown;
