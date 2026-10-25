// A markdown file rendered with the app's typography (.hx-md): tables, task lists and strikethrough of GFM; raw HTML is
// never rendered, pictures are named by their text, links open in a tab of their own.
import renderTree, { type Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';

const components = {
  a: ({ href, children }) => (
    <a href={href} target="_blank" rel="noreferrer noopener">
      {children}
    </a>
  ),
  img: ({ alt }) => <span>{alt}</span>,
  code: ({ className, children }) => <code className={className ?? 'hx-md__code'}>{children}</code>,
  pre: ({ children }) => <pre className="hx-md__pre">{children}</pre>,
  table: ({ children }) => (
    <div className="hx-md__table">
      <table>{children}</table>
    </div>
  ),
} as Components;

const PLUGINS = [remarkGfm];

const MdPreview = ({ md }: { md: string }) => (
  <div className="hx-md hx-fed__doc">
    {renderTree({ children: md, remarkPlugins: PLUGINS, components, skipHtml: true })}
  </div>
);

export default MdPreview;
