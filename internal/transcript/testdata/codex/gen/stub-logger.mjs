const quiet = { warn() {}, info() {}, error() {}, debug() {}, child: () => quiet };
export const createLogger = () => quiet;
