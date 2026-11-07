// The host's building blocks the editor draws with: a window over the page. A builtin app runs in the host's
// page, so it uses the host's own dialog rather than one of its own; it reaches it here, not across the host's modules.
export { default as Modal } from '@/next/features/shell/modal';
