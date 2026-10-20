import { Fragment } from 'react';
import MessageLink from '@/components/timeline/message-link';
import { issueKeyRe, issueUrl, useTrackerKeys } from '@/lib/tracker-keys';

/** Plain text (a commit or MR title) with its task keys shown as live task chips. */
const IssueKeyText = ({ text }: { text: string }) => {
  useTrackerKeys();
  const parts: (string | { key: string })[] = [];
  let last = 0;
  for (const match of text.matchAll(issueKeyRe())) {
    if (match.index > last) parts.push(text.slice(last, match.index));
    parts.push({ key: match[1] });
    last = match.index + match[0].length;
  }
  if (last < text.length) parts.push(text.slice(last));
  return (
    <>
      {parts.map((part, i) =>
        typeof part === 'string' ? (
          <Fragment key={i}>{part}</Fragment>
        ) : (
          <MessageLink key={i} href={issueUrl(part.key)}>
            {part.key}
          </MessageLink>
        ),
      )}
    </>
  );
};

export default IssueKeyText;
