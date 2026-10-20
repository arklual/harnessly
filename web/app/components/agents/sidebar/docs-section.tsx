import { useRouter } from 'next/router';
import { FileText, Shapes } from 'lucide-react';
import SidebarSection from '@/components/agents/sidebar/sidebar-section';
import { TwBadge } from '@/components/agents/ui';
import useDocList from '@/hooks/use-doc-list';
import { t } from '@/lib/i18n';

/** Documents and boards the person owns or wrote in; hidden when there are none. */
const DocsSection = () => {
  const router = useRouter();
  const { docs } = useDocList();

  if (!docs || docs.length === 0) return null;

  return (
    <SidebarSection section="docs" title={t('sidebar.docsSection.title')}>
      {docs.map((doc) => (
        <button
          key={doc.slug}
          type="button"
          className="tw-row"
          onClick={() => router.push(`/docs/${doc.slug}`)}
        >
          <span className="tw-srow-lead">
            {doc.kind === 'board' ? <Shapes aria-hidden="true" /> : <FileText aria-hidden="true" />}
          </span>
          <span className="tw-row-title" title={doc.title}>
            {doc.title}
          </span>
          {(doc.unread > 0 || doc.openThreads > 0) && (
            <TwBadge
              className="td-sidebar-row-unread"
              tone={doc.unread > 0 ? 'accent' : 'neutral'}
              title={
                doc.unread > 0 ? t('sidebar.docsSection.newComments') : t('sidebar.docsSection.openThreads')
              }
            >
              {doc.unread > 0 ? doc.unread : doc.openThreads}
            </TwBadge>
          )}
        </button>
      ))}
    </SidebarSection>
  );
};

export default DocsSection;
