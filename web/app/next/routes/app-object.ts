// The object of an app in its address (`?o=`), apart from routes.ts so that a view's host reads it without the router.

/** An app's object in the address (`?o=`): a slug or an id of the app's service, no spaces or slashes. */
export const APP_OBJECT_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
