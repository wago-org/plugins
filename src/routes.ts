const pluginIDPattern = /^(?:[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?\.)+[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\/[A-Za-z0-9](?:[A-Za-z0-9._~-]*[A-Za-z0-9])?)+$/;

// canonicalPluginIDFromPath accepts only a literal full-ID plugin route. The
// ID alphabet is already URL-safe, so percent-encoding, doubled/trailing
// slashes, short names, hashes, and v0 paths are all aliases and are rejected.
export function canonicalPluginIDFromPath(pathname: string): string | null {
    if (!pathname.startsWith("/") || pathname.includes("%")) return null;
    const id = pathname.slice(1);
    return id.startsWith("github.com/") && id.length <= 300 && pluginIDPattern.test(id) ? id : null;
}
