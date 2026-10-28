// What a failed call says to the person. The UI decides by the error code, never by the hub's message; a screen that
// knows more handles its own codes first and falls back to this.
import { ApiError } from './api';
import { t } from './i18n';

export const errorText = (e: unknown): string => {
  if (!(e instanceof ApiError)) return t('err.internal');
  switch (e.code) {
    case 'network':
      return t('err.network');
    case 'unauthenticated':
      return t('err.unauthenticated');
    case 'forbidden':
      return t('err.forbidden');
    case 'cross_site':
      return t('err.crossSite');
    case 'signup_closed':
      return t('err.signupClosed');
    case 'machine_policy':
      return t('err.machinePolicy');
    case 'not_found':
      return t('err.notFound');
    case 'conflict':
      if (e.reason === 'login_running') return t('err.loginRunning');
      if (e.reason === 'last_method') return t('err.lastMethod');
      if (e.reason === 'last_owner') return t('err.lastOwner');
      return t('err.conflict');
    case 'account_exists':
      return t('err.accountExists');
    case 'expired':
      return t('err.expired');
    case 'provider_off':
      return t('err.providerOff');
    case 'rate_limited':
      return e.retryAfter ? t('err.rateLimitedIn', { n: e.retryAfter }) : t('err.rateLimited');
    case 'machine_offline':
      return t('err.machineOffline');
    case 'machine_error': {
      const agent = e.details.agentError;
      return typeof agent === 'string' && agent
        ? t('err.machineErrorText', { text: agent })
        : t('err.machineError');
    }
    case 'machine_timeout':
      return t('err.machineTimeout');
    case 'invalid_code':
      return t('err.invalidCode');
    case 'invalid_request':
      return t('err.invalidRequest');
    case 'internal':
      return t('err.internal');
  }
};

/** The errors of the sign-in redirect (`/signin?error=<code>`). */
export const signinRedirectError = (code: string | null): string | null => {
  switch (code) {
    case null:
    case '':
      return null;
    case 'account_exists':
      return t('err.accountExists');
    case 'expired':
      return t('auth.err.expired');
    case 'signup_closed':
      return t('err.signupClosed');
    case 'provider_off':
      return t('err.providerOff');
    default:
      return t('auth.err.oauthFailed');
  }
};
