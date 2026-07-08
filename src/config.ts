// Runtime configuration. The API base can be overridden by a small inline
// script in index.html (window.WAGO_CONFIG); otherwise we guess from the host:
// localhost during development, the production API host in the wild.
//
// The site is designed to work *without* a backend at all (GitHub Pages only):
// package data comes from the static data/packages.json, and social features
// (sign-in, stars, reviews) fall back to a local, in-browser stand-in. When the
// Go backend is reachable at API_BASE, those features become real and shared.

interface WagoConfig {
    apiBase?: string;
}

declare global {
    interface Window {
        WAGO_CONFIG?: WagoConfig;
    }
}

function guessApiBase(): string {
    const cfg = window.WAGO_CONFIG?.apiBase;
    if (typeof cfg === "string") return cfg.replace(/\/$/, "");
    const host = location.hostname;
    if (host === "localhost" || host === "127.0.0.1") {
        return "http://localhost:8787";
    }
    // Production default: an `api.` sibling of the site host. Adjust via
    // window.WAGO_CONFIG if the backend lives elsewhere.
    return `${location.protocol}//api.${host}`;
}

export const API_BASE = guessApiBase();

// Path to the static package index, served alongside the site.
export const PACKAGES_URL = "/data/packages.json";
