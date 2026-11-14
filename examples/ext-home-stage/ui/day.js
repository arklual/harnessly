// The page of the day on the whole start page: the greeting over the field is its own, the rest of the stage is its list.
const FOCUS = ['Answer the review of the billing change', 'Ship the release notes', 'Call the venue about Friday'];
const MEETINGS = ['10:00 Stand-up', '13:30 Design review', '17:00 One-to-one'];

const fill = (id, items) => {
  const list = document.getElementById(id);
  for (const text of items) {
    const li = document.createElement('li');
    li.textContent = text;
    list.append(li);
  }
};

harnessly.connect().then(async (hl) => {
  fill('focus', FOCUS);
  fill('meetings', MEETINGS);
  await hl.slot.greet({ title: 'Three things for today', sub: 'Two meetings, one review waits for you' });
});
