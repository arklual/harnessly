// The colours of the chart's lines, in the order of the legend: violet, teal, amber, grey, rose. The legend dots in
// the CSS (an-legend__dot) use the same hues.
const PALETTE_LIGHT = ['#6d49f4', '#00a2a3', '#cf7d00', '#8a8a96', '#ec305a'];
const PALETTE_DARK = ['#7a5aff', '#2bc4c5', '#e8a02a', '#9a9aa6', '#ff5b7f'];

export const lineColors = (dark: boolean): string[] => (dark ? PALETTE_DARK : PALETTE_LIGHT);
