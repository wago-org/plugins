import assert from "node:assert/strict";

const start = "https://plugins.wago.sh/auth";
globalThis.window = {
    location: { href: start },
    WAGO_CONFIG: { apiBase: "https://api.plugins.wago.sh" },
};
globalThis.location = {
    href: start,
    hostname: "plugins.wago.sh",
    protocol: "https:",
    pathname: "/auth",
    search: "",
};
globalThis.history = { state: null };

const { doSignIn } = await import("../assets/js/app.js");
const { authHistoryState } = await import("../assets/js/auth-return.js");
doSignIn();

const oauth = new URL(window.location.href);
assert.equal(
    oauth.searchParams.get("redirect"),
    "https://plugins.wago.sh/",
    "signing in from /auth should return to the previous non-auth page, or home when none exists",
);

const previous = "https://plugins.wago.sh/search?q=runtime";
history.state = authHistoryState(previous, null);
window.location.href = start;
doSignIn();

const returnedOauth = new URL(window.location.href);
assert.equal(
    returnedOauth.searchParams.get("redirect"),
    previous,
    "signing in should return to the non-auth page that opened /auth",
);

history.state = authHistoryState("https://plugins.wago.sh/wago-org/wasi", null);
window.location.href = start;
doSignIn();
assert.equal(
    new URL(window.location.href).searchParams.get("redirect"),
    "https://plugins.wago.sh/wago-org/wasi",
    "package pages should be preserved as OAuth return targets",
);

console.log("auth return route tests passed");
