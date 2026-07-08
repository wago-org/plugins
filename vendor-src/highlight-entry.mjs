// Bundle entry for the vendored syntax highlighter: Prism grammars via refractor
// (synchronous, rich token coverage) + hast-util-to-html to serialize. Bundled to
// a single self-contained ESM at assets/vendor/highlight.esm.js — no runtime CDN.
//
// Rebuild after editing this file:
//   npm i --no-save esbuild refractor hast-util-to-html
//   npx esbuild vendor-src/highlight-entry.mjs --bundle --format=esm --minify \
//     --platform=browser --legal-comments=none --outfile=assets/vendor/highlight.esm.js
//   npm i --no-save jsdom   # restore the test-only dep npm prunes above
import { refractor } from "refractor";
import { toHtml } from "hast-util-to-html";

// Common language aliases → refractor's registered names.
const ALIAS = {
    js: "javascript", jsx: "jsx", ts: "typescript", tsx: "tsx",
    sh: "bash", shell: "bash", zsh: "bash", console: "bash", shellsession: "bash",
    yml: "yaml", rs: "rust", py: "python", golang: "go", md: "markdown",
    "c++": "cpp", "c#": "csharp", cs: "csharp", htm: "html", plaintext: "text", text: "text",
};

export function supports(lang) {
    const name = ALIAS[lang] || lang;
    return name !== "text" && refractor.registered(name);
}

// highlight returns Prism-classed HTML (<span class="token keyword">…) for code,
// or null if the language isn't supported (caller then leaves it plain).
export function highlight(code, lang) {
    const name = ALIAS[lang] || lang;
    if (!refractor.registered(name)) return null;
    try {
        return toHtml(refractor.highlight(code, name));
    } catch {
        return null;
    }
}
