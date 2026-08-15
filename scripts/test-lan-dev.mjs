import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";

const head = fs.readFileSync(new URL("../assets/js/head.js", import.meta.url), "utf8");

function apiBaseFor(location) {
    const window = {};
    const document = { documentElement: { dataset: {} }, querySelector: () => null };
    const context = {
        window,
        location,
        document,
        matchMedia: () => ({ matches: false }),
        localStorage: { getItem: () => null, removeItem: () => {} },
    };
    vm.runInNewContext(head, context);
    return window.WAGO_CONFIG.apiBase;
}

assert.equal(
    apiBaseFor({ hostname: "localhost", protocol: "http:", port: "8000" }),
    undefined,
);
assert.equal(
    apiBaseFor({ hostname: "192.168.0.28", protocol: "http:", port: "8000" }),
    "http://192.168.0.28:8787",
);
assert.equal(
    apiBaseFor({ hostname: "plugins.wago.sh", protocol: "https:", port: "" }),
    "https://api.plugins.wago.sh",
);

console.log("LAN dev routing uses the frontend host; production uses the production API");
