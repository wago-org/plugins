// Shared data shapes for the registry frontend. These mirror the wago-plugin/v1
// manifest model (a package is a Go module shipping one or more subpackages) plus
// the derived fields the Go backend adds. The static data/packages.json ships the
// catalog taxonomy and a fallback package list in the same shapes.

export type Stability = "experimental" | "stable" | "deprecated";

export interface Compatibility {
    engines: Record<string, string>; // wago / tinygo / go → semver range
    platforms: string[];
}

export interface Subpackage {
    import: string;
    id: string;
    name: string;
    version: string;
    description: string;
    stability: Stability;
    tags: string[];
    compatibility: Compatibility;
    readme?: string; // subpackage-specific readme (markdown); optional
}

export interface Author {
    name: string;
    github: string;
}

export interface VersionRow {
    version: string;
    commit: string;
    publishedAt: string;
    notes: string;
    unpackedKB: number;
    latest: boolean;
    installShare: number;
    deprecated?: boolean;
    hidden?: boolean; // placeholder release (0.0.0): not a real, citable version
}

export interface Issue {
    num: number;
    title: string;
    state: "open" | "closed";
    labels: string[];
    comments: number;
    age: string;
    author: string;
}

export interface Report {
    id: string;
    packageShort: string;
    reporterLogin: string;
    reason: string;
    detail?: string;
    createdAt: string;
    resolved?: boolean;
    resolvedBy?: string;
    resolvedAt?: string;
}

export interface Review {
    id?: string;
    userId?: string | number;
    author: string;
    login?: string; // GitHub login, for profile-pic enrichment
    avatarUrl?: string;
    rating: number;
    body: string;
    createdAt: string;
    score?: number;
    upvotes?: number;
    downvotes?: number;
    myVote?: "up" | "down" | null;
    mine?: boolean;
    // derived for rendering
    initial?: string;
    bg?: string;
}

export interface Comment {
    id: string;
    packageShort?: string;
    userId?: string | number;
    author: string;
    login?: string; // GitHub login, for profile-pic enrichment
    avatarUrl?: string;
    body: string;
    createdAt: string;
    parentId?: string;
    archived?: boolean; // soft-hidden by its author or the package owner
    // comment votes (mirrors reviews)
    score?: number;
    upvotes?: number;
    downvotes?: number;
    myVote?: "up" | "down" | null;
    // derived for rendering
    initial?: string;
    bg?: string;
    mine?: boolean;
    canModerate?: boolean; // viewer is the package owner (may archive others')
}

export interface Package {
    name: string; // module path, e.g. github.com/wago-org/wasi
    short: string;
    description: string;
    category: string;
    tags: string[];
    keywords?: string[];
    license: string;
    repository: string;
    homepage?: string;
    stability: Stability;
    verified: boolean;
    official?: boolean;
    ownerLogin?: string;
    allowedPublishers?: string[]; // extra logins the owner lets publish (beyond repo admins)
    dependencies?: string[]; // module paths this package depends on
    readme?: string; // module-level readme (markdown); fallback for subpackages
    deprecatedMessage?: string;
    compatibility: Compatibility;
    capabilities: string[];
    authors: Author[];
    contributors: string[];
    subpackages: Subpackage[];

    version: string; // latest version, convenience for cards
    latestVersion: string;
    versions: VersionRow[];
    updatedAt: string; // RFC3339 of the latest version

    rating: number;
    ratingCount: number;
    score: number;
    stars: number;
    forks?: number;
    unpackedKB?: number;

    // derived / backend-provided
    search?: string; // precomputed lowercased haystack (short+name+desc+tags+keywords)
    starred?: boolean;
    installsWeek: number;
    installsWeekLabel: string;
    installsTotal?: number;

    issues?: Issue[];
}

// A public profile of any user (author/contributor), shown at #/u/{login}.
// `claimed` distinguishes a real wago member (signed in) from a profile we
// generated from public registry + GitHub data.
export interface ViewUser {
    login: string;
    name: string;
    avatarUrl?: string;
    bio?: string;
    company?: string;
    location?: string;
    blog?: string;
    twitterUsername?: string;
    htmlUrl?: string;
    githubCreatedAt?: string;
    createdAt?: string; // wago join date (claimed members only)
    followers?: number;
    following?: number;
    publicRepos?: number;
    starsGiven?: number;
    claimed: boolean;
    isOrg?: boolean; // GitHub Organization vs a person
    orgs?: { login: string; avatarUrl: string }[]; // user's org memberships
    members?: { login: string; avatarUrl: string }[]; // org's public members
}

export interface CategoryDef {
    key: string;
    label: string;
    count: number;
}

export interface StatDef {
    value: string;
    label: string;
}

export interface Registry {
    packages: Package[];
    stats: StatDef[];
    categories: CategoryDef[];
}

export interface InstallPoint {
    date: string;
    count: number;
}

export interface UserEmail {
    address: string;
    verified: boolean;
    source: "github" | "added";
}

export interface User {
    id: number | string;
    login: string;
    name: string;
    avatarUrl?: string;
    email?: string;
    bio?: string;
    // rich GitHub profile
    company?: string;
    location?: string;
    blog?: string;
    twitterUsername?: string;
    htmlUrl?: string;
    githubCreatedAt?: string;
    followers?: number;
    following?: number;
    publicRepos?: number;
    hireable?: boolean;
    createdAt?: string; // when they joined wago (RFC3339), for membership duration
    emails?: UserEmail[];
    // Whether the user granted the public_repo scope, letting the registry star
    // repos on their behalf. Derived server-side; never the raw token.
    canStar?: boolean;
    admin?: boolean; // site-wide admin (moderate any package's discussion)
    initial: string;
    bg: string;
}
