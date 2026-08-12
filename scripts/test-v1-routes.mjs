import assert from "node:assert/strict";

import { canonicalPluginIDFromPath } from "../assets/js/routes.js";
import { findPackage } from "../assets/js/state.js";
import { displayPluginID, pkgPath } from "../assets/js/util.js";

assert.equal(
    canonicalPluginIDFromPath("/wago-org/wasi"),
    "github.com/wago-org/wasi",
);

assert.equal(displayPluginID("github.com/wago-org/wasi"), "wago-org/wasi");
assert.equal(displayPluginID("example.com/acme/plugin"), "example.com/acme/plugin");
assert.equal(pkgPath({ short: "github.com/wago-org/wasi" }), "/wago-org/wasi");
assert.equal(
    canonicalPluginIDFromPath("/acme/plugin/provider"),
    "github.com/acme/plugin/provider",
);
assert.equal(
    canonicalPluginIDFromPath("/wago-org/wasi/"),
    "github.com/wago-org/wasi",
    "GitHub Pages directory redirects must remain routable",
);

for (const rejected of [
    "/github.com/wago-org/wasi",
    "/wago-org%2Fwasi",
    "/wago-org/wasi%2Fprovider",
    "/wago-org/%77asi",
    "/wago-org//wasi",
    "/wago-org/wasi//",
    "//wago-org/wasi",
    "/wago-org/plugin+bad",
    "/wago-org/.plugin",
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

console.log("v1 routes: GitHub-hostless URLs map to canonical IDs; host-prefixed and malformed forms rejected");
