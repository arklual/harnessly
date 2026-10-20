import { useState } from 'react';
import { FileText, Globe2, Mail } from 'lucide-react';
import { SiClaude, SiGithub, SiGitlab } from 'react-icons/si';
import { ArtifactGlyph, TrackerGlyph } from '@/components/agents/link-glyphs';
import { scopedAppPath } from '@/lib/app-path';
import { presentLink } from '@/lib/presented-link';
import { cn } from '@/lib/utils';

const hostOf = (href: string) => {
  try {
    return new URL(href).hostname;
  } catch {
    return '';
  }
};

const Fallback = ({ kind, className }: { kind: string; className?: string }) => {
  if (kind === 'artifact') return <ArtifactGlyph className={className} />;
  if (kind === 'issue') return <TrackerGlyph className={className} />;
  if (kind === 'mr' || kind === 'gitlab')
    return <SiGitlab className={cn(className, 'tw-site-gitlab')} aria-hidden="true" />;
  if (kind === 'claude-artifact')
    return <SiClaude className={cn(className, 'tw-site-claude')} aria-hidden="true" />;
  if (kind === 'github') return <SiGithub className={className} aria-hidden="true" />;
  if (kind === 'mail') return <Mail className={className} aria-hidden="true" />;
  if (kind === 'file') return <FileText className={className} aria-hidden="true" />;
  return <Globe2 className={className} aria-hidden="true" />;
};

/** The site's own favicon, resolved server-side; a drawn glyph only when the site has none. */
const SiteIcon = ({ href, className }: { href: string; className?: string }) => {
  const [failed, setFailed] = useState(false);
  const { kind } = presentLink(href);
  const host = hostOf(href);
  if (!host || failed) return <Fallback kind={kind} className={className} />;
  return (
    <img
      src={scopedAppPath(`/api/platform/favicon?host=${encodeURIComponent(host)}`)}
      alt=""
      className={cn(className, 'tw-site-favicon')}
      loading="lazy"
      onError={() => setFailed(true)}
    />
  );
};

export default SiteIcon;
