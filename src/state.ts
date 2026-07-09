// The single mutable app state. Screens render as a pure function of this; the
// event layer mutates it and asks for a re-render. Ephemeral UI bits (open
// menus, draft text, active tab) live here alongside loaded data.

import type { Comment, Issue, InstallPoint, Package, Registry, Review, User, ViewUser } from "./types.js";

export type Screen = "home" | "search" | "package" | "auth" | "account" | "user";
export type PkgTab = "readme" | "reviews" | "comments" | "issues" | "versions" | "subpackages" | "settings";
export type Sort = "popular" | "recent";
export type AcctTab = "profile" | "plugins" | "stars" | "settings";

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
    sub: string | null; // id of the subpackage whose page is open (else null)
    readme: string | null; // the package repo's README markdown, fetched from GitHub
    readmeLoading: boolean;
    readmeBase: { owner: string; repo: string; ref?: string } | null; // for resolving the README's relative image/link URLs
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

    // GitHub star consent flow (option 1: star the real repo on the user's
    // behalf). starPrompt shows the consent panel; starPromptDontAsk mirrors the
    // "don't ask again" checkbox inside it.
    starPrompt: boolean;
    starPromptDontAsk: boolean;

    // Report-a-package modal.
    reportOpen: boolean;
    reportReason: string;
    reportDetail: string;
    reportSending: boolean;
    reportDone: boolean;

    // Settings tab (package owner / admin).
    publisherDraft: string;
    publisherResults: { login: string; avatarUrl: string }[]; // GitHub autocomplete
    deprecateDraft: string;

    // GitHub issue sync (client-side fetch)
    ghIssues: Issue[] | null; // null = not synced; [] = synced but empty
    ghIssuesLoading: boolean;
    ghIssuesError: boolean; // couldn't reach GitHub / rate-limited

    reviewEditing: string | null; // id of the review being edited (composer prefilled)

    // comments
    comments: Comment[];
    commentsLoading: boolean;
    commentDraft: string;
    commentPreview: boolean; // comment composer: Write | Preview
    replyTo: string | null;
    replyDraft: string;
    replyPreview: boolean; // reply composer: Write | Preview
    commentEditing: string | null; // id of the comment being edited inline
    commentEditDraft: string;

    // install history (for the sidebar sparkline)
    installSeries: InstallPoint[];

    // public user profile (#/u/{login})
    viewUser: ViewUser | null;
    viewUserLoading: boolean;

    // account
    acctTab: AcctTab;
    starShorts: string[] | null; // packages the user has starred (null = unloaded)
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
    sub: null,
    readme: null,
    readmeLoading: false,
    readmeBase: null,
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
    starPrompt: false,
    starPromptDontAsk: false,
    reportOpen: false,
    reportReason: "",
    reportDetail: "",
    reportSending: false,
    reportDone: false,
    publisherDraft: "",
    publisherResults: [],
    deprecateDraft: "",

    ghIssues: null,
    ghIssuesLoading: false,
    ghIssuesError: false,

    reviewEditing: null,

    comments: [],
    commentsLoading: false,
    commentDraft: "",
    commentPreview: false,
    replyTo: null,
    replyDraft: "",
    replyPreview: false,
    commentEditing: null,
    commentEditDraft: "",

    installSeries: [],

    viewUser: null,
    viewUserLoading: false,

    acctTab: "profile",
    starShorts: null,
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
