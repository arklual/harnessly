// The right pane's tab of a change (a merge request, a pull request, a commit, an issue of GitLab or GitHub) for the eye
// and for screenshots: ?dev=change&url=<link> with the installation's next-ui feature. It asks the page's own
// route for the link (/api/platform/change-view), so a browser test answers that with what it wants shown; &fake=1
// answers it here with a made-up pull request. &w=420|670|full is the pane's width, &theme=light|dark and &lang=en|ru
// pick the look. Never part of a production path.
import '@/next/styles/order';
import '@/next/dev/change-gallery.css';

import { useEffect } from 'react';
import { useRouter } from '@/platform/router';
import { pinLocale } from '@/next/i18n';
import ToolPane from '@/next/features/pane/tool-pane';
import { ChangeTab, changeTabName } from '@/next/legacy/change';
import { fakeChangeViews } from '@/next/dev/change-gallery-data';

const FALLBACK = 'https://github.com/ly-solutions/harnessly/pull/1';

const ChangeGallery = () => {
  const { query } = useRouter();
  const dark = query.theme === 'dark';
  pinLocale(query.lang === 'en' ? 'en' : 'ru');
  const url = typeof query.url === 'string' ? query.url : FALLBACK;
  if (query.fake === '1') fakeChangeViews(url);
  const width = query.w === '420' || query.w === '670' ? query.w : 'full';
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = dark ? 'dark' : 'light';
    root.classList.toggle('dark', dark);
    root.style.colorScheme = dark ? 'dark' : 'light';
  }, [dark]);
  return (
    <div className="hx-root hx-gallery-change" data-w={width}>
      <ToolPane
        tabs={[{ id: 'c', icon: 'git-pull-request', name: changeTabName(url) }]}
        tab="c"
        maxed={false}
        onPick={() => {}}
        onCloseTab={() => {}}
        onToggleMax={() => {}}
        onClose={() => {}}
      >
        <div className="hx-legacy tw-rpane-panel tw-rpane-panel-gitlab">
          <ChangeTab url={url} />
        </div>
      </ToolPane>
    </div>
  );
};

export default ChangeGallery;
