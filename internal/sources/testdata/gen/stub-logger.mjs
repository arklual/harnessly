const quiet = { debug() {}, info() {}, warn() {}, error() {}, child() { return quiet; } };
export const createLogger = () => quiet;
export default quiet;
