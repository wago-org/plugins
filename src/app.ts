// App orchestrator: owns the render loop, the hash router (via history
// pushState / popstate), and a single delegated event layer. Screens are pure;
// everything that mutates state and asks for a re-render lives here.

import * as api from "./api.js";
import { backendMode } from "./api.js";
import { copyFrom } from "./copy.js";
import {
    accountScreen,
    authScreen,
    footer,
    homeScreen,
    nav,
    packageScreen,
    searchScreen,
} from "./screens.js";
import { findPackage, state } from "./state.js";
import type { AcctTab, PkgTab, Sort } from "./state.js";

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
        default:
            return homeScreen(state);
    }
}

function render(): void {
    root().innerHTML = nav(state) + screenBody() + footer(state);
}

// ── router ───────────────────────────────────────────────────────────────────

function pushUrl(hash: string): void {
    if (location.hash !== hash) history.pushState(null, "", hash || "#/");
}

// Rebuild screen-level state from the current URL hash (used on load and on
// browser back/forward). Ephemeral filters/tabs intentionally reset here.
async function route(): Promise<void> {
    const raw = location.hash.replace(/^#/, "") || "/";
    const [path, queryStr] = raw.split("?");
    const params = new URLSearchParams(queryStr || "");
    const parts = path.split("/").filter(Boolean); // e.g. ["p","wasi-fs"]

    if (parts[0] === "p" && parts[1]) {
        await openPackage(decodeURIComponent(parts[1]), false);
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
    if (parts[0] === "account") {
        state.screen = state.user ? "account" : "auth";
        render();
        return;
    }
    state.screen = "home";
    render();
}

// ── navigation helpers ───────────────────────────────────────────────────────

function navHome(): void {
    state.screen = "home";
    state.menuOpen = false;
    pushUrl("#/");
    render();
    scrollTop();
}

function navAuth(): void {
    state.screen = "auth";
    state.authError = null;
    pushUrl("#/auth");
    render();
    scrollTop();
}

function navSearch(): void {
    state.screen = "search";
    const q = state.query.trim();
    pushUrl(q ? `#/search?q=${encodeURIComponent(q)}` : "#/search");
    render();
    scrollTop();
}

function navAccount(tab: AcctTab): void {
    if (!state.user) {
        navAuth();
        return;
    }
    state.screen = "account";
    state.acctTab = tab;
    state.menuOpen = false;
    pushUrl("#/account");
    render();
    scrollTop();
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
    state.composerOpen = false;
    state.draftRating = 0;
    state.hoverRating = 0;
    state.draftText = "";
    state.issueFilter = "open";
    state.reviews = [];
    state.reviewsSummary = { average: pkg.rating, count: pkg.ratingCount };
    state.reviewsLoading = false;
    state.comments = [];
    state.commentsLoading = false;
    state.commentDraft = "";
    state.replyTo = null;
    state.replyDraft = "";
    state.installSeries = [];
    // seed the star widget synchronously so the header renders immediately…
    state.starred = !!pkg.starred;
    state.starCount = pkg.stars;
    state.bookmarked = api.isBookmarked(pkg.short);
    if (push) pushUrl(`#/p/${encodeURIComponent(short)}`);
    render();
    scrollTop();
    // …then enrich from the backend (or local store) when available.
    const detail = await api.loadPackage(short, pkg);
    if (state.pkg !== pkg) return;
    state.pkg = detail;
    state.starred = !!detail.starred;
    state.starCount = detail.stars;
    render();
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
    }
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

async function doSignIn(): Promise<void> {
    if (backendMode() === "remote") {
        window.location.href = api.signInUrl(location.href);
        return;
    }
    state.user = await api.localSignIn();
    navAccount("profile");
}

async function doSignOut(): Promise<void> {
    await api.signOut();
    state.user = null;
    state.menuOpen = false;
    navHome();
}

async function toggleStar(): Promise<void> {
    if (!state.user) {
        navAuth();
        return;
    }
    if (!state.pkg) return;
    const on = !state.starred;
    // optimistic
    state.starred = on;
    state.starCount += on ? 1 : -1;
    render();
    try {
        const res = await api.setStar(state.pkg, on);
        state.starred = res.starred;
        state.starCount = res.stars;
    } catch {
        // revert on failure
        state.starred = !on;
        state.starCount += on ? -1 : 1;
    }
    render();
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
    await refreshComments();
}

async function submitReply(parentId: string): Promise<void> {
    if (!state.user || !state.pkg) return;
    const text = state.replyDraft.trim();
    if (!text) return;
    await api.postComment(state.pkg, state.user, text, parentId);
    state.replyTo = null;
    state.replyDraft = "";
    await refreshComments();
}

async function removeComment(id: string): Promise<void> {
    if (!state.pkg) return;
    await api.deleteComment(state.pkg, id);
    await refreshComments();
}

function setPkgTab(tab: PkgTab): void {
    state.pkgTab = tab;
    render();
    if (tab === "reviews" && state.reviews.length === 0 && !state.reviewsLoading) {
        void refreshReviews();
    }
    if (tab === "comments" && state.comments.length === 0 && !state.commentsLoading) {
        void refreshComments();
    }
}

// ── event delegation ─────────────────────────────────────────────────────────

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
            render();
            break;
        case "open":
            if (arg) void openPackage(arg);
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
        case "star":
            void toggleStar();
            break;
        case "bookmark":
            if (state.pkg) {
                state.bookmarked = !state.bookmarked;
                api.setBookmark(state.pkg.short, state.bookmarked);
                render();
            }
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
            render();
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
            render();
            break;
        case "reply-submit":
            if (arg) void submitReply(arg);
            break;
        case "comment-delete":
            if (arg) void removeComment(arg);
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
        if (!actEl) return;
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
        if (act === "query") state.query = value;
        else if (act === "draft") state.draftText = value;
        else if (act === "bio") state.bioDraft = value;
        else if (act === "comment-draft") state.commentDraft = value;
        else if (act === "reply-draft") state.replyDraft = value;
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
        if (!state.menuOpen) return;
        const inMenu = (e.target as HTMLElement).closest?.("[data-profile-menu]");
        if (!inMenu) {
            state.menuOpen = false;
            render();
        }
    });

    window.addEventListener("popstate", () => void route());
}

// ── boot ─────────────────────────────────────────────────────────────────────

export async function init(): Promise<void> {
    render(); // "Loading registry…"
    wireEvents();
    await api.probeBackend();
    try {
        state.registry = await api.loadRegistry();
    } catch (err) {
        root().innerHTML = `<div style="padding:120px 0;text-align:center;color:#ff9ec4;font-size:15px">Failed to load the registry index.<br><span style="color:#7d72b0;font-size:13px">${String(err)}</span></div>`;
        return;
    }
    state.user = await api.getMe();
    await route();
}
