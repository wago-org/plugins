// Small pure helpers shared across screens. Kept dependency-free so the render
// layer stays a straight function of state.

import type { User } from "./types.js";

// A 5-slot star string, filled + hollow, matching the design.
export function starStr(r: number): string {
    const n = Math.round(r);
    return "★".repeat(n) + "☆".repeat(5 - n);
}

// Popularity tier colour, keyed off a package's composite score.
export function tier(score: number): string {
    if (score >= 92) return "#74e0ad"; // top-rated
    if (score >= 85) return "#c3a8ff"; // popular
    return "#8d7fc7"; // rising
}

// Escape untrusted text before it goes into innerHTML. Every value that
// originates from the backend (names, review bodies, bios) passes through this.
export function esc(s: unknown): string {
    return String(s ?? "")
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#39;");
}

// Attribute-safe escaping (same rules; named separately for intent).
export const escAttr = esc;

// Deterministic avatar tint from a string, so a signed-in user or review author
// gets a stable colour without the backend having to pick one.
const AVATAR_BG = ["#c3a8ff", "#74e0ad", "#ff9ec4", "#8d7fc7", "#7bd0ff"];
export function avatarBg(seed: string): string {
    let h = 0;
    for (let i = 0; i < seed.length; i++) h = (h * 31 + seed.charCodeAt(i)) | 0;
    return AVATAR_BG[Math.abs(h) % AVATAR_BG.length];
}

export function initialOf(name: string): string {
    const c = (name || "?").trim()[0] || "?";
    return c.toUpperCase();
}

// Normalize a raw backend user (or a locally-faked one) into the shape screens
// expect, deriving avatar colour + initial when absent.
export function normalizeUser(raw: Partial<User> & { login: string }): User {
    const name = raw.name || raw.login;
    return {
        id: raw.id ?? raw.login,
        login: raw.login,
        name,
        avatarUrl: raw.avatarUrl,
        email: raw.email,
        bio: raw.bio,
        company: raw.company,
        location: raw.location,
        blog: raw.blog,
        twitterUsername: raw.twitterUsername,
        htmlUrl: raw.htmlUrl,
        githubCreatedAt: raw.githubCreatedAt,
        followers: raw.followers,
        following: raw.following,
        publicRepos: raw.publicRepos,
        hireable: raw.hireable,
        createdAt: raw.createdAt,
        emails: raw.emails,
        canStar: raw.canStar,
        initial: initialOf(name),
        bg: raw.bg || avatarBg(raw.login),
    };
}

// Compact install/count label: 4_200_000 → "4.2M", 48_200 → "48.2k".
export function compactNum(n: number): string {
    if (n >= 1e6) return `${trim(n / 1e6)}M`;
    if (n >= 1e3) return `${trim(n / 1e3)}k`;
    return String(Math.round(n));
}
function trim(x: number): string {
    return x.toFixed(1).replace(/\.0$/, "");
}

// Short git hash for display (first 7 chars).
export function shortHash(commit: string): string {
    return (commit || "").slice(0, 7);
}

// Turn an RFC3339 timestamp into a human "3 days ago" string.
export function relativeDate(iso: string): string {
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

// Full date label from an ISO timestamp, e.g. "July 7, 2026". Formatted in UTC
// so a join date renders as the same calendar day for every viewer. Empty on
// bad input.
export function fullDate(iso: string): string {
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return "";
    return d.toLocaleDateString("en-US", {
        month: "long",
        day: "numeric",
        year: "numeric",
        timeZone: "UTC",
    });
}

// Human "how long they've been a member" from an ISO join date, e.g.
// "3 years", "5 months", "12 days", "today". Returns "" for a bad/empty date.
export function memberFor(iso: string): string {
    const then = Date.parse(iso);
    if (Number.isNaN(then)) return "";
    const secs = Math.max(0, Math.floor((Date.now() - then) / 1000));
    const units: [number, string][] = [
        [31536000, "year"],
        [2592000, "month"],
        [86400, "day"],
    ];
    for (const [size, name] of units) {
        const n = Math.floor(secs / size);
        if (n >= 1) return `${n} ${name}${n === 1 ? "" : "s"}`;
    }
    return "today";
}

// Build an SVG sparkline (points + filled area) from a weekly series, matching
// the package sidebar chart in the design.
export function sparkline(series: number[]): {
    points: string;
    area: string;
    endX: string;
    endY: string;
} {
    const n = series.length;
    const px = (i: number): number => (i / (n - 1)) * 100;
    const py = (v: number): number => 38 - (v / 100) * 34;
    const pts = series.map((v, i) => `${px(i).toFixed(2)},${py(v).toFixed(2)}`);
    return {
        points: pts.join(" "),
        area: `M0,40 L${pts.join(" L")} L100,40 Z`,
        endX: px(n - 1).toFixed(2),
        endY: py(series[n - 1]).toFixed(2),
    };
}
