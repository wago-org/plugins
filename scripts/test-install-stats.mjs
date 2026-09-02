import assert from "node:assert/strict";

import { monthlyInstallCount } from "../assets/js/util.js";

assert.equal(monthlyInstallCount([], 31), 31, "missing history uses the backend monthly total");
assert.equal(
    monthlyInstallCount([
        { date: "2026-08-31", count: 5 },
        ...Array.from({ length: 30 }, (_, index) => ({ date: `2026-09-${String(index + 1).padStart(2, "0")}`, count: 1 })),
    ], 99),
    30,
    "loaded history sums only the latest 30 days",
);
assert.equal(
    monthlyInstallCount([{ date: "2026-09-02", count: 0 }], 31),
    0,
    "loaded zero history does not reuse a stale fallback",
);

console.log("monthly install totals use 30-day history with a backend fallback");
