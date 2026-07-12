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
    (src: string, opts?: { breaks?: boolean }): string;
    setOptions(opts: { gfm?: boolean; breaks?: boolean }): void;
};
type Purify = {
    sanitize(dirty: string, cfg?: Record<string, unknown>): string;
    addHook(entryPoint: string, cb: (node: Element) => void): void;
};
// Vendored highlighter (Prism grammars via refractor) — see assets/vendor.
type Highlighter = {
    highlight(code: string, lang: string): string | null; // Prism-classed HTML, or null if unsupported
    supports(lang: string): boolean;
};

let marked: MarkedFn | null = null;
let purify: Purify | null = null;
let highlighter: Highlighter | null = null;
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

// README bodies come from the package's own repo, so — like GitHub, but a touch
// more permissive — we render images, task-list checkboxes, <details>, <kbd>, and
// the layout/centering markup READMEs lean on (<div>/<center> + `align`/`style`).
// Comment/review bodies stay on the strict list above.
const RICH_TAGS = [...ALLOWED_TAGS, "img", "input", "details", "summary", "kbd", "picture", "source", "div", "center"];
// `class` survives so the highlighter's `token` spans keep their colours; `style`
// enables author-driven layout (centering, columns, colour). DOMPurify still runs
// every `style` value through its CSS filter, so no scripts/expressions get in.
const RICH_ATTR = [...ALLOWED_ATTR, "src", "alt", "width", "height", "loading", "type", "checked", "disabled", "open", "srcset", "media", "class", "style"];

// highlightBlocks applies syntax highlighting to fenced code blocks in-place, the
// way GitHub does: only blocks with an explicit, recognised language. The result
// is `<span class="token …">` spans that the theme in tokens.css colours.
function highlightBlocks(rawHtml: string): string {
    if (!highlighter || typeof DOMParser === "undefined") return rawHtml;
    try {
        const doc = new DOMParser().parseFromString(`<body>${rawHtml}</body>`, "text/html");
        doc.querySelectorAll("pre > code").forEach((code) => {
            const m = (code.getAttribute("class") || "").match(/language-([\w+#-]+)/i);
            const lang = m && m[1] ? m[1].toLowerCase() : "";
            if (!lang || !highlighter!.supports(lang)) return; // unlabeled / unknown → leave plain
            const out = highlighter!.highlight(code.textContent || "", lang);
            if (out != null) code.innerHTML = out;
        });
        return doc.body.innerHTML;
    } catch {
        return rawHtml;
    }
}

// Options for a single render. base rewrites the README's relative image/link
// URLs to GitHub (raw for images, blob for links) — exactly what GitHub does.
export interface MdOptions {
    images?: boolean; // allow <img> and other rich tags (README, not comments)
    mentions?: boolean; // linkify @handles to in-app profiles (default true; off for READMEs)
    base?: { owner: string; repo: string; ref?: string };
}

// Set for the duration of a rich render so the sanitize hook can resolve
// relative URLs. Rendering is synchronous, so a module-level value is safe.
let activeBase: MdOptions["base"] | null = null;

// resolveRepoUrl turns a README-relative URL into an absolute GitHub URL, the way
// GitHub renders them: images point at raw.githubusercontent.com, links at the
// repo's blob view, both pinned to the release ref. Absolute URLs and in-page
// anchors pass through untouched.
function resolveRepoUrl(base: NonNullable<MdOptions["base"]>, url: string, image: boolean): string {
    if (!url || url.startsWith("#")) return url;
    if (/^[a-z][a-z0-9+.-]*:/i.test(url) || url.startsWith("//")) return url; // already absolute
    const ref = base.ref || "HEAD";
    const path = url.replace(/^\.\//, "").replace(/^\//, ""); // repo-root or dir relative
    return image
        ? `https://raw.githubusercontent.com/${base.owner}/${base.repo}/${ref}/${path}`
        : `https://github.com/${base.owner}/${base.repo}/blob/${ref}/${path}`;
}

// initMarkdown loads the vendored libraries and configures them. It resolves
// even on failure — renderMarkdown then keeps using the escaped-text fallback.
export async function initMarkdown(): Promise<void> {
    if (ready) return;
    try {
        const [markedMod, purifyMod, hlMod] = await Promise.all([
            // Vendored ESM under assets/vendor — untyped, resolved at runtime
            // relative to the compiled module (assets/js → assets/vendor).
            // @ts-expect-error — no type declarations for the vendored bundle.
            import("../vendor/marked.esm.js"),
            // @ts-expect-error — no type declarations for the vendored bundle.
            import("../vendor/purify.es.mjs"),
            // The highlighter is optional — its failure must not disable markdown.
            // @ts-expect-error — no type declarations for the vendored bundle.
            import("../vendor/highlight.esm.js").catch(() => null),
        ]);
        marked = markedMod.marked as unknown as MarkedFn;
        purify = purifyMod.default as unknown as Purify;
        highlighter = hlMod ? (hlMod as unknown as Highlighter) : null;
        marked.setOptions({ gfm: true, breaks: true });
        // Force external links to open safely in a new tab, but leave internal
        // in-app links (#/… — e.g. @mention profile links) navigating in place.
        purify.addHook("afterSanitizeAttributes", (node) => {
            // In a README render, rewrite relative image/link URLs to GitHub.
            if (activeBase && node.tagName === "IMG") {
                const src = node.getAttribute("src") || "";
                const abs = resolveRepoUrl(activeBase, src, true);
                if (abs !== src) node.setAttribute("src", abs);
                node.setAttribute("loading", "lazy");
            }
            if (node.tagName === "A") {
                let href = node.getAttribute("href") || "";
                if (activeBase) {
                    const abs = resolveRepoUrl(activeBase, href, false);
                    if (abs !== href) {
                        node.setAttribute("href", abs);
                        href = abs;
                    }
                }
                // Internal in-app links (/{login}, /{owner}/{repository}) navigate in
                // place; everything else opens in a new tab.
                if (href.startsWith("/") || href.startsWith("#")) {
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
        (_m, pre: string, login: string) => `${pre}[@${login}](/${login})`,
    );
}

// renderMarkdown returns sanitized HTML for `src`. Before initMarkdown resolves,
// or if the libraries failed to load, it returns escaped text wrapped in a
// paragraph so plain content still shows (and stays inert).
export function renderMarkdown(src: string, opts: MdOptions = {}): string {
    const input = src ?? "";
    if (!ready || !marked || !purify) {
        return `<p>${esc(input)}</p>`;
    }
    const source = opts.mentions === false ? input : linkifyMentions(input);
    activeBase = opts.base ?? null;
    try {
        // READMEs are standard Markdown (a single newline is a soft wrap, so a
        // hard-wrapped paragraph flows as one line); comments keep GFM line breaks
        // so a user's newlines are preserved. Highlight fenced code for READMEs.
        const rawHtml = opts.images
            ? highlightBlocks(marked(source, { breaks: false }))
            : marked(source, { breaks: true });
        return purify.sanitize(rawHtml, {
            ALLOWED_TAGS: opts.images ? RICH_TAGS : ALLOWED_TAGS,
            ALLOWED_ATTR: opts.images ? RICH_ATTR : ALLOWED_ATTR,
            ALLOW_DATA_ATTR: false,
        });
    } catch {
        return `<p>${esc(input)}</p>`;
    } finally {
        activeBase = null;
    }
}

// Wrap rendered markdown in the `.md` container the stylesheet targets.
export function mdBlock(src: string, opts: MdOptions = {}): string {
    return `<div class="md">${renderMarkdown(src, opts)}</div>`;
}
