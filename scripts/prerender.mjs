// Build-time crawler output for the registry SPA.
//
// The interactive application still owns #app in browsers, but the deployed
// HTML contains complete catalog/package/author content before JavaScript runs.
// This same pass generates the LLM documents, structured catalog JSON, JSON-LD,
// robots.txt, and sitemap from one package snapshot.

import { readFile, writeFile, mkdir } from "node:fs/promises";
import { dirname, join } from "node:path";

const DIST = "dist";
const ORIGIN = "https://plugins.wago.sh";
const API = "https://api.plugins.wago.sh/api/packages";
const LOGO = `${ORIGIN}/assets/wago-logo.png`;
const SEO_RE = /<!-- prerender:seo:start[\s\S]*?<!-- prerender:seo:end -->/;
const CONTENT_RE = /<!-- prerender:content:start[\s\S]*?<!-- prerender:content:end -->/;

const RESERVED = new Set([
    "search",
    "auth",
    "account",
    "settings",
    "notifications",
    "assets",
    "data",
    "api",
    "u",
    "p",
    "packages",
    "404",
]);

const esc = (value) =>
    String(value ?? "")
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;");

const clean = (value) => String(value ?? "").replace(/\s+/g, " ").trim();

const canonicalID = (pkg) => {
    const explicit = clean(pkg.id).replace(/^github\.com\//, "");
    if (explicit.includes("/")) return explicit;
    const legacy = clean(pkg.short).replace(/^github\.com\//, "");
    if (legacy.includes("/")) return legacy;
    if (pkg.ownerLogin && legacy) return `${pkg.ownerLogin}/${legacy}`;
    return clean(pkg.name).replace(/^github\.com\//, "");
};

const pathForID = (id) => id.split("/").map(encodeURIComponent).join("/");

const unique = (values) =>
    [...new Set((values || []).map(clean).filter(Boolean))];

function catalogPackage(pkg) {
    const id = canonicalID(pkg);
    return {
        id,
        url: `${ORIGIN}/${pathForID(id)}`,
        description: clean(pkg.description),
        repository: clean(pkg.repository) || `https://github.com/${id}`,
        homepage: clean(pkg.homepage) || undefined,
        category: clean(pkg.category) || "uncategorized",
        stability: clean(pkg.stability) || "unspecified",
        version: clean(pkg.latestVersion || pkg.version) || "unreleased",
        license: clean(pkg.license) || "unspecified",
        official: Boolean(pkg.official),
        verified: Boolean(pkg.verified),
        owner: clean(pkg.ownerLogin || id.split("/")[0]),
        keywords: unique([...(pkg.keywords || []), ...(pkg.tags || [])]),
        capabilities: unique(pkg.capabilities),
        compatibility: {
            engines: pkg.compatibility?.engines || {},
            platforms: unique(pkg.compatibility?.platforms),
        },
        authors: (pkg.authors || []).map((author) => ({
            name: clean(author.name),
            github: clean(author.github),
        })),
        contributors: unique(pkg.contributors),
        subpackages: (pkg.subpackages || []).map((sub) => ({
            id: clean(sub.id),
            name: clean(sub.name),
            import: clean(sub.import),
            version: clean(sub.version),
            description: clean(sub.description),
            stability: clean(sub.stability) || "unspecified",
            tags: unique(sub.tags),
            compatibility: sub.compatibility || { engines: {}, platforms: [] },
        })),
        updatedAt: clean(pkg.updatedAt) || undefined,
        metrics: {
            stars: Number(pkg.stars || 0),
            rating: Number(pkg.rating || 0),
            ratingCount: Number(pkg.ratingCount || 0),
            installsWeek: Number(pkg.installsWeek || 0),
            installsMonth: Number(pkg.installsMonth || 0),
            installsTotal: Number(pkg.installsTotal || 0),
        },
    };
}

function jsonLdScript(value) {
    const json = JSON.stringify(value).replace(/</g, "\\u003c");
    return `<script type="application/ld+json">${json}</script>`;
}

function seoBlock({ title, description, url, image = LOGO, type = "website", jsonLd }) {
    const t = esc(title);
    const d = esc(description);
    return [
        "<!-- prerender:seo:start -->",
        `<title>${t}</title>`,
        `<meta name="description" content="${d}" />`,
        `<link rel="canonical" href="${esc(url)}" />`,
        `<meta name="robots" content="index, follow, max-image-preview:large, max-snippet:-1" />`,
        `<meta property="og:site_name" content="wago plugins" />`,
        `<meta property="og:type" content="${esc(type)}" />`,
        `<meta property="og:title" content="${t}" />`,
        `<meta property="og:description" content="${d}" />`,
        `<meta property="og:url" content="${esc(url)}" />`,
        `<meta property="og:image" content="${esc(image)}" />`,
        `<meta name="twitter:card" content="summary" />`,
        `<meta name="twitter:title" content="${t}" />`,
        `<meta name="twitter:description" content="${d}" />`,
        `<meta name="twitter:image" content="${esc(image)}" />`,
        jsonLdScript(jsonLd),
        "<!-- prerender:seo:end -->",
    ].join("\n        ");
}

function contentBlock(content) {
    return `<!-- prerender:content:start -->\n${content}\n            <!-- prerender:content:end -->`;
}

function page(template, seo, content) {
    return template.replace(SEO_RE, seoBlock(seo)).replace(CONTENT_RE, contentBlock(content));
}

function tags(values) {
    if (!values.length) return "";
    return `<div class="crawler__tags">${values
        .map((value) => `<span class="crawler__tag">${esc(value)}</span>`)
        .join("")}</div>`;
}

function brand() {
    return `<a class="crawler__brand" href="/">
                    <img src="/assets/wago-logo.png" alt="" />
                    <span>wago plugins</span>
                </a>`;
}

function packageCard(pkg) {
    const labels = unique([
        pkg.category !== "uncategorized" ? pkg.category : "",
        pkg.stability !== "unspecified" ? pkg.stability : "",
        pkg.official ? "official" : "",
        ...pkg.keywords.slice(0, 4),
    ]);
    return `<a class="crawler__card" href="/${esc(pathForID(pkg.id))}">
                    <h3>${esc(pkg.id)}</h3>
                    <p>${esc(pkg.description || "A plugin in the wago registry.")}</p>
                    <div class="crawler__meta">version ${esc(pkg.version)} · owner ${esc(pkg.owner)}</div>
                    ${tags(labels)}
                </a>`;
}

function homeContent(packages, generated) {
    return `            <main class="crawler">
                ${brand()}
                <h1>Extend your runtime.</h1>
                <p class="crawler__lead">
                    Browse Go plugins for the wago WebAssembly engine: host integrations,
                    runtime services, compiler extensions, workers, and reusable execution infrastructure.
                </p>
                <p class="crawler__meta">${packages.length} published plugins · catalog generated ${esc(generated)}</p>
                <div class="crawler__grid">
                ${packages.map(packageCard).join("\n")}
                </div>
                <h2>Machine-readable registry</h2>
                <p>
                    Read the <a href="/llms.txt">concise registry brief</a>,
                    <a href="/llms-full.txt">complete plugin catalog</a>, or
                    <a href="/data/catalog.json">structured JSON catalog</a>.
                </p>
                <p><a href="https://github.com/wago-org/wago">Read the wago documentation →</a></p>
            </main>`;
}

function fact(label, value) {
    if (!value) return "";
    return `<li><strong>${esc(label)}</strong>${esc(value)}</li>`;
}

function packageContent(pkg) {
    const engines = Object.entries(pkg.compatibility.engines)
        .map(([engine, range]) => `${engine} ${range}`)
        .join(", ");
    const platforms = pkg.compatibility.platforms.join(", ");
    const subpackages = pkg.subpackages.length
        ? `<h2>Subpackages</h2><ul>${pkg.subpackages
              .map(
                  (sub) =>
                      `<li><strong>${esc(sub.name || sub.id)}</strong>${sub.import ? ` — <code>${esc(sub.import)}</code>` : ""}${sub.description ? `: ${esc(sub.description)}` : ""}</li>`,
              )
              .join("")}</ul>`
        : "";
    return `            <main class="crawler">
                ${brand()}
                <p class="crawler__meta"><a href="/">Registry</a> / ${esc(pkg.owner)}</p>
                <h1>${esc(pkg.id)}</h1>
                <p class="crawler__lead">${esc(pkg.description || "A plugin in the wago registry.")}</p>
                <ul class="crawler__facts">
                    ${fact("Latest version", pkg.version)}
                    ${fact("Owner", pkg.owner)}
                    ${fact("Category", pkg.category)}
                    ${fact("Stability", pkg.stability)}
                    ${fact("License", pkg.license)}
                    ${fact("Compatible engines", engines)}
                    ${fact("Platforms", platforms)}
                </ul>
                ${tags(unique([...pkg.keywords, ...pkg.capabilities]))}
                ${subpackages}
                <h2>Sources</h2>
                <p><a href="${esc(pkg.repository)}">Repository on GitHub →</a></p>
                <p><a href="/data/catalog.json">Structured registry data</a> · <a href="/llms-full.txt">Complete readable catalog</a></p>
            </main>`;
}

function authorContent(login, packages) {
    return `            <main class="crawler">
                ${brand()}
                <p class="crawler__meta"><a href="/">Registry</a> / authors</p>
                <h1>@${esc(login)}</h1>
                <p class="crawler__lead">${esc(login)} maintains or contributes to ${packages.length} plugin${packages.length === 1 ? "" : "s"} in the wago registry.</p>
                <div class="crawler__grid">${packages.map(packageCard).join("\n")}</div>
                <p><a href="https://github.com/${encodeURIComponent(login)}">View ${esc(login)} on GitHub →</a></p>
            </main>`;
}

function homeJsonLd(packages, generated) {
    return {
        "@context": "https://schema.org",
        "@graph": [
            {
                "@type": "CollectionPage",
                "@id": `${ORIGIN}/#registry`,
                name: "wago plugins",
                url: `${ORIGIN}/`,
                description: "The plugin registry for the wago WebAssembly engine.",
                dateModified: generated,
                mainEntity: { "@id": `${ORIGIN}/#catalog` },
            },
            {
                "@type": "ItemList",
                "@id": `${ORIGIN}/#catalog`,
                name: "Published wago plugins",
                numberOfItems: packages.length,
                itemListElement: packages.map((pkg, index) => ({
                    "@type": "ListItem",
                    position: index + 1,
                    url: pkg.url,
                    name: pkg.id,
                })),
            },
        ],
    };
}

function packageJsonLd(pkg) {
    return {
        "@context": "https://schema.org",
        "@type": "SoftwareSourceCode",
        "@id": `${pkg.url}#software`,
        name: pkg.id,
        description: pkg.description,
        url: pkg.url,
        codeRepository: pkg.repository,
        programmingLanguage: "Go",
        runtimePlatform: "wago WebAssembly engine",
        version: pkg.version,
        license: pkg.license,
        keywords: pkg.keywords.join(", "),
        dateModified: pkg.updatedAt,
        author: {
            "@type": pkg.owner.includes("-org") ? "Organization" : "Person",
            name: pkg.owner,
            url: `https://github.com/${encodeURIComponent(pkg.owner)}`,
        },
    };
}

function authorJsonLd(login, packages) {
    return {
        "@context": "https://schema.org",
        "@type": "ProfilePage",
        name: `@${login} on the wago plugin registry`,
        url: `${ORIGIN}/${encodeURIComponent(login)}`,
        mainEntity: {
            "@type": login.includes("-org") ? "Organization" : "Person",
            name: login,
            url: `https://github.com/${encodeURIComponent(login)}`,
            owns: packages.map((pkg) => ({ "@id": `${pkg.url}#software` })),
        },
    };
}

function sitemap(entries) {
    return `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${entries
        .map(
            ({ url, lastmod }) =>
                `  <url>\n    <loc>${esc(url)}</loc>${lastmod ? `\n    <lastmod>${esc(lastmod.slice(0, 10))}</lastmod>` : ""}\n  </url>`,
        )
        .join("\n")}\n</urlset>\n`;
}

function llmsSummary(packages, generated) {
    const categories = unique(packages.map((pkg) => pkg.category));
    return `# wago plugins

> The official registry for Go plugins that extend the wago WebAssembly engine.

Canonical site: ${ORIGIN}/
Registry API: ${API}
Catalog generated: ${generated}
Published plugins: ${packages.length}
Categories: ${categories.join(", ")}

## Read next

- [Complete plugin catalog with descriptions, compatibility, versions, and repositories](${ORIGIN}/llms-full.txt)
- [Structured plugin catalog (JSON)](${ORIGIN}/data/catalog.json)
- [Browse the interactive registry](${ORIGIN}/)
- [wago engine documentation](https://github.com/wago-org/wago)

Treat version "unreleased" or "0.0.0" as a placeholder, not a stable published release. Use each package's repository and compatibility fields as the canonical technical references.
`;
}

function llmsFull(packages, generated) {
    const sections = packages.map((pkg) => {
        const engines = Object.entries(pkg.compatibility.engines)
            .map(([engine, range]) => `${engine} ${range}`)
            .join(", ");
        return `## ${pkg.id}

- Registry page: ${pkg.url}
- Repository: ${pkg.repository}
- Description: ${pkg.description || "No description published."}
- Version: ${pkg.version}
- Owner: ${pkg.owner}
- Category: ${pkg.category}
- Stability: ${pkg.stability}
- License: ${pkg.license}
- Official: ${pkg.official}
- Verified: ${pkg.verified}
- Engines: ${engines || "not specified"}
- Platforms: ${pkg.compatibility.platforms.join(", ") || "not specified"}
- Keywords: ${pkg.keywords.join(", ") || "none published"}
- Capabilities: ${pkg.capabilities.join(", ") || "none published"}
- Subpackages: ${pkg.subpackages.map((sub) => sub.import || sub.name || sub.id).join(", ") || "none published"}
- Updated: ${pkg.updatedAt || "not specified"}`;
    });
    return `# wago plugin registry: complete catalog

Source: ${ORIGIN}/
API: ${API}
Catalog generated: ${generated}
Published plugins: ${packages.length}

This document is generated from the same live registry snapshot as the static HTML pages and structured JSON.

${sections.join("\n\n")}
`;
}

async function emit(relPath, html) {
    const full = join(DIST, relPath, "index.html");
    await mkdir(dirname(full), { recursive: true });
    await writeFile(full, html);
}

async function loadPackages() {
    try {
        const response = await fetch(API, { headers: { accept: "application/json" } });
        if (response.ok) {
            const data = await response.json();
            if (Array.isArray(data?.packages) && data.packages.length) {
                console.log(`prerender: ${data.packages.length} packages from the live API`);
                return data.packages;
            }
        }
        console.warn(`prerender: API returned ${response.status}; trying committed index`);
    } catch (error) {
        console.warn(`prerender: API unreachable (${error.message}); trying committed index`);
    }
    const file = JSON.parse(await readFile(join(DIST, "data", "packages.json"), "utf8"));
    if (!Array.isArray(file.packages) || !file.packages.length) {
        throw new Error("no package data available from the live API or committed index");
    }
    console.log(`prerender: ${file.packages.length} packages from data/packages.json`);
    return file.packages;
}

async function main() {
    const template = await readFile(join(DIST, "index.html"), "utf8");
    if (!SEO_RE.test(template) || !CONTENT_RE.test(template)) {
        throw new Error("index.html is missing prerender SEO/content markers");
    }

    const rawPackages = await loadPackages();
    const packages = rawPackages.map(catalogPackage).filter((pkg) => pkg.id.includes("/"));
    if (packages.length !== rawPackages.length) {
        throw new Error(
            `only ${packages.length}/${rawPackages.length} packages have canonical owner/repository IDs`,
        );
    }
    packages.sort((a, b) => a.id.localeCompare(b.id));

    const generated = new Date().toISOString();
    const catalog = {
        schemaVersion: 1,
        generated,
        source: API,
        canonicalUrl: `${ORIGIN}/`,
        total: packages.length,
        packages,
    };
    await writeFile(join(DIST, "data", "catalog.json"), `${JSON.stringify(catalog, null, 2)}\n`);
    await writeFile(join(DIST, "llms.txt"), llmsSummary(packages, generated));
    await writeFile(join(DIST, "llms-full.txt"), llmsFull(packages, generated));

    const home = page(
        template,
        {
            title: "wago plugins — registry for the wago WebAssembly engine",
            description:
                "Browse Go plugins, runtime services, host integrations, and compiler extensions for the wago WebAssembly engine.",
            url: `${ORIGIN}/`,
            jsonLd: homeJsonLd(packages, generated),
        },
        homeContent(packages, generated),
    );
    await writeFile(join(DIST, "index.html"), home);

    const authors = new Map();
    const noteAuthor = (login, pkg) => {
        const cleanLogin = clean(login);
        if (!cleanLogin || RESERVED.has(cleanLogin.toLowerCase())) return;
        const current = authors.get(cleanLogin) || [];
        if (!current.some((candidate) => candidate.id === pkg.id)) current.push(pkg);
        authors.set(cleanLogin, current);
    };

    for (const pkg of packages) {
        const html = page(
            template,
            {
                title: `${pkg.id} | wago plugins`,
                description: pkg.description || `${pkg.id} — a plugin in the wago registry.`,
                url: pkg.url,
                jsonLd: packageJsonLd(pkg),
            },
            packageContent(pkg),
        );
        await emit(pathForID(pkg.id), html);
        noteAuthor(pkg.owner, pkg);
        for (const author of pkg.authors) noteAuthor(author.github, pkg);
        for (const contributor of pkg.contributors) noteAuthor(contributor, pkg);
    }

    for (const [login, ownedPackages] of authors) {
        const url = `${ORIGIN}/${encodeURIComponent(login)}`;
        const html = page(
            template,
            {
                title: `@${login} | wago plugins`,
                description: `${login}'s packages on the wago plugin registry.`,
                url,
                image: `https://github.com/${encodeURIComponent(login)}.png`,
                type: "profile",
                jsonLd: authorJsonLd(login, ownedPackages),
            },
            authorContent(login, ownedPackages),
        );
        await emit(encodeURIComponent(login), html);
    }

    const sitemapEntries = [
        { url: `${ORIGIN}/` },
        { url: `${ORIGIN}/search` },
        { url: `${ORIGIN}/llms.txt` },
        { url: `${ORIGIN}/llms-full.txt` },
        { url: `${ORIGIN}/data/catalog.json` },
        ...packages.map((pkg) => ({ url: pkg.url, lastmod: pkg.updatedAt })),
        ...[...authors.keys()].map((login) => ({
            url: `${ORIGIN}/${encodeURIComponent(login)}`,
        })),
    ];
    await writeFile(join(DIST, "sitemap.xml"), sitemap(sitemapEntries));
    await writeFile(
        join(DIST, "robots.txt"),
        `# LLM-readable registry: ${ORIGIN}/llms.txt\n# Structured catalog: ${ORIGIN}/data/catalog.json\nUser-agent: *\nAllow: /\n\nSitemap: ${ORIGIN}/sitemap.xml\n`,
    );

    console.log(
        `prerender: wrote ${packages.length} package pages + ${authors.size} author pages + crawler metadata`,
    );
}

main().catch((error) => {
    console.error("prerender: failed:", error);
    process.exitCode = 1;
});
