// Runs before the rest of the guest page loads: the texts read the page's language once, from <meta name="hl-config">,
// and the page may carry no inline script (the shell is served under a strict CSP), so the language is set here.
const lang = (navigator.language || 'en').toLowerCase().startsWith('ru') ? 'ru' : 'en';
document.querySelector('meta[name="hl-config"]')?.setAttribute('content', JSON.stringify({ locale: lang }));
document.documentElement.lang = lang;
