// Shared data shapes for the registry frontend. These mirror the wago-plugin/v1
// manifest model (a package is a Go module shipping one or more subpackages) plus
// the derived fields the Go backend adds. The static data/packages.json ships
// the same shapes (minus derived fields, which the frontend fills in for the
// no-backend demo mode).

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

export interface Review {
    id?: string;
    userId?: string | number;
    author: string;
    avatarUrl?: string;
    rating: number;
    body: string;
    createdAt: string;
    score?: number;
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
    avatarUrl?: string;
    body: string;
    createdAt: string;
    parentId?: string;
    // derived for rendering
    initial?: string;
    bg?: string;
    mine?: boolean;
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
    starred?: boolean;
    installsWeek: number;
    installsWeekLabel: string;
    installsTotal?: number;

    issues?: Issue[];
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

export interface User {
    id: number | string;
    login: string;
    name: string;
    avatarUrl?: string;
    email?: string;
    bio?: string;
    initial: string;
    bg: string;
}
