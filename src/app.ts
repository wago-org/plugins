// App orchestrator: owns the render loop, the hash router (via history
// pushState / popstate), and a single delegated event layer. Screens are pure;
// everything that mutates state and asks for a re-render lives here.

import * as api from "./api.js";
import { CHECK_ICON, COPY_ICON, copyFrom, copyText } from "./copy.js";
import * as github from "./github.js";
import { initMarkdown } from "./markdown.js";
import {
    accountScreen,
    authScreen,
    footer,
    homeScreen,
    nav,
    notificationsScreen,
    packageScreen,
    publisherDropdownHtml,
    transferDropdownHtml,
    searchRows,
    searchScreen,
    searchSummary,
    userScreen,
} from "./screens.js";
import { findPackage, state } from "./state.js";
import type { AcctTab, PkgTab, Sort } from "./state.js";
import type { ViewUser } from "./types.js";
import { pkgPath } from "./util.js";

const root = (): HTMLElement => document.getElementById("app")!;

// ── render ───────────────────────────────────────────────────────────────────

function screenBody(): string {
    if (!state.registry) {
        return `<div style="padding:120px 0;text-align:center;color:#7d72b0;font-size:15px">Loading registry…</div>`;
    }
    switch (state.screen) {
        case "search":
            return searchScreen(state);
        case "package":
            return state.pkg ? packageScreen(state) : homeScreen(state);
        case "auth":
            return authScreen(state);
        case "account":
            return state.user ? accountScreen(state) : authScreen(state);
        case "notifications":
            return state.user ? notificationsScreen(state) : authScreen(state);
        case "user":
            return userScreen(state);
        default:
            return homeScreen(state);
    }
}

function render(): void {
    applyMeta();
    root().innerHTML = nav(state) + screenBody() + footer(state);
    enhanceCodeBlocks();
    enhanceSparkline();
}

const SITE = "Plugins";
const SITE_ORIGIN = "https://plugins.wago.sh";

// currentMeta derives the tab title + social/canonical metadata for the current
// screen. (This keeps the SPA's tab title and in-app OG correct; crawlers get the
// prerendered per-page tags baked into each package/author HTML at build time.)
function currentMeta(): { title: string; description: string; url: string; image: string } {
    const image = `${SITE_ORIGIN}/assets/wago-logo.png`;
    const url = SITE_ORIGIN + location.pathname + location.search;
    if (state.screen === "package" && state.pkg) {
        const p = state.pkg;
        return {
            title: `${p.short} | ${SITE}`,
            description: p.description || `${p.short} — a plugin on the wago registry.`,
            url,
            image,
        };
    }
    if (state.screen === "user" && state.viewUser) {
        const u = state.viewUser;
        return {
            title: `@${u.login} | ${SITE}`,
            description: u.bio || `${u.name || u.login} on the wago plugin registry.`,
            url,
            image: u.avatarUrl || image,
        };
    }
    if (state.screen === "account" && state.user) {
        return { title: `@${state.user.login} | ${SITE}`, description: "Your wago account.", url, image };
    }
    if (state.screen === "notifications") {
        return { title: `Notifications | ${SITE}`, description: "Your wago notifications.", url, image };
    }
    if (state.screen === "search") {
        const t = state.query ? `${state.query} | Search | ${SITE}` : `Search | ${SITE}`;
        return { title: t, description: "Search the wago plugin registry.", url, image };
    }
    return {
        title: `Home | ${SITE}`,
        description: "The wago plugin registry — host-import bundles, WASI shims, debuggers and codegen backends. Drop-in Go modules for the wago WebAssembly engine.",
        url: `${SITE_ORIGIN}/`,
        image,
    };
}

// applyMeta updates document.title and the og/twitter/canonical/description tags
// to match the current screen.
function applyMeta(): void {
    const m = currentMeta();
    document.title = m.title;
    setMetaTag("description", m.description, "name");
    setMetaTag("og:title", m.title);
    setMetaTag("og:description", m.description);
    setMetaTag("og:url", m.url);
    setMetaTag("og:image", m.image);
    setMetaTag("twitter:title", m.title, "name");
    setMetaTag("twitter:description", m.description, "name");
    setMetaTag("twitter:image", m.image, "name");
    const link = document.head.querySelector<HTMLLinkElement>('link[rel="canonical"]');
    if (link) link.href = m.url;
}

function setMetaTag(key: string, content: string, attr: "property" | "name" = "property"): void {
    let el = document.head.querySelector<HTMLMetaElement>(`meta[${attr}="${CSS.escape(key)}"]`);
    if (!el) {
        el = document.createElement("meta");
        el.setAttribute(attr, key);
        document.head.appendChild(el);
    }
    el.setAttribute("content", content);
}

interface SparkPt {
    x: number;
    y: number;
    c: number;
    d: string;
}

// enhanceSparkline adds an npm-style hover readout to the installs chart: a guide
// line + dot follow the cursor to the nearest weekly point, and a tooltip shows
// that week's install total and start date. Per-point data is embedded in the
// chart's data-spark attribute (viewBox coords + the week's total/start-date).
function enhanceSparkline(): void {
    const wrap = root().querySelector<HTMLElement>(".spark-wrap");
    if (!wrap || wrap.dataset.enh === "1") return;
    let pts: SparkPt[];
    try {
        pts = JSON.parse(wrap.dataset.spark || "[]") as SparkPt[];
    } catch {
        return;
    }
    if (!pts.length) return;
    wrap.dataset.enh = "1";

    const line = document.createElement("div");
    Object.assign(line.style, { position: "absolute", top: "0", bottom: "0", width: "1px", background: "#443a8c", pointerEvents: "none", display: "none", transform: "translateX(-0.5px)" });
    const dot = document.createElement("div");
    Object.assign(dot.style, { position: "absolute", width: "8px", height: "8px", borderRadius: "50%", background: "#c3a8ff", border: "2px solid #221c52", pointerEvents: "none", display: "none", transform: "translate(-50%,-50%)" });
    const tip = document.createElement("div");
    Object.assign(tip.style, { position: "absolute", top: "-2px", transform: "translate(-50%,-100%)", background: "#25205a", border: "1px solid #443a8c", borderRadius: "7px", padding: "3px 8px", fontFamily: "'JetBrains Mono', monospace", fontSize: "11px", fontWeight: "700", color: "#f3effd", whiteSpace: "nowrap", pointerEvents: "none", display: "none", zIndex: "5", boxShadow: "0 8px 20px -8px rgba(0,0,0,.6)" });
    wrap.append(line, dot, tip);

    wrap.addEventListener("mousemove", (e) => {
        const r = wrap.getBoundingClientRect();
        if (!r.width) return;
        const vx = ((e.clientX - r.left) / r.width) * 100;
        let best = pts[0];
        for (const pt of pts) if (Math.abs(pt.x - vx) < Math.abs(best.x - vx)) best = pt;
        const px = (best.x / 100) * r.width;
        const py = (best.y / 40) * r.height;
        line.style.left = `${px}px`;
        line.style.display = "block";
        dot.style.left = `${px}px`;
        dot.style.top = `${py}px`;
        dot.style.display = "block";
        tip.textContent = `${best.c.toLocaleString()} install${best.c === 1 ? "" : "s"} · week of ${fmtShortDate(best.d)}`;
        tip.style.display = "block";
        const half = tip.offsetWidth / 2;
        tip.style.left = `${Math.max(half, Math.min(px, r.width - half))}px`;
    });
    wrap.addEventListener("mouseleave", () => {
        line.style.display = "none";
        dot.style.display = "none";
        tip.style.display = "none";
    });
}

// fmtShortDate turns "2026-06-01" into "Jun 1".
function fmtShortDate(iso: string): string {
    const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(iso);
    if (!m) return iso;
    const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
    return `${months[+m[2] - 1]} ${+m[3]}`;
}

// enhanceCodeBlocks adds a GitHub-style copy button to the top-right of every
// rendered code block. The button is injected by trusted JS (not baked into the
// sanitized markdown). Icons are shared with the install-command copy button.
function enhanceCodeBlocks(): void {
    root()
        .querySelectorAll<HTMLElement>(".md pre")
        .forEach((pre) => {
            if (pre.querySelector(".code-copy")) return; // already enhanced
            const code = pre.querySelector("code");
            const text = code?.textContent ?? pre.textContent ?? "";
            if (!text.trim()) return;
            const btn = document.createElement("button");
            btn.type = "button";
            btn.className = "code-copy";
            btn.title = "Copy code";
            btn.setAttribute("aria-label", "Copy code");
            btn.innerHTML = COPY_ICON;
            let reset: ReturnType<typeof setTimeout> | undefined;
            btn.addEventListener("click", async (e) => {
                e.preventDefault();
                const okCopy = await copyText(text);
                btn.innerHTML = okCopy ? CHECK_ICON : COPY_ICON;
                btn.classList.toggle("copied", okCopy);
                clearTimeout(reset);
                reset = setTimeout(() => {
                    btn.innerHTML = COPY_ICON;
                    btn.classList.remove("copied");
                }, 1400);
            });
            pre.appendChild(btn);
        });
}

// ── router ───────────────────────────────────────────────────────────────────

function pushUrl(path: string): void {
    if (location.pathname + location.search !== path) history.pushState(null, "", path || "/");
}

// Rebuild screen-level state from the current URL path (used on load and on
// browser back/forward). Ephemeral filters/tabs intentionally reset here.
async function route(): Promise<void> {
    const parts = location.pathname.split("/").filter(Boolean).map(decodeURIComponent);
    const params = new URLSearchParams(location.search);

    if (parts.length === 0) {
        state.screen = "home";
        render();
        return;
    }
    if (parts[0] === "search") {
        state.screen = "search";
        state.query = params.get("q") || state.query;
        render();
        return;
    }
    if (parts[0] === "auth") {
        state.screen = "auth";
        render();
        return;
    }
    if (parts[0] === "account" || parts[0] === "settings") {
        if (!state.user) {
            state.screen = "auth";
            render();
            return;
        }
        const tab = (parts[0] === "settings" ? "settings" : (params.get("tab") as AcctTab)) || "profile";
        showAccount(tab, false);
        return;
    }
    if (parts[0] === "notifications") {
        if (!state.user) {
            state.screen = "auth";
            render();
            return;
        }
        showNotifications(false);
        return;
    }
    // Two segments are the canonical GitHub-relative package ID: /owner/repository.
    if (parts.length >= 2) {
        await openPackage(`${parts[0]}/${parts[1]}`, false);
        if (parts.length >= 3) openSub(decodeURIComponent(parts[2]), false);
        return;
    }
    // Single segment = your own account (when it's your login), else a user/org.
    const name = parts[0];
    if (state.user && name.toLowerCase() === state.user.login.toLowerCase()) {
        showAccount((params.get("tab") as AcctTab) || "profile", false);
        return;
    }
    await openProfile(name, false);
}

// ── navigation helpers ───────────────────────────────────────────────────────

function navHome(): void {
    state.screen = "home";
    state.menuOpen = false;
    pushUrl("/");
    render();
    scrollTop();
}

function navAuth(): void {
    state.screen = "auth";
    state.authError = null;
    pushUrl("/auth");
    render();
    scrollTop();
}

function navSearch(): void {
    state.screen = "search";
    const q = state.query.trim();
    pushUrl(q ? `/search?q=${encodeURIComponent(q)}` : "/search");
    render();
    scrollTop();
}

// ── realtime search ───────────────────────────────────────────────────────────

let searchTimer: ReturnType<typeof setTimeout> | undefined;

// Called on every keystroke in a search box. On the search screen we patch only
// the results + summary in place (so the input never loses focus) and keep the
// ?q= in the URL current via replaceState. Elsewhere (nav bar / home hero) the
// first keystroke jumps to the search screen, carrying focus with it.
function onQueryInput(el: HTMLInputElement): void {
    if (state.screen === "search") {
        if (searchTimer) clearTimeout(searchTimer);
        searchTimer = setTimeout(() => {
            const rows = document.getElementById("pkg-results");
            const summary = document.getElementById("search-summary");
            if (rows) rows.innerHTML = searchRows(state);
            if (summary) summary.innerHTML = searchSummary(state);
            const q = state.query.trim();
            history.replaceState(null, "", q ? `/search?q=${encodeURIComponent(q)}` : "/search");
        }, 80);
        return;
    }
    // Coming from the home hero / a nav bar on another screen: switch to search,
    // then move focus to the (freshly rendered) nav search box at the caret.
    // Deferred to the next tick so the refocus lands after this input event (and
    // the re-render it triggers) has fully settled.
    const caret = el.selectionStart ?? state.query.length;
    navSearch();
    setTimeout(() => {
        const input = document.querySelector<HTMLInputElement>('nav input[data-act="query"]');
        if (!input) return;
        input.focus();
        const pos = Math.min(caret, input.value.length);
        try {
            input.setSelectionRange(pos, pos);
        } catch {
            /* some input types disallow setSelectionRange */
        }
    }, 0);
}

// Show the current user's account at /{login} (?tab= for non-profile tabs).
function showAccount(tab: AcctTab, push: boolean): void {
    if (!state.user) {
        navAuth();
        return;
    }
    state.screen = "account";
    state.acctTab = tab;
    state.savedShorts = api.getBookmarks();
    state.menuOpen = false;
    if (push) {
        const q = tab && tab !== "profile" ? `?tab=${tab}` : "";
        pushUrl(`/${encodeURIComponent(state.user.login)}${q}`);
    }
    render();
    scrollTop();
    void loadStars();
    if (tab === "reports") void loadReports();
}

// loadReports fetches the moderation queue (admins only), then re-renders if the
// Reports tab is still open.
async function loadReports(): Promise<void> {
    if (!state.user?.admin) return;
    try {
        state.reports = await api.loadReports();
    } catch {
        state.reports = [];
    }
    if (state.screen === "account" && state.acctTab === "reports") render();
}

async function doResolveReport(id: string): Promise<void> {
    try {
        await api.resolveReport(id);
        state.reports = (state.reports || []).map((r) => (r.id === id ? { ...r, resolved: true } : r));
    } catch {
        alert("Couldn't resolve that report.");
    }
    render();
}

function navAccount(tab: AcctTab): void {
    showAccount(tab, true);
}

// ── notifications ────────────────────────────────────────────────────────────

function navNotifications(): void {
    showNotifications(true);
}

function showNotifications(push: boolean): void {
    if (!state.user) {
        navAuth();
        return;
    }
    state.screen = "notifications";
    state.menuOpen = false;
    if (push) pushUrl("/notifications");
    render();
    scrollTop();
    void loadNotifications();
}

// loadNotifications fetches the signed-in user's inbox (drives the nav badge and
// the notifications screen), then re-renders.
async function loadNotifications(): Promise<void> {
    if (!state.user) return;
    try {
        state.notifications = await api.listNotifications();
    } catch {
        state.notifications = [];
    }
    render();
}

async function acceptNotification(id: string): Promise<void> {
    try {
        await api.acceptNotification(id);
    } catch {
        alert("Couldn't accept — it may have been withdrawn or already handled.");
    }
    await loadNotifications();
    // If we're looking at the affected package, refresh it so new rights show.
    if (state.screen === "package" && state.pkg) void openPackage(state.pkg.short, false);
}

async function declineNotification(id: string): Promise<void> {
    try {
        await api.declineNotification(id);
    } catch {
        alert("Couldn't decline that invite.");
    }
    await loadNotifications();
}

// Load the current user's starred packages (used by the profile stat and the
// "Your stars" tab). Fired whenever the account screen is shown.
async function loadStars(): Promise<void> {
    if (!state.user) return;
    try {
        state.starShorts = await api.myStars();
    } catch {
        state.starShorts = [];
    }
    if (state.screen === "account") render();
}

async function openPackage(short: string, push = true): Promise<void> {
    const pkg = findPackage(state.registry, short);
    if (!pkg) {
        navHome();
        return;
    }
    state.pkg = pkg;
    state.screen = "package";
    state.pkgTab = "readme";
    state.sub = null;
    state.readme = null;
    state.ghContributors = null;
    state.readmeBase = null;
    state.readmeLoading = true; // settled by enrichReadme (stored readme, GitHub fetch, or neither)
    state.composerOpen = false;
    state.draftRating = 0;
    state.hoverRating = 0;
    state.draftText = "";
    state.reviewPreview = false;
    state.reviewEditing = null;
    state.issueFilter = "open";
    state.ghIssues = null;
    state.ghIssuesLoading = false;
    state.ghIssuesError = false;
    state.reviews = [];
    state.reviewsSummary = { average: pkg.rating, count: pkg.ratingCount };
    state.reviewsLoading = false;
    state.comments = [];
    state.commentsLoading = false;
    state.commentDraft = "";
    state.commentPreview = false;
    state.replyTo = null;
    state.replyDraft = "";
    state.replyPreview = false;
    state.commentEditing = null;
    state.commentEditDraft = "";
    state.installSeries = [];
    // seed the star widget synchronously so the header renders immediately…
    state.starred = !!pkg.starred;
    state.starCount = pkg.stars;
    state.bookmarked = api.isBookmarked(pkg.short);
    if (push) pushUrl(pkgPath(pkg));
    render();
    scrollTop();
    // …then enrich from the backend (or local store) when available.
    const detail = await api.loadPackage(short, pkg);
    if (state.pkg !== pkg) return;
    state.pkg = detail;
    state.starred = !!detail.starred;
    state.starCount = detail.stars;
    render();
    enrichAvatars(); // author/contributor profile pics
    enrichGithubStars(); // real GitHub stargazer count
    enrichReadme(); // the repo's real README from GitHub
    enrichContributors(); // real repo contributors from GitHub
    // install history for the sidebar sparkline, and comments for the tab count.
    void api.loadInstalls(detail).then((r) => {
        if (state.pkg === detail) {
            state.installSeries = r.series;
            state.starCount = detail.stars;
            render();
        }
    });
    void refreshComments();
}

function scrollTop(): void {
    try {
        window.scrollTo(0, 0);
    } catch {
        /* non-browser */
    }
}

// ── public user / org profiles (/{login}) ────────────────────────────────────

// A display name for a login pulled from the registry (an author entry), else "".
function registryDisplayName(login: string): string {
    const key = login.toLowerCase();
    for (const p of state.registry?.packages || []) {
        for (const a of p.authors || []) {
            if ((a.github || "").toLowerCase() === key && a.name) return a.name;
        }
    }
    return "";
}

// Open a user or organization's wago profile. Shows a claimed member's profile
// when one exists, otherwise one generated from registry + GitHub public data.
// Enriched with the real GitHub avatar/bio and org memberships (user) or public
// members (org), fetched client-side and cached.
async function openProfile(login: string, push = true): Promise<void> {
    if (!login) return;
    state.screen = "user";
    state.menuOpen = false;
    const seedName = registryDisplayName(login) || login;
    // Seed synchronously so the header paints immediately with what we know.
    state.viewUser = { login, name: seedName, claimed: false };
    state.viewUserLoading = true;
    if (push) pushUrl(`/${encodeURIComponent(login)}`);
    render();
    scrollTop();

    const [claimed, gh] = await Promise.all([api.getPublicUser(login), github.fetchUser(login)]);
    if (state.viewUser?.login !== login) return; // navigated away meanwhile
    const isOrg = !!gh?.isOrg;

    const merged: ViewUser = {
        login,
        name: claimed?.name || gh?.name || seedName,
        avatarUrl: claimed?.avatarUrl || gh?.avatarUrl,
        bio: claimed?.bio || gh?.bio,
        company: claimed?.company,
        location: claimed?.location,
        blog: claimed?.blog,
        twitterUsername: claimed?.twitterUsername,
        htmlUrl: claimed?.htmlUrl || `https://github.com/${login}`,
        githubCreatedAt: claimed?.githubCreatedAt,
        createdAt: claimed?.createdAt,
        followers: claimed?.followers,
        following: claimed?.following,
        publicRepos: claimed?.publicRepos,
        starsGiven: claimed?.starsGiven,
        claimed: !!claimed,
        isOrg,
    };
    state.viewUser = merged;
    state.viewUserLoading = false;
    render();

    // Lazily load org memberships (for a person) or public members (for an org).
    void (isOrg ? github.fetchOrgMembers(login) : github.fetchOrgs(login)).then((accts) => {
        if (state.viewUser?.login !== login) return;
        if (isOrg) state.viewUser.members = accts;
        else state.viewUser.orgs = accts;
        render();
    });
}

// ── reviews ──────────────────────────────────────────────────────────────────

async function refreshReviews(): Promise<void> {
    const pkg = state.pkg;
    if (!pkg) return;
    state.reviewsLoading = true;
    render();
    try {
        const res = await api.loadReviews(pkg, state.user);
        if (state.pkg !== pkg) return;
        state.reviews = res.reviews;
        state.reviewsSummary = res.summary;
    } catch {
        state.reviews = [];
    } finally {
        state.reviewsLoading = false;
        render();
        enrichAvatars();
    }
}

// ── comments ─────────────────────────────────────────────────────────────────

async function refreshComments(): Promise<void> {
    const pkg = state.pkg;
    if (!pkg) return;
    state.commentsLoading = true;
    render();
    try {
        const list = await api.loadComments(pkg, state.user);
        if (state.pkg !== pkg) return;
        state.comments = list;
    } catch {
        state.comments = [];
    } finally {
        state.commentsLoading = false;
        render();
        enrichAvatars();
    }
}

// ── GitHub enrichment (client-side fetch, cached) ─────────────────────────────

// Collect every GitHub login on the current package view (authors, contributors,
// review + comment authors) and lazily fetch their avatars, re-rendering when a
// new one arrives. Cached hits cost nothing; requests are deduped in github.ts.
function enrichAvatars(): void {
    const logins: string[] = [];
    const p = state.pkg;
    if (p) {
        for (const a of p.authors || []) if (a.github) logins.push(a.github);
        for (const c of p.contributors || []) logins.push(c);
    }
    for (const r of state.reviews) if (r.login) logins.push(r.login);
    for (const c of state.comments) if (c.login) logins.push(c.login);
    github.ensureAvatars(logins, () => {
        if (state.screen === "package") render();
    });
}

// Show the package's real GitHub stargazer count (stars = GitHub stars). Uses
// the cached value immediately if present, then refreshes from the API. Failures
// (rate-limit / offline) leave the seed count in place.
// Fetch the package repo's real README from GitHub (client-side, cached) and show
// it in the Readme tab.
function enrichReadme(): void {
    const p = state.pkg;
    if (!p) return;
    const settle = () => {
        state.readmeLoading = false;
        if (state.screen === "package") render();
    };
    // Prefer the repository URL, but fall back to the module path itself — a Go
    // import path like "github.com/wago-org/wasi" already names the GitHub repo,
    // so the README resolves even when `repository` wasn't recorded.
    const repo = github.parseRepo(p.repository) || github.parseRepo(p.name);
    if (!repo) {
        settle();
        return;
    }
    // Pin to the published release's commit — a permalink, so the README matches
    // the version on the page rather than whatever HEAD happens to be.
    const commit = (p.versions.find((v) => v.latest) || p.versions[0])?.commit;
    state.readmeBase = { owner: repo.owner, repo: repo.repo, ref: commit || undefined };
    void github.fetchReadme(repo.owner, repo.repo, commit).then((md) => {
        if (state.pkg !== p) return;
        state.readme = md;
        settle();
    });
}

// enrichContributors loads the repo's real GitHub contributors (replacing the
// "who published on wago" list when available).
function enrichContributors(): void {
    const p = state.pkg;
    if (!p) return;
    const repo = github.parseRepo(p.repository) || github.parseRepo(p.name);
    if (!repo) return;
    void github.fetchContributors(repo.owner, repo.repo).then((list) => {
        if (state.pkg !== p || !list.length) return;
        state.ghContributors = list;
        if (state.screen === "package") render();
    });
}

function enrichGithubStars(): void {
    const p = state.pkg;
    if (!p) return;
    const repo = github.parseRepo(p.repository);
    if (!repo) return;
    const cached = github.starsFor(repo.owner, repo.repo);
    if (cached != null) {
        state.starCount = cached;
        p.stars = cached;
    }
    void github.fetchRepo(repo.owner, repo.repo).then((r) => {
        if (state.pkg !== p || !r) return;
        state.starCount = r.stars;
        p.stars = r.stars;
        if (typeof r.forks === "number") p.forks = r.forks;
        if (state.screen === "package") render();
    });
}

// enrichCatalogStars refreshes the catalog's star counts from GitHub so the home
// and search cards show real stargazers, not the seed baseline (which is stuck at
// the publish-time value). Each repo is fetched once and cached/deduped by
// github.ts; a re-render lands when the visible list changes.
function enrichCatalogStars(): void {
    const packages = state.registry?.packages;
    if (!packages) return;
    for (const p of packages) {
        const repo = github.parseRepo(p.repository) || github.parseRepo(p.name);
        if (!repo) continue;
        const cached = github.starsFor(repo.owner, repo.repo);
        if (cached != null) {
            p.stars = cached;
            continue;
        }
        void github.fetchRepo(repo.owner, repo.repo).then((r) => {
            if (!r) return;
            p.stars = r.stars;
            if (typeof r.forks === "number") p.forks = r.forks;
            if (state.screen === "home" || state.screen === "search") render();
        });
    }
}

// Sync the Issues tab from the live GitHub API for this package's repo.
async function syncIssues(): Promise<void> {
    const pkg = state.pkg;
    if (!pkg) return;
    const repo = github.parseRepo(pkg.repository);
    if (!repo) {
        state.ghIssuesError = true;
        render();
        return;
    }
    state.ghIssuesLoading = true;
    state.ghIssuesError = false;
    render();
    const issues = await github.fetchIssues(repo.owner, repo.repo);
    if (state.pkg !== pkg) return;
    state.ghIssuesLoading = false;
    if (issues === null) {
        // Rate-limited / error / unreachable — keep showing the seed sample.
        state.ghIssuesError = true;
    } else {
        state.ghIssues = issues;
    }
    render();
}

// ── star picker painting (in place, no full re-render on hover) ───────────────

function paintPicker(active: number): void {
    const picker = document.getElementById("star-picker");
    if (!picker) return;
    picker.querySelectorAll<HTMLElement>(".pick-star").forEach((el) => {
        const n = Number(el.getAttribute("data-n"));
        el.textContent = n <= active ? "★" : "☆";
        el.style.color = n <= active ? "#c3a8ff" : "#4b407e";
    });
}

// ── actions ──────────────────────────────────────────────────────────────────

function doSignIn(): void {
    // Always real GitHub OAuth via the backend.
    window.location.href = api.signInUrl(location.href);
}

async function doSignOut(): Promise<void> {
    await api.signOut();
    api.clearLocalState();
    state.user = null;
    state.menuOpen = false;
    // Hard-reload from the root so ALL in-memory + cached state is erased. The
    // next sign-in then goes through GitHub's account picker with a clean slate,
    // so switching accounts actually works.
    try {
        location.hash = "#/";
        location.reload();
    } catch {
        navHome();
    }
}

// Stars mirror the package's real GitHub stars. With permission (public_repo
// scope) we star the actual repo on the user's behalf via the backend. Without
// it, we ask once — the user can grant permission, or just be sent to GitHub to
// star manually — and remember a "don't ask again" preference.
const STAR_MODE_KEY = "wago.starMode"; // "" = ask · "manual" = open GitHub, don't ask
const PENDING_STAR_KEY = "wago.pendingStar"; // package short to star after granting

function starMode(): string {
    try {
        return localStorage.getItem(STAR_MODE_KEY) || "";
    } catch {
        return "";
    }
}
function rememberManualStar(): void {
    try {
        localStorage.setItem(STAR_MODE_KEY, "manual");
    } catch {
        /* storage disabled */
    }
}

async function toggleStar(): Promise<void> {
    if (!state.user) {
        navAuth();
        return;
    }
    const p = state.pkg;
    if (!p) return;
    const on = !state.starred;

    // Unstar: never prompt. Use the GitHub API when permitted, else drop it from
    // the local "Your stars" list.
    if (!on) {
        state.starred = false;
        render();
        if (state.user.canStar) void api.githubStar(p, false);
        else
            try {
                await api.setStar(p, false);
            } catch {
                /* ignore */
            }
        state.starShorts = null;
        render();
        return;
    }

    // Starring with permission → star the real repo directly, no redirect.
    if (state.user.canStar) {
        state.starred = true;
        render();
        const res = await api.githubStar(p, true);
        if (res === "need_permission") {
            // Token was revoked or lost scope — re-consent.
            openStarPrompt();
        } else {
            state.starShorts = null;
        }
        render();
        return;
    }

    // No permission yet: honor a saved "don't ask" preference, else ask once.
    if (starMode() === "manual") {
        void manualStar(p);
        return;
    }
    openStarPrompt();
}

function openStarPrompt(): void {
    state.starPrompt = true;
    state.starPromptDontAsk = false;
    render();
}

// Fallback (redirect): record the star locally for "Your stars" and open the
// repo on GitHub so the user can star it there.
async function manualStar(p: import("./types.js").Package): Promise<void> {
    state.starred = true;
    render();
    try {
        await api.setStar(p, true);
    } catch {
        /* ignore */
    }
    state.starShorts = null;
    try {
        window.open(p.repository, "_blank", "noopener");
    } catch {
        /* non-browser */
    }
    render();
}

// Redirect to GitHub to grant the star permission, remembering which package to
// star once we return with the upgraded scope.
function grantStarPermission(): void {
    const p = state.pkg;
    if (p) {
        try {
            localStorage.setItem(PENDING_STAR_KEY, p.short);
        } catch {
            /* storage disabled */
        }
    }
    window.location.href = api.signInUrl(location.href, true);
}

// After returning from a permission grant, finish the star the user intended.
async function completePendingStar(): Promise<void> {
    if (!state.user?.canStar) return;
    let short = "";
    try {
        short = localStorage.getItem(PENDING_STAR_KEY) || "";
    } catch {
        /* ignore */
    }
    if (!short) return;
    try {
        localStorage.removeItem(PENDING_STAR_KEY);
    } catch {
        /* ignore */
    }
    const pkg = findPackage(state.registry, short);
    // Await so the star is recorded before route() loads the package detail —
    // otherwise the page can render unstarred (detail fetched mid-flight).
    if (pkg) await api.githubStar(pkg, true);
}

async function submitReview(): Promise<void> {
    if (!state.user) {
        navAuth();
        return;
    }
    if (!state.pkg) return;
    const text = state.draftText.trim();
    if (!text) return;
    const rating = state.draftRating || 5;
    await api.postReview(state.pkg, state.user, rating, text);
    state.draftText = "";
    state.draftRating = 0;
    state.hoverRating = 0;
    state.composerOpen = false;
    state.reviewPreview = false;
    state.reviewEditing = null;
    await refreshReviews();
}

// Open the review composer prefilled with the user's own review, to edit it.
// Submitting upserts (replaces) it.
function editReviewOpen(id: string): void {
    const r = state.reviews.find((x) => x.id === id);
    if (!r || !r.mine) return;
    state.composerOpen = true;
    state.reviewEditing = id;
    state.draftRating = r.rating;
    state.hoverRating = 0;
    state.draftText = r.body;
    state.reviewPreview = false;
    render();
}

async function deleteReviewAction(id: string): Promise<void> {
    if (!state.user || !state.pkg) return;
    const r = state.reviews.find((x) => x.id === id);
    if (!r || !r.mine) return;
    await api.deleteReview(state.pkg, id);
    if (state.reviewEditing === id) {
        state.reviewEditing = null;
        state.composerOpen = false;
        state.draftText = "";
        state.draftRating = 0;
    }
    await refreshReviews();
}

async function voteOnReview(id: string, dir: "up" | "down"): Promise<void> {
    if (!state.user) {
        navAuth();
        return;
    }
    const r = state.reviews.find((x) => x.id === id);
    if (!r || r.mine) return;
    const next = r.myVote === dir ? null : dir;
    await api.voteReview(id, next);
    await refreshReviews();
}

async function voteOnComment(id: string, dir: "up" | "down"): Promise<void> {
    if (!state.user) {
        navAuth();
        return;
    }
    const c = state.comments.find((x) => x.id === id);
    if (!c) return;
    const next = c.myVote === dir ? null : dir;
    await api.voteComment(id, next);
    await refreshComments();
}

async function submitComment(): Promise<void> {
    if (!state.user) {
        navAuth();
        return;
    }
    if (!state.pkg) return;
    const text = state.commentDraft.trim();
    if (!text) return;
    await api.postComment(state.pkg, state.user, text);
    state.commentDraft = "";
    state.commentPreview = false;
    await refreshComments();
}

async function submitReply(parentId: string): Promise<void> {
    if (!state.user || !state.pkg) return;
    const text = state.replyDraft.trim();
    if (!text) return;
    await api.postComment(state.pkg, state.user, text, parentId);
    state.replyTo = null;
    state.replyDraft = "";
    state.replyPreview = false;
    await refreshComments();
}

async function removeComment(id: string): Promise<void> {
    if (!state.pkg) return;
    await api.deleteComment(state.pkg, id);
    await refreshComments();
}

// Hide (archived=true) or restore a comment. Allowed for the author or a
// moderator (package/org owner); the server enforces the actual permission.
async function setCommentArchived(id: string, archived: boolean): Promise<void> {
    if (!state.user || !state.pkg) {
        if (!state.user) navAuth();
        return;
    }
    const c = state.comments.find((x) => x.id === id);
    if (!c || !(c.mine || c.canModerate)) return;
    await api.archiveComment(state.pkg, id, archived);
    await refreshComments();
}

function editCommentOpen(id: string): void {
    const c = state.comments.find((x) => x.id === id);
    if (!c || !c.mine) return;
    state.commentEditing = id;
    state.commentEditDraft = c.body;
    render();
}

async function saveCommentEdit(id: string): Promise<void> {
    if (!state.pkg) return;
    const text = state.commentEditDraft.trim();
    if (!text) return;
    await api.editComment(state.pkg, id, text);
    state.commentEditing = null;
    state.commentEditDraft = "";
    await refreshComments();
}

// ── secondary emails ─────────────────────────────────────────────────────────

// Sync = re-run OAuth. GitHub silently re-authorizes an already-approved app and
// the callback refreshes the stored profile (added emails are preserved).
function syncFromGithub(): void {
    window.location.href = api.signInUrl(location.href);
}

async function addEmail(): Promise<void> {
    if (!state.user) {
        navAuth();
        return;
    }
    const email = state.emailDraft.trim();
    if (!email) return;
    state.emailMsg = null;
    try {
        const res = await api.addEmail(email);
        state.emailDraft = "";
        if (state.user) state.user.emails = await api.listEmails();
        state.emailMsg = res.sent
            ? `We emailed a 6-digit code to ${email}.`
            : `Added ${email}. Enter the code we sent to verify.`;
    } catch (err) {
        state.emailMsg = `Couldn't add that email: ${String(err instanceof Error ? err.message : err)}`;
    }
    render();
}

async function verifyEmail(address: string): Promise<void> {
    const code = (state.verifyDrafts[address] || "").trim();
    if (!code) return;
    state.emailMsg = null;
    try {
        if (state.user) state.user.emails = await api.verifyEmail(address, code);
        delete state.verifyDrafts[address];
        state.emailMsg = `${address} is verified.`;
    } catch {
        state.emailMsg = `That code didn't match. Check it and try again.`;
    }
    render();
}

async function deleteEmail(address: string): Promise<void> {
    try {
        if (state.user) state.user.emails = await api.deleteEmail(address);
        delete state.verifyDrafts[address];
        state.emailMsg = `Removed ${address}.`;
    } catch {
        /* ignore */
    }
    render();
}

function setPkgTab(tab: PkgTab): void {
    state.pkgTab = tab;
    // Selecting any tab leaves an open subpackage page and returns to the package
    // URL. A tab switch pushes the package path so the sub URL is left behind.
    if (state.sub) {
        state.sub = null;
        if (state.pkg) pushUrl(pkgPath(state.pkg));
    }
    render();
    if (tab === "reviews" && state.reviews.length === 0 && !state.reviewsLoading) {
        void refreshReviews();
    }
    if (tab === "comments" && state.comments.length === 0 && !state.commentsLoading) {
        void refreshComments();
    }
}

// Open a subpackage's page (its readme) at /{owner}/{repository}/{id}. `push` is
// false when arriving via the router (URL already correct).
function openSub(id: string, push = true): void {
    if (!state.pkg) return;
    const e = state.pkg.subpackages.find((x) => x.id === id);
    if (!e) return;
    state.sub = id;
    if (push) pushUrl(`${pkgPath(state.pkg)}/${encodeURIComponent(id)}`);
    render();
    scrollTop();
}

// ── event delegation ─────────────────────────────────────────────────────────

// submitReport sends the current report modal to the backend, then shows the
// "sent" confirmation (or leaves the modal open to retry on failure).
async function submitReport(): Promise<void> {
    if (!state.pkg || !state.reportReason || state.reportSending) return;
    state.reportSending = true;
    render();
    try {
        await api.reportPackage(state.pkg.short, state.reportReason, state.reportDetail);
        state.reportDone = true;
    } catch {
        // Keep the form open so the user can try again.
    }
    state.reportSending = false;
    render();
}

// doTakedown removes a package (admins). Confirms first, then returns home.
async function doTakedown(): Promise<void> {
    const p = state.pkg;
    if (!p) return;
    if (!confirm(`Take down "${p.short}"? This removes the package from the registry for everyone.`)) return;
    try {
        await api.takedownPackage(p.short);
        navHome();
    } catch {
        alert("Takedown failed — you may not have permission, or the server errored.");
    }
}

// savePublishers persists the package's allowed-publisher list and updates state.
async function savePublishers(next: string[]): Promise<void> {
    const p = state.pkg;
    if (!p) return;
    try {
        const updated = await api.setPublishers(p.short, next);
        if (state.pkg?.short === p.short) state.pkg.allowedPublishers = updated.allowedPublishers || [];
    } catch {
        alert("Couldn't update publishers — you may not have permission.");
    }
    render();
}

// onPublisherInput debounces a GitHub user search and patches just the dropdown
// slot (a full render would steal the input's focus).
let publisherSearchTimer: ReturnType<typeof setTimeout> | undefined;
function onPublisherInput(): void {
    clearTimeout(publisherSearchTimer);
    const q = state.publisherDraft.trim();
    if (q.length < 2) {
        state.publisherResults = [];
        patchPublisherDropdown();
        return;
    }
    publisherSearchTimer = setTimeout(() => {
        void github.searchUsers(q).then((results) => {
            if (state.publisherDraft.trim() !== q) return; // draft moved on
            state.publisherResults = results;
            patchPublisherDropdown();
        });
    }, 280);
}

// patchPublisherDropdown re-renders only the autocomplete slot in place.
function patchPublisherDropdown(): void {
    const el = root().querySelector<HTMLElement>(".pub-dropdown");
    if (el) el.innerHTML = publisherDropdownHtml(state);
}

// onTransferInput debounces a GitHub user search for the transfer-owner field and
// patches just its dropdown slot (mirrors the publisher search).
let transferSearchTimer: ReturnType<typeof setTimeout> | undefined;
function onTransferInput(): void {
    clearTimeout(transferSearchTimer);
    const q = state.transferDraft.trim();
    if (q.length < 2) {
        state.transferResults = [];
        patchTransferDropdown();
        return;
    }
    transferSearchTimer = setTimeout(() => {
        void github.searchUsers(q).then((results) => {
            if (state.transferDraft.trim() !== q) return; // draft moved on
            state.transferResults = results;
            patchTransferDropdown();
        });
    }, 280);
}

function patchTransferDropdown(): void {
    const el = root().querySelector<HTMLElement>(".transfer-dropdown");
    if (el) el.innerHTML = transferDropdownHtml(state);
}

// pickTransferOwner fills the transfer field with a login chosen from the
// dropdown (ownership is deliberate — the user still confirms via Transfer).
function pickTransferOwner(login: string): void {
    state.transferDraft = login;
    state.transferResults = [];
    render();
}

function clearTransferSearch(): void {
    if (!state.transferResults.length) return;
    state.transferResults = [];
    patchTransferDropdown();
}

// sendPublishInvite creates a pending publish invite for a login. They must
// accept it (in their notifications) before they can publish; until then it
// shows as a pending chip.
async function sendPublishInvite(login: string): Promise<void> {
    const p = state.pkg;
    if (!p || !login) return;
    try {
        const updated = await api.invitePublisher(p.short, login);
        if (state.pkg?.short === p.short) {
            state.pkg.allowedPublishers = updated.allowedPublishers || [];
            state.pkg.pendingPublishers = updated.pendingPublishers || [];
        }
    } catch {
        alert(`Couldn't invite @${login} — they may already publish this package or have a pending invite.`);
    }
    render();
}

async function addPublisher(): Promise<void> {
    const p = state.pkg;
    const login = state.publisherDraft.trim().replace(/^@/, "");
    if (!p || !login) return await clearPublisherSearch(true);
    state.publisherDraft = "";
    state.publisherResults = [];
    await sendPublishInvite(login);
}

// pickPublisher invites a login chosen from the autocomplete dropdown.
async function pickPublisher(login: string): Promise<void> {
    const p = state.pkg;
    if (!p) return;
    state.publisherDraft = "";
    state.publisherResults = [];
    await sendPublishInvite(login);
}

// cancelPublisherInvite withdraws a pending publish invite (declines it as the
// package manager who sent it).
async function cancelPublisherInvite(id: string): Promise<void> {
    if (!state.pkg) return;
    try {
        await api.declineNotification(id);
        if (state.pkg) {
            state.pkg.pendingPublishers = (state.pkg.pendingPublishers || []).filter((x) => x.id !== id);
        }
    } catch {
        alert("Couldn't cancel that invite.");
    }
    render();
}

// clearPublisherSearch drops any open autocomplete (and optionally re-renders).
async function clearPublisherSearch(rerender = false): Promise<void> {
    if (!state.publisherResults.length && !rerender) return;
    state.publisherResults = [];
    if (rerender) render();
    else patchPublisherDropdown();
}

async function removePublisher(login: string): Promise<void> {
    const p = state.pkg;
    if (!p) return;
    await savePublishers((p.allowedPublishers || []).filter((x) => x.toLowerCase() !== login.toLowerCase()));
}

// setDeprecated marks the package deprecated (message) or undoes it, then refreshes.
async function setDeprecated(message: string, undo: boolean): Promise<void> {
    const p = state.pkg;
    if (!p) return;
    try {
        const updated = await api.deprecatePackage(p.short, message, undo);
        if (state.pkg?.short === p.short) {
            state.pkg.deprecatedMessage = updated.deprecatedMessage;
            state.deprecateDraft = "";
        }
    } catch {
        alert("Couldn't update deprecation — you may not have permission.");
    }
    render();
}

// transferPackage moves ownership to the destination in the transfer field.
// Transferring to your own login or the org that owns the source repo applies
// immediately; any other account gets a pending invite it must accept. We honor
// the backend's canManage in the reply (you may lose control after an org move).
async function transferPackage(): Promise<void> {
    const p = state.pkg;
    if (!p) return;
    const owner = state.transferDraft.trim().replace(/^@/, "");
    if (!owner || owner.toLowerCase() === (p.ownerLogin || "").toLowerCase()) return;
    if (!confirm(`Transfer "${p.short}" to @${owner}? If it's not the repo's org, they'll get an invite to accept.`)) return;
    try {
        const { pkg: updated, invited } = await api.transferPackage(p.short, owner);
        state.transferDraft = "";
        if (invited) {
            alert(`Invite sent — @${invited} will become the owner once they accept it in their notifications.`);
        } else if (state.pkg?.short === p.short) {
            state.pkg.ownerLogin = updated.ownerLogin;
            state.pkg.canManage = updated.canManage;
            state.pkg.allowedPublishers = updated.allowedPublishers || [];
            state.pkg.pendingPublishers = updated.pendingPublishers || [];
        }
    } catch {
        alert("Couldn't transfer — check the destination login and that you're an owner/admin of it.");
    }
    render();
}

function dispatch(act: string, arg: string | null, el: HTMLElement): void {
    switch (act) {
        case "home":
            navHome();
            break;
        case "auth":
            navAuth();
            break;
        case "signin":
            void doSignIn();
            break;
        case "signout":
            void doSignOut();
            break;
        case "menu-toggle":
            state.menuOpen = !state.menuOpen;
            render();
            break;
        case "acct":
            navAccount((arg as AcctTab) || "profile");
            break;
        case "acct-tab":
            state.acctTab = (arg as AcctTab) || "profile";
            state.savedShorts = api.getBookmarks();
            render();
            if (state.acctTab === "reports") void loadReports();
            break;
        case "resolve-report":
            if (arg) void doResolveReport(arg);
            break;
        case "unsave":
            if (arg) {
                api.setBookmark(arg, false);
                state.savedShorts = api.getBookmarks();
                if (state.pkg?.short === arg) state.bookmarked = false;
                render();
            }
            break;
        case "open":
            if (arg) void openPackage(arg);
            break;
        case "user":
            if (arg) void openProfile(arg);
            break;
        case "search":
            navSearch();
            break;
        case "cat":
            if (arg) {
                state.cats = { [arg]: true };
                state.query = "";
                state.verified = false;
                navSearch();
            }
            break;
        case "kw":
            if (arg) {
                state.cats = {};
                state.verified = false;
                state.query = arg;
                navSearch();
            }
            break;
        case "filter-cat":
            if (arg) {
                state.cats = { ...state.cats, [arg]: !state.cats[arg] };
                render();
            }
            break;
        case "toggle-verified":
            state.verified = !state.verified;
            render();
            break;
        case "sort":
            state.sort = (arg as Sort) || "popular";
            render();
            break;
        case "tab":
            setPkgTab((arg as PkgTab) || "readme");
            break;
        case "open-sub":
            if (arg) openSub(arg);
            break;
        case "star":
            void toggleStar();
            break;
        case "star-allow":
            grantStarPermission();
            break;
        case "star-just-github":
            if (state.starPromptDontAsk) rememberManualStar();
            state.starPrompt = false;
            if (state.pkg) void manualStar(state.pkg);
            break;
        case "star-cancel":
            state.starPrompt = false;
            state.starPromptDontAsk = false;
            render();
            break;
        case "star-dontask":
            state.starPromptDontAsk = !state.starPromptDontAsk;
            render();
            break;
        case "bookmark":
            if (state.pkg) {
                state.bookmarked = !state.bookmarked;
                api.setBookmark(state.pkg.short, state.bookmarked);
                render();
            }
            break;
        case "report-open":
            state.reportOpen = true;
            state.reportReason = "";
            state.reportDetail = "";
            state.reportDone = false;
            state.reportSending = false;
            render();
            break;
        case "report-reason":
            state.reportReason = arg || "";
            render();
            break;
        case "report-cancel":
            state.reportOpen = false;
            state.reportSending = false;
            render();
            break;
        case "report-submit":
            void submitReport();
            break;
        case "takedown":
            void doTakedown();
            break;
        case "publisher-add":
            void addPublisher();
            break;
        case "publisher-remove":
            if (arg) void removePublisher(arg);
            break;
        case "publisher-cancel":
            if (arg) void cancelPublisherInvite(arg);
            break;
        case "publisher-pick":
            if (arg) void pickPublisher(arg);
            break;
        case "notifications":
            navNotifications();
            break;
        case "notif-accept":
            if (arg) void acceptNotification(arg);
            break;
        case "notif-decline":
            if (arg) void declineNotification(arg);
            break;
        case "deprecate":
            void setDeprecated(state.deprecateDraft.trim(), false);
            break;
        case "undeprecate":
            void setDeprecated("", true);
            break;
        case "transfer":
            void transferPackage();
            break;
        case "transfer-pick":
            if (arg) pickTransferOwner(arg);
            break;
        case "composer-open":
            if (!state.user) {
                navAuth();
                break;
            }
            state.composerOpen = true;
            render();
            break;
        case "composer-close":
            state.composerOpen = false;
            state.draftText = "";
            state.draftRating = 0;
            state.hoverRating = 0;
            state.reviewPreview = false;
            state.reviewEditing = null;
            render();
            break;
        case "review-edit":
            if (arg) editReviewOpen(arg);
            break;
        case "review-delete":
            if (arg) void deleteReviewAction(arg);
            break;
        case "rate":
            state.draftRating = Number(arg) || 0;
            state.hoverRating = 0;
            paintPicker(state.draftRating);
            break;
        case "review-submit":
            void submitReview();
            break;
        case "issue-filter":
            state.issueFilter = arg === "closed" ? "closed" : "open";
            render();
            break;
        case "vote-up":
            if (arg) void voteOnReview(arg, "up");
            break;
        case "vote-down":
            if (arg) void voteOnReview(arg, "down");
            break;
        case "comment-vote-up":
            if (arg) void voteOnComment(arg, "up");
            break;
        case "comment-vote-down":
            if (arg) void voteOnComment(arg, "down");
            break;
        case "review-write":
            state.reviewPreview = false;
            render();
            break;
        case "review-preview":
            state.reviewPreview = true;
            render();
            break;
        case "comment-write":
            state.commentPreview = false;
            render();
            break;
        case "comment-preview":
            state.commentPreview = true;
            render();
            break;
        case "reply-write":
            state.replyPreview = false;
            render();
            break;
        case "reply-preview":
            state.replyPreview = true;
            render();
            break;
        case "sync-issues":
            void syncIssues();
            break;
        case "comment-submit":
            void submitComment();
            break;
        case "reply-open":
            if (!state.user) {
                navAuth();
                break;
            }
            state.replyTo = arg;
            state.replyDraft = "";
            render();
            break;
        case "reply-cancel":
            state.replyTo = null;
            state.replyDraft = "";
            state.replyPreview = false;
            render();
            break;
        case "reply-submit":
            if (arg) void submitReply(arg);
            break;
        case "comment-delete":
            if (arg) void removeComment(arg);
            break;
        case "comment-archive":
            if (arg) void setCommentArchived(arg, true);
            break;
        case "comment-unarchive":
            if (arg) void setCommentArchived(arg, false);
            break;
        case "comment-edit-open":
            if (arg) editCommentOpen(arg);
            break;
        case "comment-edit-save":
            if (arg) void saveCommentEdit(arg);
            break;
        case "comment-edit-cancel":
            state.commentEditing = null;
            state.commentEditDraft = "";
            render();
            break;
        case "setting":
            if (arg && arg in state.settings) {
                const key = arg as keyof typeof state.settings;
                state.settings[key] = !state.settings[key];
                render();
            }
            break;
        case "save-profile":
            if (state.user && state.bioDraft != null) {
                state.user.bio = state.bioDraft;
            }
            flash(el, "Saved");
            break;
        case "sync-github":
            syncFromGithub();
            break;
        case "add-email":
            void addEmail();
            break;
        case "verify-email":
            if (arg) void verifyEmail(arg);
            break;
        case "delete-email":
            if (arg) void deleteEmail(arg);
            break;
        default:
            break;
    }
}

// Brief inline confirmation on a button without a re-render.
function flash(el: HTMLElement, text: string): void {
    const orig = el.textContent;
    el.textContent = `✓ ${text}`;
    setTimeout(() => {
        el.textContent = orig;
    }, 1300);
}

function wireEvents(): void {
    const app = root();

    app.addEventListener("click", (e) => {
        const target = e.target as HTMLElement;
        const copyBtn = target.closest<HTMLElement>("[data-copy]");
        if (copyBtn) {
            void copyFrom(copyBtn);
            // Copying the install command counts as an install.
            if (copyBtn.getAttribute("data-act") === "copy-install" && state.pkg) {
                void api.recordInstall(state.pkg).then(() =>
                    api.loadInstalls(state.pkg!).then((r) => {
                        if (state.screen === "package") {
                            state.installSeries = r.series;
                            render();
                        }
                    }),
                );
            }
            return;
        }
        const actEl = target.closest<HTMLElement>("[data-act]");
        if (!actEl) {
            // Internal path links without a data-act (e.g. @mention links inside
            // rendered markdown) — route them in-app instead of a full reload.
            const a = target.closest<HTMLAnchorElement>("a[href^='/']");
            if (a && !a.hasAttribute("target") && !e.metaKey && !e.ctrlKey && e.button === 0) {
                e.preventDefault();
                const dest = a.getAttribute("href") || "/";
                history.pushState(null, "", dest);
                void route();
                scrollTop();
            }
            return;
        }
        const act = actEl.getAttribute("data-act")!;
        // Anchors/labels that we handle in JS shouldn't also navigate natively.
        if (actEl.tagName === "A" || actEl.tagName === "LABEL") e.preventDefault();
        dispatch(act, actEl.getAttribute("data-arg"), actEl);
    });

    // Text inputs update state silently so re-renders never steal focus.
    app.addEventListener("input", (e) => {
        const el = e.target as HTMLElement;
        const act = el.getAttribute("data-act");
        const value = (el as HTMLInputElement | HTMLTextAreaElement).value;
        if (act === "query") {
            state.query = value;
            onQueryInput(el as HTMLInputElement);
        } else if (act === "draft") state.draftText = value;
        else if (act === "bio") state.bioDraft = value;
        else if (act === "comment-draft") state.commentDraft = value;
        else if (act === "report-detail") state.reportDetail = value;
        else if (act === "publisher-draft") {
            state.publisherDraft = value;
            onPublisherInput();
        }
        else if (act === "deprecate-draft") state.deprecateDraft = value;
        else if (act === "transfer-draft") {
            state.transferDraft = value;
            onTransferInput();
        }
        else if (act === "comment-edit-draft") state.commentEditDraft = value;
        else if (act === "reply-draft") state.replyDraft = value;
        else if (act === "email-draft") state.emailDraft = value;
        else if (act === "email-code") {
            const addr = el.getAttribute("data-arg");
            if (addr) state.verifyDrafts[addr] = value;
        }
    });

    app.addEventListener("keydown", (e) => {
        const el = e.target as HTMLElement;
        if (e.key === "Enter" && el.getAttribute("data-enter") === "search") {
            e.preventDefault();
            navSearch();
        }
    });

    // Star-rating hover preview, painted in place.
    app.addEventListener("mouseover", (e) => {
        const star = (e.target as HTMLElement).closest<HTMLElement>(".pick-star");
        if (star) paintPicker(Number(star.getAttribute("data-n")));
    });
    app.addEventListener("mouseout", (e) => {
        const picker = (e.target as HTMLElement).closest("#star-picker");
        const to = e.relatedTarget as HTMLElement | null;
        if (picker && (!to || !to.closest || !to.closest("#star-picker"))) {
            paintPicker(state.draftRating);
        }
    });

    // Close the profile menu on an outside click.
    document.addEventListener("mousedown", (e) => {
        const t = e.target as HTMLElement;
        if (state.menuOpen && !t.closest?.("[data-profile-menu]")) {
            state.menuOpen = false;
            render();
        }
        // Close the publisher autocomplete on an outside click (a click inside —
        // the input or a result — keeps it so the pick registers).
        if (state.publisherResults.length && !t.closest?.("[data-pub-search]")) {
            void clearPublisherSearch();
        }
        if (state.transferResults.length && !t.closest?.("[data-transfer-search]")) {
            clearTransferSearch();
        }
    });

    window.addEventListener("popstate", () => void route());
}

// Translate legacy hash URLs (#/p/x, #/u/x, #/account, …) to the new path form,
// rewriting the address bar in place so old links keep working.
function migrateLegacyHash(): void {
    if (!location.hash.startsWith("#/")) return;
    const [h, q] = location.hash.slice(1).split("?");
    const parts = h.split("/").filter(Boolean);
    let path = "/";
    if (parts[0] === "p" && parts[1]) path = `/packages/${parts[1]}`;
    else if (parts[0] === "u" && parts[1]) path = `/${parts[1]}`;
    else if (parts[0] === "search") path = "/search";
    else if (parts[0] === "auth") path = "/auth";
    else if (parts[0] === "account") path = "/account";
    history.replaceState(null, "", path + (q ? `?${q}` : ""));
}

// ── boot ─────────────────────────────────────────────────────────────────────

export async function init(): Promise<void> {
    migrateLegacyHash();
    render(); // "Loading registry…"
    wireEvents();
    // Load the markdown renderer up front; re-render once ready so any already
    // painted bodies switch from the escaped-text fallback to rendered HTML.
    void initMarkdown().then(() => render());
    try {
        state.registry = await api.loadRegistry();
    } catch (err) {
        root().innerHTML = `<div style="padding:120px 0;text-align:center;color:#ff9ec4;font-size:15px">Failed to load the registry index.<br><span style="color:#7d72b0;font-size:13px">${String(err)}</span></div>`;
        return;
    }
    state.user = await api.getMe();
    enrichCatalogStars(); // real GitHub stargazers for the home/search cards
    await completePendingStar();
    if (state.user) void loadNotifications(); // populate the inbox badge
    await route();
}
