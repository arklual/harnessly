import * as http from 'http';

/** True when a Harnessly runtime answers /api/health on this loopback port. */
export const isRuntimeHealthy = (port: number, timeoutMs = 1500): Promise<boolean> =>
  new Promise((resolve) => {
    const req = http.get(`http://127.0.0.1:${port}/api/health`, { timeout: timeoutMs }, (res) => {
      res.resume();
      res.on('end', () => resolve(res.statusCode === 200));
    });
    req.on('error', () => resolve(false));
    req.on('timeout', () => {
      req.destroy();
      resolve(false);
    });
  });
