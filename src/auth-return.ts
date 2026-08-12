const RETURN_TO_KEY = "wagoAuthReturnTo";

type HistoryState = Record<string, unknown> | null;

function isAuthURL(url: URL): boolean {
    return url.pathname.replace(/\/$/, "") === "/auth" || /^#\/auth(?:[/?]|$)/.test(url.hash);
}

function asSameOriginNonAuth(raw: unknown, base: URL): string | null {
    if (typeof raw !== "string") return null;
    try {
        const candidate = new URL(raw, base);
        return candidate.origin === base.origin && !isAuthURL(candidate) ? candidate.href : null;
    } catch {
        return null;
    }
}

// The /auth history entry remembers the non-auth page that opened it. Keeping
// this in history state avoids putting an internal return URL in the address bar.
export function authHistoryState(currentHref: string, currentState: unknown): HistoryState {
    const base = new URL(currentHref);
    const prior = typeof currentState === "object" && currentState !== null ? currentState : {};
    const returnTo = asSameOriginNonAuth(currentHref, base);
    return returnTo ? { ...prior, [RETURN_TO_KEY]: returnTo } : { ...prior };
}

// Protected pages can render the auth screen without changing their URL, so the
// current non-auth URL remains a valid return target. A direct /auth visit has no
// prior target and intentionally falls back to the site root.
export function authReturnTarget(currentHref: string, historyState: unknown): string {
    const current = new URL(currentHref);
    const saved =
        typeof historyState === "object" && historyState !== null
            ? asSameOriginNonAuth((historyState as Record<string, unknown>)[RETURN_TO_KEY], current)
            : null;
    return saved || asSameOriginNonAuth(currentHref, current) || new URL("/", current).href;
}
