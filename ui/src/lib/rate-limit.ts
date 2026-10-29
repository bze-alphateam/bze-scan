/** Paces calls: `wait()` resolves when the next call may start. */
export interface RateLimiter {
  wait(signal?: AbortSignal): Promise<void>;
}

/** The time source a limiter reads and sleeps on: the real one, or a fake in tests. */
export interface Clock {
  now(): number;
  sleep(ms: number, signal?: AbortSignal): Promise<void>;
}

function abortError(signal: AbortSignal): unknown {
  return signal.reason ?? new DOMException("Aborted", "AbortError");
}

export const systemClock: Clock = {
  now: () => Date.now(),
  sleep: (ms, signal) =>
    new Promise((resolve, reject) => {
      if (signal?.aborted) return reject(abortError(signal));
      const done = () => {
        signal?.removeEventListener("abort", stop);
        resolve();
      };
      const timer = setTimeout(done, ms);
      const stop = () => {
        clearTimeout(timer);
        reject(abortError(signal!));
      };
      signal?.addEventListener("abort", stop, { once: true });
    }),
};

/**
 * At most `perSecond` calls a second, evenly spaced: each call starts at
 * least 1000 / perSecond ms after the one before.
 */
export function createRateLimiter(perSecond: number, clock: Clock = systemClock): RateLimiter {
  const interval = 1000 / perSecond;
  let next = -Infinity;
  return {
    async wait(signal) {
      if (signal?.aborted) throw abortError(signal);
      const now = clock.now();
      const slot = Math.max(now, next);
      next = slot + interval;
      if (slot > now) await clock.sleep(slot - now, signal);
    },
  };
}
