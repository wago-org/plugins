// The single mutable app state. Screens render as a pure function of this; the
// event layer mutates it and asks for a re-render. Ephemeral UI bits (open
// menus, draft text, active tab) live here alongside loaded data.

import type { Comment, Issue, InstallPoint, Package, Registry, Review, User } from "./types.js";

export type Screen = "home" | "search" | "package" | "auth" | "account";
export type PkgTab = "readme" | "reviews" | "comments" | "issues" | "versions";
export type Sort = "popular" | "quality" | "recent";
export type AcctTab = "profile" | "plugins" | "settings";

export interface AppState {
    registry: Registry | null;
    user: User | null;
    screen: Screen;

    // search
    query: string;
    sort: Sort;
    verified: boolean;
    cats: Record<string, boolean>;

    // package view
    pkg: Package | null;
    pkgTab: PkgTab;
    reviews: Review[];
    reviewsSummary: { average: number; count: number };
    reviewsLoading: boolean;
    composerOpen: boolean;
    draftRating: number;
    hoverRating: number;
    draftText: string;
    reviewPreview: boolean; // review composer: Write | Preview
    issueFilter: "open" | "closed";
    starred: boolean;
    starCount: number;
    bookmarked: boolean;

    // GitHub issue sync (client-side fetch)
    ghIssues: Issue[] | null; // null = not synced; [] = synced but empty
    ghIssuesLoading: boolean;
    ghIssuesError: boolean; // couldn't reach GitHub / rate-limited

    // comments
    comments: Comment[];
    commentsLoading: boolean;
    commentDraft: string;
    commentPreview: boolean; // comment composer: Write | Preview
    replyTo: string | null;
    replyDraft: string;
    replyPreview: boolean; // reply composer: Write | Preview

    // install history (for the sidebar sparkline)
    installSeries: InstallPoint[];

    // account
    acctTab: AcctTab;
    menuOpen: boolean;
    settings: { releases: boolean; security: boolean; digest: boolean };
    bioDraft: string | null;

    // secondary emails (settings)
    emailDraft: string;
    verifyDrafts: Record<string, string>;
    emailMsg: string | null;

    authError: string | null;
}

export const state: AppState = {
    registry: null,
    user: null,
    screen: "home",

    query: "",
    sort: "popular",
    verified: false,
    cats: {},

    pkg: null,
    pkgTab: "readme",
    reviews: [],
    reviewsSummary: { average: 0, count: 0 },
    reviewsLoading: false,
    composerOpen: false,
    draftRating: 0,
    hoverRating: 0,
    draftText: "",
    reviewPreview: false,
    issueFilter: "open",
    starred: false,
    starCount: 0,
    bookmarked: false,

    ghIssues: null,
    ghIssuesLoading: false,
    ghIssuesError: false,

    comments: [],
    commentsLoading: false,
    commentDraft: "",
    commentPreview: false,
    replyTo: null,
    replyDraft: "",
    replyPreview: false,

    installSeries: [],

    acctTab: "profile",
    menuOpen: false,
    settings: { releases: true, security: true, digest: false },
    bioDraft: null,

    emailDraft: "",
    verifyDrafts: {},
    emailMsg: null,

    authError: null,
};

// Look up a package by its short slug (wasi-fs) or full module path.
export function findPackage(reg: Registry | null, key: string): Package | null {
    if (!reg) return null;
    return reg.packages.find((p) => p.short === key || p.name === key) || null;
}
