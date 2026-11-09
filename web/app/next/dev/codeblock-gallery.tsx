// Code blocks of the agent's text at the widths a chat gives them, for the eye and for screenshots: ?dev=codeblock.
// A short and a long one-line command, a long block, on the desktop column, a narrow one and a phone's. All the
// interface's stylesheets in their order, as in the product: a class another feature defines shows up here too.
// ?theme=light|dark&lang=en|ru pick the look. Never part of a production path.
import '@/next/styles/order';
import '@/next/dev/codeblock-gallery.css';

import { useEffect } from 'react';
import { useRouter } from '@/platform/router';
import { pinLocale } from '@/next/i18n';
import Markdown from '@/next/features/chat/markdown';

const WIDTHS = ['desktop', 'column', 'narrow'] as const;

const BLOCKS = [
  '```\n! bash /tmp/x.sh\n```',
  '```bash\n! cd ~/projects/app && source ~/.config/app/github.env && git push github 9cbbf8044599223208aa9230653d643561e23f19:refs/tags/v0.16.196\n```',
  '```go\n' + Array.from({ length: 18 }, (_, i) => `fmt.Println("line ${i + 1}")`).join('\n') + '\n```',
] as const;

const CodeblockGallery = () => {
  const { query } = useRouter();
  const theme = query.theme === 'dark' ? 'dark' : 'light';
  pinLocale(query.lang === 'en' ? 'en' : 'ru');
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = theme;
    root.classList.toggle('dark', theme === 'dark');
    root.style.colorScheme = theme;
  }, [theme]);
  return (
    <div className="hx-root hx-codeblocks">
      {WIDTHS.map((width) => (
        <section key={width} className="hx-codeblocks__col" data-width={width}>
          <h2 className="hx-codeblocks__ttl">{width}</h2>
          {BLOCKS.map((md) => (
            <Markdown key={md} md={`Нужна твоя команда:\n\n${md}`} />
          ))}
        </section>
      ))}
    </div>
  );
};

export default CodeblockGallery;
