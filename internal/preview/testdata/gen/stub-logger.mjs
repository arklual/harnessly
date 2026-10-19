const quiet = { info() {}, warn() {}, error() {}, debug() {}, child() { return quiet; } };
export const createLogger = () => quiet;
export default quiet;
