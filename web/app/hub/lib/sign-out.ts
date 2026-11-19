import { useApi } from './env';
import { navigate } from './router';
import { useSession } from './session';

/** Signs the person out and leads them to `/signin`. */
export const useSignOut = () => {
  const api = useApi();
  const { clear } = useSession();
  return () => {
    void api.auth.logout().finally(() => {
      clear();
      navigate('/signin', { replace: true });
    });
  };
};
