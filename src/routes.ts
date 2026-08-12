const pluginIDPattern = /^(?:[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?\.)+[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\/[A-Za-z0-9](?:[A-Za-z0-9._~-]*[A-Za-z0-9])?)+$/;

// Public plugin routes omit the only supported source host. Convert
// /owner/repo[/provider] back to the canonical github.com/owner/repo[/provider]
// ID used by the API and store. The ID alphabet is already URL-safe, so encoded,
// doubled, trailing-slash, and host-prefixed forms are rejected.
export function canonicalPluginIDFromPath(pathname: string): string | null {
    if (!pathname.startsWith("/") || pathname.startsWith("//") || pathname.includes("%")) return null;
    const relativeID = pathname.slice(1);
    const segments = relativeID.split("/");
    if (relativeID.startsWith("github.com/") || segments.length < 2 || segments[0].includes(".")) return null;
    const id = `github.com/${relativeID}`;
    return id.length <= 300 && pluginIDPattern.test(id) ? id : null;
}
