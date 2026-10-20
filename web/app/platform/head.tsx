// next/head for a client-only page: <title> sets the document title; other children are not rendered.
import { Children, isValidElement, useEffect, type ReactNode } from 'react';

const textOf = (node: ReactNode): string => {
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(textOf).join('');
  return '';
};

export default function Head({ children }: { children?: ReactNode }) {
  let title: string | null = null;
  Children.forEach(children, (child) => {
    if (isValidElement<{ children?: ReactNode }>(child) && child.type === 'title')
      title = textOf(child.props.children);
  });
  useEffect(() => {
    if (title !== null) document.title = title;
  }, [title]);
  return null;
}
