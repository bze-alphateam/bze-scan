import { describe, expect, it } from "vitest";

import { createRateLimiter, type Clock } from "./rate-limit";

/** A clock that moves only when slept on, recording every sleep. */
function fakeClock(start = 1_000): Clock & { sleeps: number[]; time: number } {
  const clock = {
    time: start,
    sleeps: [] as number[],
    now: () => clock.time,
    sleep: async (ms: number) => {
      clock.sleeps.push(ms);
      clock.time += ms;
    },
  };
  return clock;
}

describe("createRateLimiter", () => {
  it("lets the first call through and spaces the next ones 200 ms apart at 5 a second", async () => {
    const clock = fakeClock();
    const limiter = createRateLimiter(5, clock);
    const starts: number[] = [];
    for (let i = 0; i < 6; i++) {
      await limiter.wait();
      starts.push(clock.time);
    }
    expect(starts).toEqual([1_000, 1_200, 1_400, 1_600, 1_800, 2_000]);
    // Never more than five starts in any one second.
    expect(starts.filter((t) => t >= 1_000 && t < 2_000)).toHaveLength(5);
  });

  it("does not wait when the calls themselves took longer than the spacing", async () => {
    const clock = fakeClock();
    const limiter = createRateLimiter(5, clock);
    await limiter.wait();
    clock.time += 500;
    await limiter.wait();
    expect(clock.sleeps).toEqual([]);
  });

  it("waits only for what is left of the spacing", async () => {
    const clock = fakeClock();
    const limiter = createRateLimiter(5, clock);
    await limiter.wait();
    clock.time += 150;
    await limiter.wait();
    expect(clock.sleeps).toEqual([50]);
  });

  it("rejects at once on an aborted signal", async () => {
    const limiter = createRateLimiter(5, fakeClock());
    const controller = new AbortController();
    controller.abort();
    await expect(limiter.wait(controller.signal)).rejects.toBeDefined();
  });
});
