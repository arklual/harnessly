import { useCallback, useState, type ReactElement } from 'react';
import Head from 'next/head';
import { useRouter } from 'next/router';
import DocViewer from '@/components/agents/docs/doc-viewer';
import useBrowserTitle from '@/hooks/use-browser-title';
import { parseVersionParam } from '@/lib/doc-versions';
import { t } from '@/lib/i18n';

/** `/docs/<slug>`: the viewer as a page of its own. The version lives in the address (`?v=<sha>`), a push opens `?thread=<id>`. */
const DocViewerPage = () => {
  const router = useRouter();
  const slug = typeof router.query.slug === 'string' ? router.query.slug : null;
  // `?focus=<id>` is accepted as the same thing as `?thread=<id>`.
  const focusQuery = router.query.thread ?? router.query.focus;
  const [title, setTitle] = useState(t('pages.docSlug.document'));
  useBrowserTitle(title);

  const changeVersion = useCallback(
    (sha: string | null) => {
      const { v: _dropped, ...rest } = router.query;
      void router.replace({ pathname: router.pathname, query: sha ? { ...rest, v: sha } : rest }, undefined, {
        shallow: true,
      });
    },
    [router],
  );
  const back = useCallback(() => {
    void router.push('/workspace');
  }, [router]);

  if (!slug) return null;

  return (
    <>
      <Head>
        <title>{title}</title>
      </Head>
      <DocViewer
        key={slug}
        slug={slug}
        focusThreadId={typeof focusQuery === 'string' ? focusQuery : null}
        versionSha={parseVersionParam(router.query.v)}
        onVersionChange={changeVersion}
        onTitle={setTitle}
        onBack={back}
      />
    </>
  );
};

DocViewerPage.getLayout = (page: ReactElement) => page;
DocViewerPage.bootstrap = true;

export default DocViewerPage;
