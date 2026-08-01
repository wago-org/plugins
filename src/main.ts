// Entry point — boots the single-page registry app.
import { init } from "./app.js";
import { initTheme } from "./theme.js";

initTheme();

if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", () => void init());
} else {
    void init();
}
