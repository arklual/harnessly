// Subscriptions and sending: the golden's subscriptions, each endpoint answering the status it names.
const g = () => globalThis.__golden;
export const getSubscriptions = async () => g().subscriptions;
export const removeSubscription = async (endpoint) => { g().removed.push(endpoint); };
export const selectPhoneTargets = (subs, senderDeviceId) => {
  const others = subs.filter((s) => !senderDeviceId || s.deviceId !== senderDeviceId);
  const phones = others.filter((s) => s.mobile === true);
  return phones.length ? phones : others.filter((s) => s.mobile === undefined);
};
export const sendPush = async (sub, message) => { g().pushed.push({ endpoint: sub.endpoint, message }); return g().statuses[sub.endpoint] ?? 201; };
