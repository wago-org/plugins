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
// decodeBase64 decodes a base64 string to UTF-8 text (GitHub returns file
// contents base64-encoded).
function decodeBase64(b64: string): string {
    const bin = atob(b64);
    const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0));
    return new TextDecoder("utf-8").decode(bytes);
}

interface GhReadmeRaw {
    content?: string;
    encoding?: string;
}

// fetchReadme returns the repo's README as markdown (decoded from the GitHub
// API's base64), cached. When ref is given (the release commit), it fetches that
// exact commit — a permalink, so the page shows the README as of the published
// version. Null when there's no README, or on a fetch failure (rate-limit /
// offline / private repo the browser can't reach).
export async function fetchReadme(owner: string, repo: string, ref?: string): Promise<string | null> {
    const key = `readme.${owner}/${repo}@${ref || "HEAD"}`.toLowerCase();
    const cached = cacheGet<string>(key);
    if (cached != null) return cached;
    if (inFlight.has(key)) return inFlight.get(key) as Promise<string | null>;
    const p = (async (): Promise<string | null> => {
        const base = `/repos/${owner}/${repo}/readme`;
        let raw = await ghFetch<GhReadmeRaw>(ref ? `${base}?ref=${encodeURIComponent(ref)}` : base);
        // A pinned commit can be missing on GitHub (unpushed / rewritten history);
        // fall back to the default branch so the current README still shows.
        if ((!raw || !raw.content) && ref) raw = await ghFetch<GhReadmeRaw>(base);
        if (!raw || !raw.content) return null;
        const text = decodeBase64(raw.content.replace(/\s/g, ""));
        cacheSet(key, text);
        return text;
    })();
    inFlight.set(key, p);
    try {
        return await p;
    } finally {
        inFlight.delete(key);
    }
}

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

// --- Repositories (real star / fork counts) ---

export interface GhRepo {
    stars: number;
    forks: number;
    htmlUrl: string;
}

interface GhRepoRaw {
    stargazers_count: number;
    forks_count: number;
    html_url: string;
}

// starsFor returns the cached GitHub stargazer count for a repo, or null if we
// haven't fetched it yet. Synchronous cache read — does not fetch.
export function starsFor(owner: string, repo: string): number | null {
    const r = cacheGet<GhRepo>(`repo.${owner}/${repo}`.toLowerCase());
    return r ? r.stars : null;
}

// fetchRepo loads and caches a repository's public counts. Returns null on any
// failure (rate-limit / 404 / offline) so callers keep their seed numbers.
export async function fetchRepo(owner: string, repo: string): Promise<GhRepo | null> {
    const key = `repo.${owner}/${repo}`.toLowerCase();
    const cached = cacheGet<GhRepo>(key);
    if (cached) return cached;
    if (inFlight.has(key)) return inFlight.get(key) as Promise<GhRepo | null>;

    const p = (async (): Promise<GhRepo | null> => {
        const raw = await ghFetch<GhRepoRaw>(`/repos/${owner}/${repo}`);
        if (!raw) return null;
        const out: GhRepo = {
            stars: raw.stargazers_count,
            forks: raw.forks_count,
            htmlUrl: raw.html_url,
        };
        cacheSet(key, out);
        return out;
    })();
    inFlight.set(key, p);
    try {
        return await p;
    } finally {
        inFlight.delete(key);
    }
}

// searchUsers returns GitHub accounts (users + orgs) matching a query, for the
// publisher autocomplete. Cached + deduped; empty for very short queries or on
// failure (rate-limit / offline). Unauthenticated search is ~10 req/min, so
// callers should debounce.
export async function searchUsers(query: string): Promise<GhAccountRef[]> {
    const q = query.trim();
    if (q.length < 2) return [];
    const key = `usersearch.${q}`.toLowerCase();
    const cached = cacheGet<GhAccountRef[]>(key);
    if (cached) return cached;
    if (inFlight.has(key)) return (await inFlight.get(key)) as GhAccountRef[];
    const p = (async (): Promise<GhAccountRef[]> => {
        const raw = await ghFetch<{ items?: { login: string; avatar_url: string }[] }>(
            `/search/users?q=${encodeURIComponent(q)}&per_page=7`,
        );
        const out = (raw?.items || []).map((u) => ({ login: u.login, avatarUrl: u.avatar_url }));
        cacheSet(key, out);
        return out;
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
    isOrg: boolean;
}

interface GhUserRaw {
    login: string;
    name: string | null;
    avatar_url: string;
    bio: string | null;
    type?: string; // "User" | "Organization"
}

export interface GhAccountRef {
    login: string;
    avatarUrl: string;
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
            isOrg: raw.type === "Organization",
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

// --- Organizations ---

interface GhOrgRef {
    login: string;
    avatar_url: string;
}

// fetchOrgs returns a user's public organization memberships (login + avatar),
// cached. Empty on any failure.
export async function fetchOrgs(login: string): Promise<GhAccountRef[]> {
    const key = `orgs.${login.toLowerCase()}`;
    const cached = cacheGet<GhAccountRef[]>(key);
    if (cached) return cached;
    if (inFlight.has(key)) return (await inFlight.get(key)) as GhAccountRef[];
    const p = (async (): Promise<GhAccountRef[]> => {
        const raw = await ghFetch<GhOrgRef[]>(`/users/${login}/orgs`);
        const orgs = (raw || []).map((o) => ({ login: o.login, avatarUrl: o.avatar_url }));
        cacheSet(key, orgs);
        return orgs;
    })();
    inFlight.set(key, p);
    try {
        return await p;
    } finally {
        inFlight.delete(key);
    }
}

// fetchOrgMembers returns an org's public members (login + avatar), cached.
export async function fetchOrgMembers(org: string): Promise<GhAccountRef[]> {
    const key = `members.${org.toLowerCase()}`;
    const cached = cacheGet<GhAccountRef[]>(key);
    if (cached) return cached;
    if (inFlight.has(key)) return (await inFlight.get(key)) as GhAccountRef[];
    const p = (async (): Promise<GhAccountRef[]> => {
        const raw = await ghFetch<GhOrgRef[]>(`/orgs/${org}/public_members?per_page=30`);
        const members = (raw || []).map((m) => ({ login: m.login, avatarUrl: m.avatar_url }));
        cacheSet(key, members);
        return members;
    })();
    inFlight.set(key, p);
    try {
        return await p;
    } finally {
        inFlight.delete(key);
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
