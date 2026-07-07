// A tiny GitHub REST client that runs in the browser. Unauthenticated calls to
// api.github.com are limited to 60/hr per IP, so every response is cached in
// localStorage (TTL ~15 min) and in-flight requests are deduped. Nothing here
// ever throws: on 403 rate-limit, network error, or a miss it returns null/[]
// and the caller falls back to its seed data / initial avatar.

import type { Issue } from "./types.js";

const TTL_MS = 15 * 60 * 1000;
const CACHE_PREFIX = "wago.gh.";
const API = "https://api.github.com";

interface CacheEntry<T> {
    data: T;
    at: number;
}

function cacheGet<T>(key: string): T | null {
    try {
        const raw = localStorage.getItem(CACHE_PREFIX + key);
        if (!raw) return null;
        const entry = JSON.parse(raw) as CacheEntry<T>;
        if (Date.now() - entry.at > TTL_MS) return null;
        return entry.data;
    } catch {
        return null;
    }
}

function cacheSet<T>(key: string, data: T): void {
    try {
        localStorage.setItem(CACHE_PREFIX + key, JSON.stringify({ data, at: Date.now() }));
    } catch {
        /* storage disabled — stay uncached */
    }
}

// Dedupe concurrent fetches for the same key within a page lifetime.
const inFlight = new Map<string, Promise<unknown>>();

async function ghFetch<T>(path: string): Promise<T | null> {
    try {
        const res = await fetch(`${API}${path}`, {
            headers: { Accept: "application/vnd.github+json" },
        });
        if (!res.ok) return null; // 403 rate-limit, 404, etc. — caller falls back.
        return (await res.json()) as T;
    } catch {
        return null;
    }
}

// --- Issues ---

// parseRepo pulls {owner, repo} out of a repository URL like
// https://github.com/wago-org/wasi(.git) or git@github.com:owner/repo.git.
export function parseRepo(repository: string): { owner: string; repo: string } | null {
    if (!repository) return null;
    const m = repository.match(/github\.com[/:]([^/]+)\/([^/#?]+?)(?:\.git)?\/?$/i);
    if (!m) return null;
    return { owner: m[1], repo: m[2] };
}

interface GhIssue {
    number: number;
    title: string;
    state: string;
    labels: { name: string }[];
    comments: number;
    user: { login: string } | null;
    created_at: string;
    pull_request?: unknown; // present on PRs — we filter those out
}

// fetchIssues returns issues mapped to the app's Issue shape, or null on any
// failure. Pull requests (which the issues endpoint also returns) are dropped.
export async function fetchIssues(owner: string, repo: string): Promise<Issue[] | null> {
    const key = `issues.${owner}/${repo}`;
    const cached = cacheGet<Issue[]>(key);
    if (cached) return cached;
    if (inFlight.has(key)) return inFlight.get(key) as Promise<Issue[] | null>;

    const p = (async (): Promise<Issue[] | null> => {
        const raw = await ghFetch<GhIssue[]>(
            `/repos/${owner}/${repo}/issues?state=all&per_page=30`,
        );
        if (!raw) return null;
        const issues: Issue[] = raw
            .filter((it) => !it.pull_request)
            .map((it) => ({
                num: it.number,
                title: it.title,
                state: it.state === "closed" ? "closed" : "open",
                labels: (it.labels || []).map((l) => l.name),
                comments: it.comments,
                age: relAge(it.created_at),
                author: it.user?.login || "unknown",
            }));
        cacheSet(key, issues);
        return issues;
    })();
    inFlight.set(key, p);
    try {
        return await p;
    } finally {
        inFlight.delete(key);
    }
}

// --- Users (profile pictures) ---

export interface GhUser {
    login: string;
    name: string;
    avatarUrl: string;
    bio: string;
}

interface GhUserRaw {
    login: string;
    name: string | null;
    avatar_url: string;
    bio: string | null;
}

// avatarFor returns a cached avatar_url for a login synchronously if we have
// one, else null (caller shows the initial avatar). It does not fetch.
export function avatarFor(login: string): string | null {
    const u = cacheGet<GhUser>(`user.${login.toLowerCase()}`);
    return u?.avatarUrl || null;
}

// fetchUser loads and caches a user's public profile. Returns null on failure.
export async function fetchUser(login: string): Promise<GhUser | null> {
    const key = `user.${login.toLowerCase()}`;
    const cached = cacheGet<GhUser>(key);
    if (cached) return cached;
    if (inFlight.has(key)) return inFlight.get(key) as Promise<GhUser | null>;

    const p = (async (): Promise<GhUser | null> => {
        const raw = await ghFetch<GhUserRaw>(`/users/${login}`);
        if (!raw) return null;
        const user: GhUser = {
            login: raw.login,
            name: raw.name || raw.login,
            avatarUrl: raw.avatar_url,
            bio: raw.bio || "",
        };
        cacheSet(key, user);
        return user;
    })();
    inFlight.set(key, p);
    try {
        return await p;
    } finally {
        inFlight.delete(key);
    }
}

// ensureAvatars fetches any logins we don't yet have cached (deduped), and calls
// onLoaded once at least one new avatar arrived so the caller can re-render.
export function ensureAvatars(logins: string[], onLoaded: () => void): void {
    const missing = Array.from(new Set(logins.filter((l) => l && !avatarFor(l))));
    if (!missing.length) return;
    let any = false;
    let pending = missing.length;
    for (const login of missing) {
        void fetchUser(login).then((u) => {
            if (u) any = true;
            pending--;
            if (pending === 0 && any) onLoaded();
        });
    }
}

// relAge renders an ISO timestamp as a short "3 days ago"-style label, matching
// the seed issue `age` strings.
function relAge(iso: string): string {
    const then = Date.parse(iso);
    if (Number.isNaN(then)) return iso;
    const secs = Math.max(1, Math.floor((Date.now() - then) / 1000));
    const units: [number, string][] = [
        [31536000, "year"],
        [2592000, "month"],
        [604800, "week"],
        [86400, "day"],
        [3600, "hour"],
        [60, "minute"],
    ];
    for (const [size, name] of units) {
        const n = Math.floor(secs / size);
        if (n >= 1) return `${n} ${name}${n === 1 ? "" : "s"} ago`;
    }
    return "just now";
}
