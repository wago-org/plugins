// GitHub-flavoured Markdown rendering for untrusted bodies (comments, reviews).
//
// Both `marked` and `DOMPurify` are self-hosted ESM under assets/vendor and are
// loaded lazily via dynamic import from the compiled output (assets/js →
// ../vendor). The site can't reach a CDN at runtime (CSP / offline), so nothing
// here touches the network.
//
// The specifier "../vendor/…" resolves the same way in the browser (assets/js →
// assets/vendor) and under Node when a test imports the compiled assets/js
// module — both resolve dynamic imports relative to the importing module's URL.

import { esc } from "./util.js";

type MarkedFn = {
    (src: string): string;
    setOptions(opts: { gfm?: boolean; breaks?: boolean }): void;
};
type Purify = {
    sanitize(dirty: string, cfg?: Record<string, unknown>): string;
    addHook(entryPoint: string, cb: (node: Element) => void): void;
};

let marked: MarkedFn | null = null;
let purify: Purify | null = null;
let ready = false;

// Conservative allow-list: standard Markdown output plus links, code, tables.
// No raw HTML passthrough beyond these, no images, no styles/scripts.
const ALLOWED_TAGS = [
    "p", "br", "hr", "blockquote", "pre", "code", "span",
    "strong", "em", "del", "s", "b", "i", "u", "sub", "sup", "mark",
    "h1", "h2", "h3", "h4", "h5", "h6",
    "ul", "ol", "li",
    "a",
    "table", "thead", "tbody", "tr", "th", "td",
];
const ALLOWED_ATTR = ["href", "title", "align", "start"];

// initMarkdown loads the vendored libraries and configures them. It resolves
// even on failure — renderMarkdown then keeps using the escaped-text fallback.
export async function initMarkdown(): Promise<void> {
    if (ready) return;
    try {
        const [markedMod, purifyMod] = await Promise.all([
            // Vendored ESM under assets/vendor — untyped, resolved at runtime
            // relative to the compiled module (assets/js → assets/vendor).
            // @ts-expect-error — no type declarations for the vendored bundle.
            import("../vendor/marked.esm.js"),
            // @ts-expect-error — no type declarations for the vendored bundle.
            import("../vendor/purify.es.mjs"),
        ]);
        marked = markedMod.marked as unknown as MarkedFn;
        purify = purifyMod.default as unknown as Purify;
        marked.setOptions({ gfm: true, breaks: true });
        // Force external links to open safely in a new tab, but leave internal
        // in-app links (#/… — e.g. @mention profile links) navigating in place.
        purify.addHook("afterSanitizeAttributes", (node) => {
            if (node.tagName === "A") {
                const href = node.getAttribute("href") || "";
                if (href.startsWith("#")) {
                    node.removeAttribute("target");
                    node.setAttribute("rel", "noopener");
                } else {
                    node.setAttribute("target", "_blank");
                    node.setAttribute("rel", "noopener nofollow");
                }
            }
        });
        // Only mark ready if sanitize is actually usable in this environment.
        if (typeof purify.sanitize === "function" && purify.sanitize("<b>x</b>")) {
            ready = true;
        }
    } catch {
        // Leave `ready` false; renderMarkdown falls back to escaped text.
        marked = null;
        purify = null;
    }
}

// Turn @mentions into markdown links to the user's wago profile. Matches a
// GitHub-style login (1–39 chars, alnum + hyphen) only when it follows start of
// string or a non-identifier char that isn't part of an email/path (so
// "a@b.com" and "foo/@bar" don't match). Applied to the source before marked.
function linkifyMentions(src: string): string {
    return src.replace(
        /(^|[\s([{><,;:!?])@([A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?)/g,
        (_m, pre: string, login: string) => `${pre}[@${login}](#/u/${login})`,
    );
}

// renderMarkdown returns sanitized HTML for `src`. Before initMarkdown resolves,
// or if the libraries failed to load, it returns escaped text wrapped in a
// paragraph so plain content still shows (and stays inert).
export function renderMarkdown(src: string): string {
    const input = src ?? "";
    if (!ready || !marked || !purify) {
        return `<p>${esc(input)}</p>`;
    }
    try {
        const rawHtml = marked(linkifyMentions(input));
        return purify.sanitize(rawHtml, {
            ALLOWED_TAGS,
            ALLOWED_ATTR,
            ALLOW_DATA_ATTR: false,
        });
    } catch {
        return `<p>${esc(input)}</p>`;
    }
}

// Wrap rendered markdown in the `.md` container the stylesheet targets.
export function mdBlock(src: string): string {
    return `<div class="md">${renderMarkdown(src)}</div>`;
}
