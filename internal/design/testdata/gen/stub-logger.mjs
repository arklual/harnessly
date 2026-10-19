const log = { trace() {}, debug() {}, info() {}, warn(...a) { if (process.env.GOLDEN_DEBUG) console.error(...a); }, error(...a) { if (process.env.GOLDEN_DEBUG) console.error(...a); }, child() { return log; } };
export const createLogger = () => log;
export default log;
