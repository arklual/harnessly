// The design's place while the app does not yet know which machine opens it (the machines are still answering): the
// stage with the design loader, as SiteView shows it, so an address opened from a link is never an empty area.
import SiteLoader from '@/next/features/designly/site-loader';
import { LOAD_STAGE } from '@/next/features/designly/site-load';
import '@/next/styles/site.css';

const SiteWait = () => (
  <div className="hx-site-wait">
    <SiteLoader stage={LOAD_STAGE.opening} />
  </div>
);

export default SiteWait;
