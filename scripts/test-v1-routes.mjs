import assert from "node:assert/strict";

import { canonicalPluginIDFromPath } from "../assets/js/routes.js";
import { findPackage } from "../assets/js/state.js";

assert.equal(
    canonicalPluginIDFromPath("/github.com/wago-org/wasi"),
    "github.com/wago-org/wasi",
);
assert.equal(
    canonicalPluginIDFromPath("/github.com/acme/plugin/provider"),
    "github.com/acme/plugin/provider",
);

for (const rejected of [
    "/wago-org/wasi",
    "/packages/wasi",
    "/github.com%2Fwago-org%2Fwasi",
    "/github.com/wago-org%2Fwasi",
    "/github.com/wago-org/%77asi",
    "/github.com//wasi",
    "/github.com/wago-org/wasi/",
    "//github.com/wago-org/wasi",
    "/github.com/wago-org/plugin+bad",
    "/github.com/wago-org/.plugin",
    "/gitlab.com/wago-org/wasi",
    "/#/p/wasi",
]) {
    assert.equal(canonicalPluginIDFromPath(rejected), null, `${rejected} must not be a v1 route`);
}

const source = {
    short: "github.com/acme/bundle",
    module: "github.com/acme/bundle",
    providerIds: ["github.com/acme/bundle/metrics"],
    versions: [],
};
assert.equal(
    findPackage({ packages: [source] }, "github.com/acme/bundle/metrics"),
    source,
    "a canonical child-provider URL must resolve through the lean package list",
);
assert.equal(findPackage({ packages: [source] }, "acme/bundle"), null);

console.log("v1 routes: canonical full IDs accepted; short and legacy forms rejected");
