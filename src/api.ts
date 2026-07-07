// The data layer. Two modes, chosen once at boot by probing the backend:
//
//   • remote  — the Go backend at API_BASE is reachable. Packages, versions,
//               stars, reviews, votes, comments and install history are all
//               real and shared; sign-in is GitHub OAuth.
//   • local   — no backend (e.g. GitHub Pages only). Packages come from the
//               static index; sign-in and social actions are faked in
//               localStorage, seeded from the file so the UI stays alive.
//
// Screens never branch on the mode — they call these methods.

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
import {
    avatarBg,
    compactNum,
    initialOf,
    normalizeUser,
    relativeDate,
} from "./util.js";

let mode: "remote" | "local" = "local";
export function backendMode(): "remote" | "local" {
    return mode;
}

// Raw seed kept around for local mode (reviews/comments/users the backend would
// otherwise own).
interface RawSeedUser {
    login: string;
    name: string;
    bg?: string;
    avatarUrl?: string;
}
interface RawPackage extends Partial<Package> {
    seedReviews?: {
        login: string;
        rating: number;
        createdAt: string;
        body: string;
        score?: number;
    }[];
    seedComments?: {
        login: string;
        createdAt: string;
        body: string;
        parentIndex?: number;
    }[];
}
let rawByShort: Record<string, RawPackage> = {};
let seedUsers: Record<string, RawSeedUser> = {};

// ── boot ────────────────────────────────────────────────────────────────────

export async function probeBackend(): Promise<void> {
    try {
        const res = await fetch(`${API_BASE}/api/health`, {
            method: "GET",
            credentials: "include",
        });
        mode = res.ok ? "remote" : "local";
    } catch {
        mode = "local";
    }
}

// Load the browsable registry: stats + categories always come from the static
// index; the package list comes from the backend when it's live (for real
// stars/installs), else from the static file.
export async function loadRegistry(): Promise<Registry> {
    const res = await fetch(PACKAGES_URL, { cache: "no-cache" });
    if (!res.ok) throw new Error(`failed to load ${PACKAGES_URL}: ${res.status}`);
    const file = (await res.json()) as {
        packages: RawPackage[];
        stats: Registry["stats"];
        categories: Registry["categories"];
        seedUsers?: RawSeedUser[];
    };

    rawByShort = {};
    for (const p of file.packages) rawByShort[p.short!] = p;
    seedUsers = {};
    for (const u of file.seedUsers || []) seedUsers[u.login] = u;

    let packages: Package[];
    if (mode === "remote") {
        try {
            const r = await apiGet<{ packages: Package[] }>("/api/packages");
            packages = r.packages.map(normalizePackage);
        } catch {
            packages = file.packages.map(normalizePackage);
        }
    } else {
        packages = file.packages.map(normalizePackage);
    }
    return { packages, stats: file.stats, categories: file.categories };
}

// Fill in derived fields so a raw seed package and a backend package render the
// same. Idempotent — backend packages already carry these.
export function normalizePackage(raw: RawPackage): Package {
    const versions = raw.versions || [];
    const latest = versions.find((v) => v.latest) || versions[0];
    const installsWeek =
        raw.installsWeek ?? (raw as { installBaseWeek?: number }).installBaseWeek ?? 0;
    return {
        name: raw.name || "",
        short: raw.short || "",
        description: raw.description || "",
        category: raw.category || "",
        tags: raw.tags || [],
        keywords: raw.keywords || raw.tags || [],
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
        installsTotal: raw.installsTotal ?? Math.round(installsWeek * 13),
        issues: raw.issues || [],
    };
}

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

// ── local fallback store (localStorage) ──────────────────────────────────────

const LS = {
    user: "wago.user",
    stars: "wago.stars",
    reviews: "wago.reviews",
    votes: "wago.votes",
    comments: "wago.comments",
    installs: "wago.installs",
    bookmarks: "wago.bookmarks",
};

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

function seedUser(login: string): { name: string; bg: string; avatarUrl?: string } {
    const u = seedUsers[login];
    return {
        name: u?.name || login,
        bg: u?.bg || avatarBg(login),
        avatarUrl: u?.avatarUrl,
    };
}

function demoUser(): User {
    const u = seedUsers["jreyes"];
    return normalizeUser({
        id: "seed:jreyes",
        login: "jreyes",
        name: u?.name || "Jordan Reyes",
        email: "jordan@users.noreply.github.com",
        bio: "Systems engineer working on WASM tooling. Maintainer of a handful of wago subpackages.",
        createdAt: "2024-02-11T00:00:00Z",
    });
}

// ── auth ─────────────────────────────────────────────────────────────────────

export async function getMe(): Promise<User | null> {
    if (mode === "remote") {
        try {
            const raw = await apiGet<{ login: string } & Partial<User>>("/api/me");
            return normalizeUser(raw);
        } catch {
            return null;
        }
    }
    const stored = lsGet<User | null>(LS.user, null);
    return stored ? normalizeUser(stored) : null;
}

// Build the GitHub sign-in URL. When star is true, the backend additionally
// requests the public_repo scope so it can star repos on the user's behalf.
// Fetch a registered wago user's public profile by login. Returns null when
// there's no such member (or no backend), so the caller can generate a profile.
export async function getPublicUser(login: string): Promise<ViewUser | null> {
    if (mode !== "remote") return null;
    try {
        const raw = await apiGet<ViewUser>(`/api/users/${encodeURIComponent(login)}`);
        return { ...raw, claimed: true };
    } catch {
        return null; // 404 (not a member) or unreachable
    }
}

export function signInUrl(returnTo: string, star = false): string {
    const base = `${API_BASE}/auth/github/login?redirect=${encodeURIComponent(returnTo)}`;
    return star ? `${base}&star=1` : base;
}

export async function localSignIn(): Promise<User> {
    const u = demoUser();
    lsSet(LS.user, u);
    return u;
}

export async function signOut(): Promise<void> {
    if (mode === "remote") {
        try {
            await apiSend("/api/logout", "POST");
        } catch {
            /* ignore */
        }
        return;
    }
    localStorage.removeItem(LS.user);
}

// Wipe every piece of per-browser state we own (session/demo user, stars, votes,
// comments, install counters, bookmarks, GitHub cache, star prefs). Called on
// sign-out so nothing carries over between accounts.
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
// Remote: real endpoints (adds email → mailed 6-digit code → verify). Local:
// faked against the stored demo user so the settings UI stays explorable.

function localEmails(): UserEmail[] {
    const u = lsGet<User | null>(LS.user, null);
    if (!u) return [];
    if (!u.emails && u.email) {
        u.emails = [{ address: u.email, verified: true, source: "github" }];
        lsSet(LS.user, u);
    }
    return u.emails || [];
}

function setLocalEmails(emails: UserEmail[]): UserEmail[] {
    const u = lsGet<User | null>(LS.user, null);
    if (u) {
        u.emails = emails;
        lsSet(LS.user, u);
    }
    return emails;
}

export async function listEmails(): Promise<UserEmail[]> {
    if (mode === "remote") {
        const r = await apiGet<{ emails: UserEmail[] }>("/api/me/emails");
        return r.emails || [];
    }
    return localEmails();
}

export async function addEmail(email: string): Promise<{ ok: boolean; sent: boolean }> {
    if (mode === "remote") {
        return apiSend<{ ok: boolean; sent: boolean }>("/api/me/emails", "POST", { email });
    }
    const emails = localEmails();
    if (emails.some((e) => e.address.toLowerCase() === email.toLowerCase())) {
        throw new Error("email already on your account");
    }
    setLocalEmails([...emails, { address: email, verified: false, source: "added" }]);
    return { ok: true, sent: false };
}

export async function verifyEmail(email: string, code: string): Promise<UserEmail[]> {
    if (mode === "remote") {
        const r = await apiSend<{ emails: UserEmail[] }>("/api/me/emails/verify", "POST", {
            email,
            code,
        });
        return r.emails || [];
    }
    // Local demo: any code verifies.
    return setLocalEmails(
        localEmails().map((e) =>
            e.address.toLowerCase() === email.toLowerCase() ? { ...e, verified: true } : e,
        ),
    );
}

export async function deleteEmail(email: string): Promise<UserEmail[]> {
    if (mode === "remote") {
        const r = await apiSend<{ emails: UserEmail[] }>(
            `/api/me/emails/${encodeURIComponent(email)}`,
            "DELETE",
        );
        return r.emails || [];
    }
    return setLocalEmails(
        localEmails().filter(
            (e) => !(e.source === "added" && e.address.toLowerCase() === email.toLowerCase()),
        ),
    );
}

// ── package detail ───────────────────────────────────────────────────────────

export async function loadPackage(short: string, fallback: Package): Promise<Package> {
    if (mode === "remote") {
        try {
            return normalizePackage(await apiGet<RawPackage>(`/api/packages/${short}`));
        } catch {
            return fallback;
        }
    }
    const s = localStarState(fallback);
    return { ...fallback, stars: s.stars, starred: s.starred };
}

// ── stars ────────────────────────────────────────────────────────────────────

export async function setStar(
    pkg: Package,
    on: boolean,
): Promise<{ stars: number; starred: boolean }> {
    if (mode === "remote") {
        return apiSend(`/api/packages/${pkg.short}/star`, on ? "POST" : "DELETE");
    }
    const stars = lsGet<Record<string, boolean>>(LS.stars, {});
    const was = !!stars[pkg.short];
    stars[pkg.short] = on;
    lsSet(LS.stars, stars);
    return { stars: pkg.stars + ((on ? 1 : 0) - (was ? 1 : 0)), starred: on };
}

// Star (on) or unstar the package's repo on GitHub via the backend, using the
// user's stored OAuth token. Returns "ok", "need_permission" (missing/expired
// public_repo scope — the caller should offer to re-authorize), or "error".
// Only meaningful in remote mode; local mode has no backend to hold the token.
export async function githubStar(
    pkg: Package,
    on: boolean,
): Promise<"ok" | "need_permission" | "error"> {
    if (mode !== "remote") return "error";
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

// The package shorts the current user has starred. Remote: a backend join;
// local: the per-browser localStorage star set.
export async function myStars(): Promise<string[]> {
    if (mode === "remote") {
        try {
            const r = await apiGet<{ stars: string[] }>("/api/me/stars");
            return r.stars || [];
        } catch {
            return [];
        }
    }
    const stars = lsGet<Record<string, boolean>>(LS.stars, {});
    return Object.keys(stars).filter((k) => stars[k]);
}

export function localStarState(pkg: Package): { stars: number; starred: boolean } {
    if (mode === "remote") return { stars: pkg.stars, starred: !!pkg.starred };
    const stars = lsGet<Record<string, boolean>>(LS.stars, {});
    const starred = !!stars[pkg.short];
    return { stars: pkg.stars + (starred ? 1 : 0), starred };
}

// ── bookmarks (save-for-later) ───────────────────────────────────────────────
// A personal, per-browser list. There's no backend endpoint for this yet, so it
// lives in localStorage in both modes.

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
    if (mode === "remote") {
        const r = await apiGet<ReviewsResult>(`/api/packages/${pkg.short}/reviews`);
        r.reviews = r.reviews.map((rv) => decorateReview(rv, user));
        return r;
    }
    const raw = rawByShort[pkg.short];
    const seeds = (raw?.seedReviews || []).map((s, i) =>
        decorateReview(
            {
                id: `seed-${pkg.short}-${i}`,
                userId: `seed:${s.login}`,
                author: s.login,
                login: s.login,
                rating: s.rating,
                body: s.body,
                createdAt: s.createdAt,
                score: s.score ?? 0,
                myVote: null,
            },
            user,
        ),
    );
    const posted = (lsGet<Record<string, Review[]>>(LS.reviews, {})[pkg.short] || []).map((rv) =>
        decorateReview(rv, user),
    );
    const votes = lsGet<Record<string, "up" | "down">>(LS.votes, {});
    const all = [...posted, ...seeds];
    for (const r of all) {
        const v = votes[r.id!] || null;
        r.myVote = v;
        r.score = (r.score || 0) + (v === "up" ? 1 : 0) + (v === "down" ? -1 : 0);
    }
    const count = pkg.ratingCount || all.length;
    return { reviews: all, summary: { average: pkg.rating, count } };
}

function decorateReview(r: Review, user: User | null): Review {
    const su = seedUser(r.author);
    return {
        ...r,
        login: r.login || r.author,
        initial: initialOf(su.name),
        bg: su.bg,
        avatarUrl: r.avatarUrl || su.avatarUrl,
        author: su.name,
        mine: r.mine ?? (!!user && String(r.userId) === String(user.id)),
    };
}

export async function postReview(
    pkg: Package,
    user: User,
    rating: number,
    body: string,
): Promise<void> {
    if (mode === "remote") {
        await apiSend(`/api/packages/${pkg.short}/reviews`, "POST", { rating, body });
        return;
    }
    const store = lsGet<Record<string, Review[]>>(LS.reviews, {});
    const list = (store[pkg.short] || []).filter((r) => String(r.userId) !== String(user.id));
    list.unshift({
        id: `local-${Date.now()}`,
        userId: user.id,
        author: user.login,
        rating,
        body,
        createdAt: new Date().toISOString(),
        score: 0,
        myVote: null,
        mine: true,
    });
    store[pkg.short] = list;
    lsSet(LS.reviews, store);
}

export async function deleteReview(pkg: Package, reviewId: string): Promise<void> {
    if (mode === "remote") {
        await apiSend(`/api/reviews/${reviewId}`, "DELETE");
        return;
    }
    const store = lsGet<Record<string, Review[]>>(LS.reviews, {});
    store[pkg.short] = (store[pkg.short] || []).filter((r) => r.id !== reviewId);
    lsSet(LS.reviews, store);
    const votes = lsGet<Record<string, "up" | "down">>(LS.votes, {});
    delete votes[reviewId];
    lsSet(LS.votes, votes);
}

export async function voteReview(reviewId: string, dir: "up" | "down" | null): Promise<void> {
    if (mode === "remote") {
        await apiSend(`/api/reviews/${reviewId}/vote`, "POST", { dir });
        return;
    }
    const votes = lsGet<Record<string, "up" | "down">>(LS.votes, {});
    if (dir === null) delete votes[reviewId];
    else votes[reviewId] = dir;
    lsSet(LS.votes, votes);
}

// ── comments ─────────────────────────────────────────────────────────────────

export async function loadComments(pkg: Package, user: User | null): Promise<Comment[]> {
    let list: Comment[];
    if (mode === "remote") {
        const r = await apiGet<{ comments: Comment[] }>(`/api/packages/${pkg.short}/comments`);
        list = r.comments;
    } else {
        const raw = rawByShort[pkg.short];
        const seeds: Comment[] = [];
        (raw?.seedComments || []).forEach((c, i) => {
            const id = `seedc-${pkg.short}-${i}`;
            const parentId =
                c.parentIndex != null ? `seedc-${pkg.short}-${c.parentIndex}` : undefined;
            seeds.push({
                id,
                userId: `seed:${c.login}`,
                author: c.login,
                login: c.login,
                body: c.body,
                createdAt: c.createdAt,
                parentId,
                score: 0,
                myVote: null,
            });
        });
        const posted = lsGet<Record<string, Comment[]>>(LS.comments, {})[pkg.short] || [];
        // Apply this browser's votes (same store the review votes use).
        const votes = lsGet<Record<string, "up" | "down">>(LS.votes, {});
        list = [...seeds, ...posted].map((c) => {
            const v = votes[c.id] || null;
            return {
                ...c,
                myVote: v,
                score: (c.score || 0) + (v === "up" ? 1 : 0) + (v === "down" ? -1 : 0),
            };
        });
    }
    return list.map((c) => {
        const su = seedUser(c.author);
        return {
            ...c,
            login: c.login || c.author,
            initial: initialOf(su.name),
            bg: su.bg,
            avatarUrl: c.avatarUrl || su.avatarUrl,
            author: su.name,
            mine: !!user && String(c.userId) === String(user.id),
        };
    });
}

export async function postComment(
    pkg: Package,
    user: User,
    body: string,
    parentId?: string,
): Promise<void> {
    if (mode === "remote") {
        await apiSend(`/api/packages/${pkg.short}/comments`, "POST", { body, parentId });
        return;
    }
    const store = lsGet<Record<string, Comment[]>>(LS.comments, {});
    const list = store[pkg.short] || [];
    list.push({
        id: `localc-${Date.now()}`,
        userId: user.id,
        author: user.login,
        body,
        createdAt: new Date().toISOString(),
        parentId,
    });
    store[pkg.short] = list;
    lsSet(LS.comments, store);
}

// Comment votes reuse the same opaque-id vote store as reviews.
export async function voteComment(commentId: string, dir: "up" | "down" | null): Promise<void> {
    if (mode === "remote") {
        await apiSend(`/api/comments/${commentId}/vote`, "POST", { dir });
        return;
    }
    const votes = lsGet<Record<string, "up" | "down">>(LS.votes, {});
    if (dir === null) delete votes[commentId];
    else votes[commentId] = dir;
    lsSet(LS.votes, votes);
}

export async function editComment(pkg: Package, id: string, body: string): Promise<void> {
    if (mode === "remote") {
        await apiSend(`/api/comments/${id}`, "PUT", { body });
        return;
    }
    const store = lsGet<Record<string, Comment[]>>(LS.comments, {});
    store[pkg.short] = (store[pkg.short] || []).map((c) => (c.id === id ? { ...c, body } : c));
    lsSet(LS.comments, store);
}

export async function deleteComment(pkg: Package, id: string): Promise<void> {
    if (mode === "remote") {
        await apiSend(`/api/comments/${id}`, "DELETE");
        return;
    }
    const store = lsGet<Record<string, Comment[]>>(LS.comments, {});
    store[pkg.short] = (store[pkg.short] || []).filter((c) => c.id !== id);
    lsSet(LS.comments, store);
}

// ── install history ──────────────────────────────────────────────────────────

export interface InstallsResult {
    series: InstallPoint[];
    total: number;
    week: number;
    weekLabel: string;
}

export async function loadInstalls(pkg: Package, days = 90): Promise<InstallsResult> {
    if (mode === "remote") {
        try {
            return await apiGet<InstallsResult>(`/api/packages/${pkg.short}/installs?days=${days}`);
        } catch {
            /* fall through to synth */
        }
    }
    return synthInstalls(pkg, days);
}

// Deterministic daily series from the weekly base so the sparkline has shape
// even with no backend. A mild sine ripple, plus this browser's own recorded
// installs on top of today.
function synthInstalls(pkg: Package, days: number): InstallsResult {
    const perDay = pkg.installsWeek / 7;
    const extra = lsGet<Record<string, number>>(LS.installs, {})[pkg.short] || 0;
    const series: InstallPoint[] = [];
    const now = new Date();
    let total = 0;
    let week = 0;
    for (let i = days - 1; i >= 0; i--) {
        const d = new Date(now);
        d.setDate(now.getDate() - i);
        const ripple = 1 + 0.25 * Math.sin(i / 4) + 0.1 * Math.cos(i / 11);
        let count = Math.max(0, Math.round(perDay * ripple));
        if (i === 0) count += extra;
        const date = d.toISOString().slice(0, 10);
        series.push({ date, count });
        total += count;
        if (i < 7) week += count;
    }
    return { series, total, week, weekLabel: compactNum(week) };
}

// Record an install (e.g. when the install command is copied).
export async function recordInstall(pkg: Package): Promise<void> {
    if (mode === "remote") {
        try {
            await apiSend(`/api/packages/${pkg.short}/installs`, "POST", {
                version: pkg.latestVersion,
            });
        } catch {
            /* best-effort */
        }
        return;
    }
    const store = lsGet<Record<string, number>>(LS.installs, {});
    store[pkg.short] = (store[pkg.short] || 0) + 1;
    lsSet(LS.installs, store);
}

// Human "last publish" from the package's latest version.
export function lastPublish(pkg: Package): string {
    return pkg.updatedAt ? relativeDate(pkg.updatedAt) : "—";
}
