export const meta = {
  name: 'checkout-review',
  description: 'Review the checkout flow, then fix what the reviewers found',
  phases: [
    { title: 'Review', detail: 'three reviewers in parallel' },
    { title: "Fix", detail: 'one fixer per finding' },
    { title: `Verify`, detail: 'tests and a last look' },
  ],
}

export default async function run({ agent, phase }) {
  phase('Review')
  const found = await Promise.all(['payments', 'forms', 'copy'].map((k) => agent(`review ${k}`, { label: 'review:' + k })))
  phase('Fix')
  return found
}
