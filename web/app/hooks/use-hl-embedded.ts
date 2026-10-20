import { useRouter } from 'next/router';

/** True inside the unified Hl workspace, which hosts agent views without the classic terminal chrome. */
const useHlEmbedded = (): boolean => {
  const router = useRouter();
  return router.pathname === '/workspace' || router.query.embedded === '1';
};

export default useHlEmbedded;
