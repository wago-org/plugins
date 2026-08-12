// Shared data shapes for the registry frontend. Published versions retain their
// exact provider definitions so authority review never depends on an aggregate.

export type Stability = "experimental" | "stable" | "deprecated";

export interface Compatibility {
    engines: Record<string, string>; // wago / tinygo / go → semver range
    platforms: string[];
}

export interface AuthorityScope {
    modules?: string[];
    maxInstances?: number;
    maxMemoryBytes?: number;
}

export interface AuthorityRequest {
    name: string;
    mode: "required" | "optional";
    reason: string;
    scope?: AuthorityScope;
    // UI-only attribution derived from the containing immutable provider.
    providerId?: string;
}

export interface PluginRequirement {
    id: string;
    version: string;
}

export interface ContractSpec {
    id: string;
    major: number;
}

export interface ContractRequirement extends ContractSpec {
    mode: "required" | "optional" | "many";
}

export interface PluginDefinition {
    id: string;
    name?: string;
    version: string;
    description?: string;
    stability?: Stability;
    compatibility?: Compatibility;
    requires?: PluginRequirement[];
    authorities?: AuthorityRequest[];
    configSchema?: Record<string, unknown>;
    provides?: ContractSpec[];
    consumes?: ContractRequirement[];
}

export interface PublishedProvider {
    id: string;
    importPath: string;
    source: { module: string; version: string; checksum: string };
    definition: PluginDefinition;
    definitionDigest: string;
}

export interface Author {
    name: string;
    email?: string;
    url?: string;
    github?: string;
}

export interface PackageSub {
    module: string;
    name: string;
    description: string;
    stability?: Stability;
    tags?: string[];
    engines?: Record<string, string>;
    platforms?: string[];
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
    sourceChecksum: string;
    providers: PublishedProvider[];
    releaseFingerprint: string;
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
    // Full canonical source Plugin ID, e.g. github.com/wago-org/wasi.
    id: string;
    // Internal registry key for API/social endpoints. New v1 publications use the
    // same full canonical module ID; it is not a public short-name alias.
    short: string;
    // Technical repository fallback; not a public package name.
    module: string;
    displayName?: string;
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
    canManage?: boolean; // backend-computed: may the current viewer manage this package (org-aware)
    allowedPublishers?: string[]; // extra logins the owner lets publish (beyond repo admins)
    pendingPublishers?: { login: string; id: string }[]; // outstanding publish invites (manager view)
    dependencies?: string[]; // exact Plugin IDs required by the latest source release
    readme?: string; // module-level readme (markdown)
    deprecatedMessage?: string;
    compatibility: Compatibility;
    authorities: AuthorityRequest[];
    providerIds?: string[]; // exact Plugin IDs exposed by the latest source release
    authors: Author[];
    // Source-package metadata only. Executable plugins come exclusively from
    // the immutable provider catalog on each VersionRow.
    subpackages?: PackageSub[];
    contributors: string[];

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
    search?: string; // precomputed lowercased haystack (id+description+tags+keywords)
    starred?: boolean;
    installsWeek: number;
    installsWeekLabel: string;
    installsMonth: number;
    installsMonthLabel: string;
    installsTotal?: number;

    issues?: Issue[];
}

// Notification is an actionable inbox item addressed to a user's GitHub login:
// an invite to publish a package, or an offer to receive ownership of one.
export interface Notification {
    id: string;
    recipient: string;
    kind: "publish-invite" | "transfer";
    packageShort: string;
    packageName: string;
    fromLogin: string;
    status: "pending" | "accepted" | "declined";
    createdAt: string;
    resolvedAt?: string;
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
    isOrg?: boolean; // this identity is an organization the user is acting as
    initial: string;
    bg: string;
}

// Account is one identity signed in on this browser, for the account switcher.
// Several can be signed in at once; exactly one is active.
export interface Account {
    id: string | number;
    login: string;
    name: string;
    avatarUrl?: string;
    active: boolean;
}

// OrgRef is one of the active account's GitHub organizations. canActAs is true
// for orgs the user owns/administers — those they may switch into and act as.
export interface OrgRef {
    login: string;
    name: string;
    avatarUrl?: string;
    role: string; // "admin" | "member"
    canActAs: boolean;
}

// Me is the full session view returned by /api/me: the active identity plus the
// switcher roster and the active account's organizations.
export interface Me {
    user: User;
    accounts: Account[];
    orgs: OrgRef[];
    activeOrg: string; // org login currently acted as, "" when personal
}
