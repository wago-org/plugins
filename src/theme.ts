type Theme = "light" | "dark";

const storageKey = "wagoPluginsTheme";
const lightQuery = "(prefers-color-scheme: light)";

function systemTheme(): Theme {
    return matchMedia(lightQuery).matches ? "light" : "dark";
}

function activeTheme(): Theme {
    const value = document.documentElement.dataset.theme;
    return value === "light" || value === "dark" ? value : systemTheme();
}

function renderTheme(theme: Theme): void {
    document.documentElement.dataset.theme = theme;
    document
        .querySelector<HTMLMetaElement>('meta[name="theme-color"]')
        ?.setAttribute("content", theme === "light" ? "#f7f4ff" : "#1a1547");
}

export function initTheme(): void {
    document.addEventListener("click", (event) => {
        const target = event.target;
        if (!(target instanceof Element) || !target.closest("[data-theme-toggle]")) return;

        const theme: Theme = activeTheme() === "dark" ? "light" : "dark";
        try {
            localStorage.setItem(
                storageKey,
                JSON.stringify({ theme, system: systemTheme() }),
            );
        } catch {
            // The toggle still works for this page load when storage is blocked.
        }
        renderTheme(theme);
    });

    matchMedia(lightQuery).addEventListener("change", (event) => {
        try {
            localStorage.removeItem(storageKey);
        } catch {
            // Theme switching still works when storage is blocked.
        }
        renderTheme(event.matches ? "light" : "dark");
    });
}
