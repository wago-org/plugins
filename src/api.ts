// The data layer. Every method talks to the Go backend at API_BASE — packages,
// versions, stars, reviews, votes, comments and install history are all real and
// shared, and sign-in is GitHub OAuth. Screens never call fetch directly; they
// go through these methods.
//
// The only per-browser state we keep is bookmarks (save-for-later), which has no
// backend endpoint yet, alongside the GitHub client-fetch cache in github.ts.

import { API_BASE, PACKAGES_URL } from "./config.js";
import type {
    Comment,
    InstallPoint,
    Package,
    Registry,
    Review,
    User,
    UserEmail,
    ViewUser,
} from "./types.js";
import { avatarBg, compactNum, initialOf, normalizeUser, relativeDate } from "./util.js";

// The static package index ships the catalog taxonomy (stats + categories) and a
// fallback package list; real per-package metrics come from the backend.
type RawPackage = Partial<Package>;

// ── remote helpers ──────────────────────────────────────────────────────────

async function apiGet<T>(path: string): Promise<T> {
    const res = await fetch(`${API_BASE}${path}`, { credentials: "include" });
    if (!res.ok) throw new Error(`${path} → ${res.status}`);
    return (await res.json()) as T;
}

async function apiSend<T>(path: string, method: string, body?: unknown): Promise<T> {
    const res = await fetch(`${API_BASE}${path}`, {
        method,
        credentials: "include",
        headers: body ? { "Content-Type": "application/json" } : undefined,
        body: body ? JSON.stringify(body) : undefined,
    });
    if (!res.ok) throw new Error(`${path} → ${res.status}`);
    return (await res.json()) as T;
}

// ── registry ────────────────────────────────────────────────────────────────

// Load the browsable registry: stats + categories come from the static index;
// the package list comes from the backend (real stars/installs), falling back to
// the static catalog only if the backend is momentarily unreachable.
export async function loadRegistry(): Promise<Registry> {
    const res = await fetch(PACKAGES_URL, { cache: "no-cache" });
    if (!res.ok) throw new Error(`failed to load ${PACKAGES_URL}: ${res.status}`);
    const file = (await res.json()) as {
        packages: RawPackage[];
        stats: Registry["stats"];
        categories: Registry["categories"];
    };
    let packages: Package[];
    try {
        const r = await apiGet<{ packages: Package[] }>("/api/packages");
        packages = r.packages.map(normalizePackage);
    } catch {
        packages = file.packages.map(normalizePackage);
    }
    return { packages, stats: file.stats, categories: file.categories };
}

// Fill in derived fields so a static-index package and a backend package render
// the same. Idempotent — backend packages already carry these.
export function normalizePackage(raw: RawPackage): Package {
    const versions = raw.versions || [];
    const latest = versions.find((v) => v.latest) || versions[0];
    const installsWeek =
        raw.installsWeek ?? (raw as { installBaseWeek?: number }).installBaseWeek ?? 0;
    const tags = raw.tags || [];
    const keywords = raw.keywords || raw.tags || [];
    return {
        name: raw.name || "",
        short: raw.short || "",
        description: raw.description || "",
        category: raw.category || "",
        tags,
        keywords,
        // Precomputed once so realtime search is a plain substring scan per keystroke.
        search: `${raw.short || ""} ${raw.name || ""} ${raw.description || ""} ${tags.join(" ")} ${keywords.join(" ")}`.toLowerCase(),
        license: raw.license || "",
        repository: raw.repository || "",
        homepage: raw.homepage,
        stability: raw.stability || "stable",
        verified: !!raw.verified,
        official: raw.official,
        ownerLogin: raw.ownerLogin,
        deprecatedMessage: raw.deprecatedMessage,
        compatibility: raw.compatibility || { engines: {}, platforms: [] },
        capabilities: raw.capabilities || [],
        authors: raw.authors || [],
        contributors: raw.contributors || [],
        subpackages: raw.subpackages || [],
        version: raw.version || latest?.version || "",
        latestVersion: raw.latestVersion || latest?.version || "",
        versions,
        updatedAt: raw.updatedAt || latest?.publishedAt || "",
        rating: raw.rating ?? 0,
        ratingCount: raw.ratingCount ?? 0,
        score: raw.score ?? 0,
        stars: raw.stars ?? 0,
        forks: raw.forks,
        unpackedKB: raw.unpackedKB ?? latest?.unpackedKB,
        starred: raw.starred,
        installsWeek,
        installsWeekLabel: raw.installsWeekLabel || compactNum(installsWeek),
        installsTotal: raw.installsTotal ?? 0,
        issues: raw.issues || [],
    };
}

// ── auth ──────────────────────────────────────────────────────────────────────

export async function getMe(): Promise<User | null> {
    try {
        const raw = await apiGet<{ login: string } & Partial<User>>("/api/me");
        return normalizeUser(raw);
    } catch {
        return null;
    }
}

// Fetch a registered wago user's public profile by login. Returns null when
// there's no such member, so the caller can generate a profile from public data.
export async function getPublicUser(login: string): Promise<ViewUser | null> {
    try {
        const raw = await apiGet<ViewUser>(`/api/users/${encodeURIComponent(login)}`);
        return { ...raw, claimed: true };
    } catch {
        return null; // 404 (not a member) or unreachable
    }
}

// Build the GitHub sign-in URL. When star is true, the backend additionally
// requests the public_repo scope so it can star repos on the user's behalf.
export function signInUrl(returnTo: string, star = false): string {
    const base = `${API_BASE}/auth/github/login?redirect=${encodeURIComponent(returnTo)}`;
    return star ? `${base}&star=1` : base;
}

export async function signOut(): Promise<void> {
    try {
        await apiSend("/api/logout", "POST");
    } catch {
        /* ignore — we clear local state regardless */
    }
}

// ── per-browser state (bookmarks + cache) ─────────────────────────────────────

const LS = { bookmarks: "wago.bookmarks" };

function lsGet<T>(key: string, fallback: T): T {
    try {
        const v = localStorage.getItem(key);
        return v ? (JSON.parse(v) as T) : fallback;
    } catch {
        return fallback;
    }
}
function lsSet(key: string, val: unknown): void {
    try {
        localStorage.setItem(key, JSON.stringify(val));
    } catch {
        /* storage disabled — stay ephemeral */
    }
}

// Wipe every piece of per-browser state we own (bookmarks, GitHub cache, star
// prefs). Called on sign-out so nothing carries over between accounts.
export function clearLocalState(): void {
    try {
        const keys: string[] = [];
        for (let i = 0; i < localStorage.length; i++) {
            const k = localStorage.key(i);
            if (k && k.startsWith("wago.")) keys.push(k);
        }
        for (const k of keys) localStorage.removeItem(k);
    } catch {
        /* storage disabled — nothing to clear */
    }
}

// ── secondary emails ─────────────────────────────────────────────────────────

export async function listEmails(): Promise<UserEmail[]> {
    const r = await apiGet<{ emails: UserEmail[] }>("/api/me/emails");
    return r.emails || [];
}

export async function addEmail(email: string): Promise<{ ok: boolean; sent: boolean }> {
    return apiSend<{ ok: boolean; sent: boolean }>("/api/me/emails", "POST", { email });
}

export async function verifyEmail(email: string, code: string): Promise<UserEmail[]> {
    const r = await apiSend<{ emails: UserEmail[] }>("/api/me/emails/verify", "POST", {
        email,
        code,
    });
    return r.emails || [];
}

export async function deleteEmail(email: string): Promise<UserEmail[]> {
    const r = await apiSend<{ emails: UserEmail[] }>(
        `/api/me/emails/${encodeURIComponent(email)}`,
        "DELETE",
    );
    return r.emails || [];
}

// ── package detail ───────────────────────────────────────────────────────────

export async function loadPackage(short: string, fallback: Package): Promise<Package> {
    try {
        return normalizePackage(await apiGet<RawPackage>(`/api/packages/${short}`));
    } catch {
        return fallback;
    }
}

// ── stars ────────────────────────────────────────────────────────────────────

export async function setStar(
    pkg: Package,
    on: boolean,
): Promise<{ stars: number; starred: boolean }> {
    return apiSend(`/api/packages/${pkg.short}/star`, on ? "POST" : "DELETE");
}

// Star (on) or unstar the package's repo on GitHub via the backend, using the
// user's stored OAuth token. Returns "ok", "need_permission" (missing/expired
// public_repo scope — the caller should offer to re-authorize), or "error".
export async function githubStar(
    pkg: Package,
    on: boolean,
): Promise<"ok" | "need_permission" | "error"> {
    try {
        const res = await fetch(`${API_BASE}/api/packages/${pkg.short}/gh-star`, {
            method: on ? "POST" : "DELETE",
            credentials: "include",
        });
        if (res.ok) return "ok";
        if (res.status === 403) return "need_permission";
        return "error";
    } catch {
        return "error";
    }
}

// The package shorts the current user has starred (a backend join).
export async function myStars(): Promise<string[]> {
    try {
        const r = await apiGet<{ stars: string[] }>("/api/me/stars");
        return r.stars || [];
    } catch {
        return [];
    }
}

// ── bookmarks (save-for-later) ───────────────────────────────────────────────
// A personal, per-browser list. There's no backend endpoint for this yet, so it
// lives in localStorage.

export function isBookmarked(short: string): boolean {
    return !!lsGet<Record<string, boolean>>(LS.bookmarks, {})[short];
}

export function setBookmark(short: string, on: boolean): void {
    const bm = lsGet<Record<string, boolean>>(LS.bookmarks, {});
    if (on) bm[short] = true;
    else delete bm[short];
    lsSet(LS.bookmarks, bm);
}

// ── reviews ──────────────────────────────────────────────────────────────────

export interface ReviewsResult {
    reviews: Review[];
    summary: { average: number; count: number };
}

export async function loadReviews(pkg: Package, user: User | null): Promise<ReviewsResult> {
    const r = await apiGet<ReviewsResult>(`/api/packages/${pkg.short}/reviews`);
    r.reviews = r.reviews.map((rv) => decorateReview(rv, user));
    return r;
}

// Fill in the render-only identity fields (initial/bg) from the author details
// the backend provides (name + login + avatar).
function decorateReview(r: Review, user: User | null): Review {
    const name = r.author || r.login || "?";
    const login = r.login || r.author || "";
    return {
        ...r,
        login,
        initial: initialOf(name),
        bg: avatarBg(login || name),
        avatarUrl: r.avatarUrl,
        author: name,
        mine: r.mine ?? (!!user && String(r.userId) === String(user.id)),
    };
}

export async function postReview(
    pkg: Package,
    _user: User,
    rating: number,
    body: string,
): Promise<void> {
    await apiSend(`/api/packages/${pkg.short}/reviews`, "POST", { rating, body });
}

export async function deleteReview(_pkg: Package, reviewId: string): Promise<void> {
    await apiSend(`/api/reviews/${reviewId}`, "DELETE");
}

export async function voteReview(reviewId: string, dir: "up" | "down" | null): Promise<void> {
    await apiSend(`/api/reviews/${reviewId}/vote`, "POST", { dir });
}

// ── comments ─────────────────────────────────────────────────────────────────

export async function loadComments(pkg: Package, user: User | null): Promise<Comment[]> {
    const r = await apiGet<{ comments: Comment[] }>(`/api/packages/${pkg.short}/comments`);
    return r.comments.map((c) => {
        const name = c.author || c.login || "?";
        const login = c.login || c.author || "";
        return {
            ...c,
            login,
            initial: initialOf(name),
            bg: avatarBg(login || name),
            avatarUrl: c.avatarUrl,
            author: name,
            mine: !!user && String(c.userId) === String(user.id),
        };
    });
}

export async function postComment(
    pkg: Package,
    _user: User,
    body: string,
    parentId?: string,
): Promise<void> {
    await apiSend(`/api/packages/${pkg.short}/comments`, "POST", { body, parentId });
}

// Comment votes reuse the same opaque-id vote store as reviews.
export async function voteComment(commentId: string, dir: "up" | "down" | null): Promise<void> {
    await apiSend(`/api/comments/${commentId}/vote`, "POST", { dir });
}

export async function editComment(_pkg: Package, id: string, body: string): Promise<void> {
    await apiSend(`/api/comments/${id}`, "PUT", { body });
}

export async function deleteComment(_pkg: Package, id: string): Promise<void> {
    await apiSend(`/api/comments/${id}`, "DELETE");
}

// archiveComment soft-hides (archived=true) or restores a comment. Server-side,
// the comment's author or a moderator (package/org owner, or site admin) may.
export async function archiveComment(_pkg: Package, id: string, archived: boolean): Promise<void> {
    await apiSend(`/api/comments/${id}/archive`, "POST", { archived });
}

// ── install history ──────────────────────────────────────────────────────────

export interface InstallsResult {
    series: InstallPoint[];
    total: number;
    week: number;
    weekLabel: string;
}

export async function loadInstalls(pkg: Package, days = 90): Promise<InstallsResult> {
    try {
        return await apiGet<InstallsResult>(`/api/packages/${pkg.short}/installs?days=${days}`);
    } catch {
        return { series: [], total: 0, week: 0, weekLabel: "0" };
    }
}

// Record an install (e.g. when the install command is copied).
export async function recordInstall(pkg: Package): Promise<void> {
    try {
        await apiSend(`/api/packages/${pkg.short}/installs`, "POST", {
            version: pkg.latestVersion,
        });
    } catch {
        /* best-effort */
    }
}

// Human "last publish" from the package's latest version.
export function lastPublish(pkg: Package): string {
    return pkg.updatedAt ? relativeDate(pkg.updatedAt) : "—";
}
