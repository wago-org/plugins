// Build-time prerender for social/crawler OG. The site is a client-rendered SPA
// on GitHub Pages, so crawlers (which don't run JS) only ever see index.html.
// This writes a per-package and per-author HTML file — a copy of dist/index.html
// with the <title> + og:/twitter: tags rewritten for that page — so links to
// /{owner}/{short} and /{login} unfurl with real titles and descriptions. The
// SPA still boots from these files and takes over normally.
//
// Package data comes from the live API; if that's unreachable (e.g. an offline
// local build) it falls back to the committed dist/data/packages.json.

import { readFile, writeFile, mkdir } from "node:fs/promises";
import { dirname, join } from "node:path";

const DIST = "dist";
const ORIGIN = "https://plugins.wago.sh";
const API = "https://api.plugins.wago.sh/api/packages";
const LOGO = `${ORIGIN}/assets/wago-logo.png`;

// Single-segment paths owned by the SPA — never emit an author page that would
// shadow one of these.
const RESERVED = new Set([
    "search", "auth", "account", "settings", "notifications",
    "assets", "data", "api", "u", "p", "packages", "404",
]);

const esc = (s) =>
    String(s ?? "")
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;");

// seoBlock renders the marker-delimited <head> SEO block for one page.
function seoBlock({ title, description, url, image, type = "website" }) {
    const t = esc(title);
    const d = esc(description);
    return [
        "<!-- prerender:seo:start -->",
        `<title>${t}</title>`,
        `<meta name="description" content="${d}" />`,
        `<link rel="canonical" href="${esc(url)}" />`,
        `<meta property="og:site_name" content="wago packages" />`,
        `<meta property="og:type" content="${esc(type)}" />`,
        `<meta property="og:title" content="${t}" />`,
        `<meta property="og:description" content="${d}" />`,
        `<meta property="og:url" content="${esc(url)}" />`,
        `<meta property="og:image" content="${esc(image)}" />`,
        `<meta name="twitter:card" content="summary" />`,
        `<meta name="twitter:title" content="${t}" />`,
        `<meta name="twitter:description" content="${d}" />`,
        `<meta name="twitter:image" content="${esc(image)}" />`,
        "<!-- prerender:seo:end -->",
    ].join("\n        ");
}

// Tolerant of extra text in the start marker (index.html annotates it).
const SEO_RE = /<!-- prerender:seo:start[\s\S]*?<!-- prerender:seo:end -->/;

async function emit(relPath, html) {
    const full = join(DIST, relPath, "index.html");
    await mkdir(dirname(full), { recursive: true });
    await writeFile(full, html);
}

async function loadPackages() {
    try {
        const res = await fetch(API, { headers: { accept: "application/json" } });
        if (res.ok) {
            const data = await res.json();
            if (Array.isArray(data?.packages) && data.packages.length) {
                console.log(`prerender: ${data.packages.length} packages from the live API`);
                return data.packages;
            }
        }
        console.warn(`prerender: API returned ${res.status}; falling back to committed index`);
    } catch (err) {
        console.warn(`prerender: API unreachable (${err.message}); falling back to committed index`);
    }
    try {
        const file = JSON.parse(await readFile(join(DIST, "data", "packages.json"), "utf8"));
        const pkgs = file.packages || [];
        console.log(`prerender: ${pkgs.length} packages from data/packages.json`);
        return pkgs;
    } catch {
        console.warn("prerender: no package data available; only the homepage gets OG");
        return [];
    }
}

async function main() {
    const template = await readFile(join(DIST, "index.html"), "utf8");
    if (!SEO_RE.test(template)) {
        console.error("prerender: SEO markers not found in index.html — skipping");
        return;
    }
    const packages = await loadPackages();

    let pkgCount = 0;
    const authors = new Map(); // login(lower) -> display login

    const noteAuthor = (login) => {
        if (!login) return;
        const key = String(login).toLowerCase();
        if (RESERVED.has(key) || authors.has(key)) return;
        authors.set(key, login);
    };

    for (const p of packages) {
        const short = p.short;
        const owner = p.ownerLogin || "packages";
        if (!short) continue;
        const url = `${ORIGIN}/${encodeURIComponent(owner)}/${encodeURIComponent(short)}`;
        const description =
            p.description || `${p.name || short} — a package on the wago registry.`;
        const html = template.replace(
            SEO_RE,
            seoBlock({ title: `${owner}/${short} | Plugins`, description, url, image: LOGO }),
        );
        await emit(`${owner}/${short}`, html);
        pkgCount++;

        noteAuthor(p.ownerLogin);
        for (const a of p.authors || []) noteAuthor(a.github);
        for (const c of p.contributors || []) noteAuthor(c);
    }

    for (const [, login] of authors) {
        const url = `${ORIGIN}/${encodeURIComponent(login)}`;
        const html = template.replace(
            SEO_RE,
            seoBlock({
                title: `@${login} | Plugins`,
                description: `${login}'s packages on the wago registry.`,
                url,
                image: `https://github.com/${encodeURIComponent(login)}.png`,
                type: "profile",
            }),
        );
        await emit(login, html);
    }

    console.log(`prerender: wrote ${pkgCount} package pages + ${authors.size} author pages`);
}

main().catch((err) => {
    // Never fail the build over prerender — the SPA + homepage OG still work.
    console.error("prerender: failed:", err);
});
