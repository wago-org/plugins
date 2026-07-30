// Parser-blocking runtime configuration. Keeping this in a small external
// TypeScript output removes executable inline code from the HTML shell.
interface HeadWagoConfig {
    apiBase?: string;
}

(window as Window & { WAGO_CONFIG?: HeadWagoConfig }).WAGO_CONFIG = {
    apiBase:
        location.hostname === "localhost" || location.hostname === "127.0.0.1"
            ? undefined
            : "https://api.plugins.wago.sh",
};
