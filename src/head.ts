// Parser-blocking runtime configuration. Keeping this in a small external
// TypeScript output removes executable inline code from the HTML shell.
interface HeadWagoConfig {
    apiBase?: string;
}

(() => {
    const key = "wagoPluginsTheme";
    const systemTheme: "light" | "dark" = matchMedia(
        "(prefers-color-scheme: light)",
    ).matches
        ? "light"
        : "dark";
    let theme = systemTheme;

    try {
        const stored = localStorage.getItem(key);
        if (stored !== null) {
            const preference = JSON.parse(stored) as {
                theme?: unknown;
                system?: unknown;
            };
            if (
                (preference.theme === "light" || preference.theme === "dark") &&
                preference.system === systemTheme
            ) {
                theme = preference.theme;
            } else {
                localStorage.removeItem(key);
            }
        }
    } catch {
        // Storage may be unavailable; fall back to the operating-system theme.
    }

    document.documentElement.dataset.theme = theme;
    document
        .querySelector<HTMLMetaElement>('meta[name="theme-color"]')
        ?.setAttribute("content", theme === "light" ? "#f7f4ff" : "#1a1547");
})();

(window as Window & { WAGO_CONFIG?: HeadWagoConfig }).WAGO_CONFIG = {
    apiBase:
        location.hostname === "localhost" || location.hostname === "127.0.0.1"
            ? undefined
            : "https://api.plugins.wago.sh",
};
