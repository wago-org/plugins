// Pure render functions: state in, HTML string out. Interactive elements carry
// data-act (+ optional data-arg) attributes that the event layer in app.ts
// dispatches on. Visual styling is inline, kept faithful to the DC design.

import type { AppState } from "./state.js";
import type {
    Comment,
    Issue,
    Package,
    Review,
    Stability,
    Subpackage,
    User,
    UserEmail,
    VersionRow,
} from "./types.js";
import { compactNum, esc, escAttr, fullDate, pkgPath, relativeDate, shortHash, sparkline, starStr, tier, weeklyBuckets } from "./util.js";
import { mdBlock } from "./markdown.js";
import { avatarFor } from "./github.js";

const C = {
    bg: "#1a1547",
    deep: "#161043",
    panel: "#221c52",
    panel2: "#25205a",
    line: "#2c2566",
    line2: "#443a8c",
    text: "#f3effd",
    dim: "#b9add9",
    soft: "#b3a6e0",
    muted: "#7d72b0",
    faint: "#6f64a8",
    lilac: "#c3a8ff",
    green: "#74e0ad",
    pink: "#ff9ec4",
    violet: "#5a3ff0",
};

// stability → accent colour trio (text, bg, border)
const STABILITY: Record<Stability, { color: string; bg: string; border: string }> = {
    stable: { color: "#74e0ad", bg: "#16322c", border: "#2e5a48" },
    experimental: { color: "#c3a8ff", bg: "#221c52", border: "#443a8c" },
    deprecated: { color: "#ffb4d2", bg: "#3a1f34", border: "#6b3453" },
};

function relative(iso: string): string {
    return iso ? relativeDate(iso) : "—";
}

// Ensure a blog value is a usable href (GitHub stores some without a scheme).
function profileHref(blog: string): string {
    return /^https?:\/\//i.test(blog) ? blog : `https://${blog}`;
}

// A follower / following / public-repos stat row, rendered only when GitHub
// supplied at least one of them.
function profileStats(u: User): string {
    const stats: { n: number; l: string }[] = [];
    if (u.followers != null) stats.push({ n: u.followers, l: "followers" });
    if (u.following != null) stats.push({ n: u.following, l: "following" });
    if (u.publicRepos != null) stats.push({ n: u.publicRepos, l: "repos" });
    if (!stats.length) return "";
    const cells = stats
        .map(
            (s) =>
                `<span style="font-family:'JetBrains Mono',monospace;font-size:12.5px;color:${C.muted}"><b style="color:${C.text};font-weight:700">${s.n.toLocaleString()}</b> ${s.l}</span>`,
        )
        .join(`<span style="color:${C.line2}">·</span>`);
    return `<div style="display:flex;gap:14px;flex-wrap:wrap;align-items:center">${cells}</div>`;
}

// Render the settings "Emails" section: GitHub primary + user-added secondaries,
// each with verified/unverified state and (when unverified) a code-entry box.
function emailsSection(s: AppState): string {
    const u = s.user!;
    const emails: UserEmail[] = u.emails || [];
    const rows = emails
        .map((e, i) => {
            const badge = e.verified
                ? `<span style="display:inline-flex;align-items:center;gap:5px;font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;color:${C.green}">✓ verified</span>`
                : `<span style="font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;color:#ffcf8f">unverified</span>`;
            const primary =
                e.source === "github"
                    ? `<span style="font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:700;color:${C.lilac};background:${C.deep};border:1px solid ${C.line2};padding:2px 7px;border-radius:100px">primary</span>`
                    : "";
            const code = s.verifyDrafts[e.address] || "";
            const verifyBox =
                !e.verified && e.source === "added"
                    ? `
              <div style="display:flex;gap:8px;align-items:center;margin-top:10px;flex-wrap:wrap">
                <input value="${escAttr(code)}" data-act="email-code" data-arg="${escAttr(e.address)}" placeholder="6-digit code" maxlength="6" inputmode="numeric" style="width:130px;background:${C.deep};border:1px solid ${C.line};border-radius:8px;padding:8px 11px;color:${C.text};font-family:'JetBrains Mono',monospace;font-size:13px;letter-spacing:2px;outline:none;box-sizing:border-box" />
                <button data-act="verify-email" data-arg="${escAttr(e.address)}" style="font-family:'JetBrains Mono',monospace;font-size:12px;font-weight:700;color:${C.bg};background:${C.lilac};border:none;padding:8px 14px;border-radius:8px;cursor:pointer">Verify</button>
                <span style="font-size:11.5px;color:${C.muted}">Enter the 6-digit code we emailed you.</span>
              </div>`
                    : "";
            const remove =
                e.source === "added"
                    ? `<button data-act="delete-email" data-arg="${escAttr(e.address)}" style="background:none;border:none;color:${C.pink};cursor:pointer;font-family:'JetBrains Mono',monospace;font-size:12px;padding:0">remove</button>`
                    : "";
            return `
            <div style="padding:14px 16px;border-top:${i === 0 ? "none" : `1px solid ${C.line}`};background:${C.deep};border:1px solid ${C.line};border-radius:12px;margin-bottom:10px">
              <div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap">
                <span style="font-size:14px;color:${C.text};font-weight:600;min-width:0;overflow:hidden;text-overflow:ellipsis">${esc(e.address)}</span>
                ${primary}
                ${badge}
                <span style="flex:1"></span>
                ${remove}
              </div>
              ${verifyBox}
            </div>`;
        })
        .join("");
    const msg = s.emailMsg
        ? `<div style="font-size:12.5px;color:${C.green};margin-bottom:10px">${esc(s.emailMsg)}</div>`
        : "";
    return `
        <div style="background:${C.panel};border:1px solid ${C.line};border-radius:16px;padding:22px 24px">
          <div style="margin-bottom:6px">
            <div style="font-weight:700;font-size:16px">Email addresses</div>
            <div style="font-size:12.5px;color:${C.muted};margin-top:4px">Your GitHub email is used by default. Use <b style="color:${C.dim};font-weight:700">Sync from GitHub</b> above to refresh it.</div>
          </div>
          ${msg}
          ${rows || `<div style="font-size:13.5px;color:${C.muted};margin-bottom:12px">No emails on file yet.</div>`}
          <div style="display:flex;gap:8px;margin-top:6px;flex-wrap:wrap">
            <input value="${escAttr(s.emailDraft)}" data-act="email-draft" placeholder="you@example.com" style="flex:1;min-width:180px;background:${C.deep};border:1px solid ${C.line};border-radius:10px;padding:11px 14px;color:${C.text};font-size:14px;outline:none;box-sizing:border-box" />
            <button data-act="add-email" style="font-family:'JetBrains Mono',monospace;font-size:13px;font-weight:700;color:${C.bg};background:${C.lilac};border:none;padding:11px 18px;border-radius:9px;cursor:pointer">Add email</button>
          </div>
        </div>`;
}

// ── shell: nav + footer ──────────────────────────────────────────────────────

export function nav(s: AppState): string {
    const homeColor = s.screen === "home" ? C.text : C.dim;
    const right = s.user ? profileMenu(s) : signInButton();
    return `
<nav style="position:sticky;top:0;z-index:60;display:flex;align-items:center;gap:20px;padding:14px 0;background:rgba(26,21,71,0.9);backdrop-filter:blur(12px);border-bottom:1px solid ${C.line}">
  <a href="/" data-act="home" style="display:flex;align-items:center;gap:11px;text-decoration:none;flex-shrink:0">
    <img src="/assets/wago-logo.png" alt="wago" style="width:34px;height:34px;border-radius:9px;flex-shrink:0" />
    <span style="font-weight:800;font-size:20px;letter-spacing:-0.5px">wago</span>
    <span style="font-family:'JetBrains Mono',monospace;font-size:11.5px;color:${C.lilac};border:1px solid ${C.line2};padding:3px 10px;border-radius:100px;margin-left:2px">packages</span>
  </a>
  <div style="flex:1;display:flex;align-items:center;gap:10px;background:${C.deep};border:1px solid ${C.line};border-radius:10px;padding:9px 14px;max-width:560px">
    <span style="color:${C.muted};font-size:15px">⌕</span>
    <input value="${escAttr(s.query)}" data-act="query" data-enter="search" placeholder="Search packages…" style="flex:1;background:transparent;border:none;outline:none;color:${C.text};font-size:14.5px" />
    <span style="font-family:'JetBrains Mono',monospace;font-size:10.5px;color:${C.muted};border:1px solid ${C.line};padding:2px 7px;border-radius:5px">↵</span>
  </div>
  <div style="display:flex;align-items:center;gap:14px;flex-shrink:0">
    <a href="/" data-act="home" style="text-decoration:none;font-size:14px;font-weight:600;color:${homeColor}">Browse</a>
    <a href="https://github.com/wago-org/wago" target="_blank" rel="noopener" style="text-decoration:none;padding:9px 16px;border-radius:9px;background:${C.lilac};color:${C.bg};font-weight:700;font-size:14px">Publish ↗</a>
    ${right}
  </div>
</nav>`;
}

function signInButton(): string {
    return `<a href="/auth" data-act="auth" style="text-decoration:none;padding:8px 15px;border-radius:9px;border:1px solid ${C.line2};color:${C.text};font-weight:600;font-size:14px">Sign in</a>`;
}

function avatarSpan(
    name: string,
    initial: string,
    bg: string,
    avatarUrl: string | undefined,
    size: number,
    font: number,
): string {
    if (avatarUrl)
        return `<img src="${escAttr(avatarUrl)}" alt="${escAttr(name)}" style="width:${size}px;height:${size}px;border-radius:50%;object-fit:cover;flex-shrink:0" />`;
    return `<span title="${escAttr(name)}" style="width:${size}px;height:${size}px;border-radius:50%;background:${bg};display:inline-flex;align-items:center;justify-content:center;font-family:'JetBrains Mono',monospace;font-weight:700;font-size:${font}px;color:${C.bg};flex-shrink:0">${esc(initial)}</span>`;
}

// Like avatarSpan, but prefers a real GitHub profile picture: an explicit
// avatarUrl wins, else a cached avatar for `login` (fetched lazily by the app
// layer), else the generated initial avatar.
function ghAvatarSpan(
    login: string | undefined,
    name: string,
    initial: string,
    bg: string,
    avatarUrl: string | undefined,
    size: number,
    font: number,
): string {
    const url = avatarUrl || (login ? avatarFor(login) || undefined : undefined);
    return avatarSpan(name, initial, bg, url, size, font);
}

// A small "Write | Preview" segmented control for a markdown composer. In
// preview mode the caller swaps the textarea for a rendered `.md` block.
function writePreviewTabs(preview: boolean, writeAct: string, previewAct: string): string {
    const btn = (label: string, on: boolean, act: string): string =>
        `<button data-act="${act}" style="font-family:'JetBrains Mono',monospace;font-size:11.5px;font-weight:700;color:${on ? C.bg : C.dim};background:${on ? C.lilac : "transparent"};border:none;padding:5px 12px;border-radius:6px;cursor:pointer">${label}</button>`;
    return `<div style="display:inline-flex;gap:2px;background:${C.panel};border:1px solid ${C.line};border-radius:8px;padding:3px;margin-bottom:10px">${btn("Write", !preview, writeAct)}${btn("Preview", preview, previewAct)}</div>`;
}

// A rendered markdown preview pane (or a muted "nothing to preview" note).
function previewPane(draft: string, minHeight: number): string {
    const body = draft.trim()
        ? mdBlock(draft)
        : `<span style="color:${C.muted};font-size:13px">Nothing to preview yet.</span>`;
    return `<div style="min-height:${minHeight}px;background:${C.panel};border:1px solid ${C.line};border-radius:10px;padding:12px 14px;box-sizing:border-box">${body}</div>`;
}

function profileMenu(s: AppState): string {
    const u = s.user!;
    const menuItems = [
        { label: "Your profile", icon: "◉", tab: "profile" },
        { label: "Your packages", icon: "▤", tab: "plugins" },
        { label: "Your stars", icon: "★", tab: "stars" },
        { label: "Settings", icon: "⚙", tab: "settings" },
    ]
        .map(
            (m) => `
        <a href="#/account" data-act="acct" data-arg="${m.tab}" style="display:flex;align-items:center;gap:11px;text-decoration:none;padding:9px 10px;border-radius:8px;font-size:13.5px;font-weight:600;color:#d8cef5">
          <span style="width:18px;text-align:center;color:${C.lilac}">${m.icon}</span> ${m.label}
        </a>`,
        )
        .join("");
    const dropdown = s.menuOpen
        ? `
    <div style="position:absolute;top:48px;right:0;width:236px;background:${C.panel};border:1px solid ${C.line};border-radius:14px;padding:8px;box-shadow:0 22px 44px -18px rgba(0,0,0,.7);z-index:80">
      <div style="display:flex;align-items:center;gap:10px;padding:8px 10px 12px;border-bottom:1px solid ${C.line};margin-bottom:6px">
        ${avatarSpan(u.name, u.initial, u.bg, u.avatarUrl, 38, 15)}
        <div style="min-width:0">
          <div style="font-size:14px;font-weight:700;color:${C.text};white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${esc(u.name)}</div>
          <div style="font-family:'JetBrains Mono',monospace;font-size:11.5px;color:${C.muted}">@${esc(u.login)}</div>
        </div>
      </div>
      ${menuItems}
      <div style="height:1px;background:${C.line};margin:6px 4px"></div>
      <a href="/" data-act="signout" style="display:flex;align-items:center;gap:11px;text-decoration:none;padding:9px 10px;border-radius:8px;font-size:13.5px;font-weight:600;color:${C.pink}">
        <span style="width:18px;text-align:center">⇥</span> Sign out
      </a>
    </div>`
        : "";
    return `
    <div data-profile-menu style="position:relative">
      <button data-act="menu-toggle" style="display:flex;align-items:center;gap:8px;background:transparent;border:1px solid ${C.line};border-radius:100px;padding:4px 11px 4px 4px;cursor:pointer">
        ${avatarSpan(u.name, u.initial, u.bg, u.avatarUrl, 28, 12)}
        <span style="font-size:13.5px;font-weight:600;color:${C.text}">${esc(u.login)}</span>
        <span style="color:${C.muted};font-size:10px">▾</span>
      </button>
      ${dropdown}
    </div>`;
}

export function footer(s: AppState): string {
    const total = s.registry?.stats?.[0]?.value ?? "1,240";
    return `
<footer style="border-top:1px solid ${C.line};margin-top:20px">
  <div style="display:flex;flex-wrap:wrap;justify-content:space-between;align-items:center;gap:20px;padding:28px 0">
    <div style="display:flex;align-items:center;gap:12px">
      <img src="/assets/wago-logo.png" alt="" style="width:34px;height:34px;border-radius:9px" />
      <span style="font-size:13px;color:${C.muted}">wago packages · ${esc(total)} indexed · Apache 2.0</span>
    </div>
    <div style="display:flex;gap:22px;font-size:14px;font-weight:600;color:${C.lilac}">
      <a href="https://github.com/wago-org/wago" target="_blank" rel="noopener" style="text-decoration:none">GitHub</a>
      <a href="/" data-act="home" style="text-decoration:none">Browse</a>
    </div>
  </div>
</footer>`;
}

function tagPills(tags: string[]): string {
    return tags
        .map(
            (t) =>
                `<span style="font-family:'JetBrains Mono',monospace;font-size:10.5px;color:${C.lilac};background:${C.deep};border:1px solid ${C.line};padding:3px 8px;border-radius:6px">${esc(t)}</span>`,
        )
        .join("");
}

function stabilityPill(st: Stability): string {
    const s = STABILITY[st] || STABILITY.stable;
    return `<span style="font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;color:${s.color};background:${s.bg};border:1px solid ${s.border};padding:4px 10px;border-radius:100px">${esc(st)}</span>`;
}

// ── home ─────────────────────────────────────────────────────────────────────

export function homeScreen(s: AppState): string {
    const reg = s.registry!;
    const stats = reg.stats
        .map(
            (st) => `
      <div style="background:${C.panel};border:1px solid ${C.line};border-radius:14px;padding:22px 24px;text-align:center">
        <div style="font-family:'JetBrains Mono',monospace;font-weight:700;font-size:34px;color:${C.lilac};line-height:1">${esc(st.value)}</div>
        <div style="font-size:13.5px;color:${C.muted};margin-top:6px">${esc(st.label)}</div>
      </div>`,
        )
        .join("");
    const cats = reg.categories
        .map(
            (c) =>
                `<button data-act="cat" data-arg="${escAttr(c.key)}" style="font-family:'JetBrains Mono',monospace;font-size:12.5px;color:#b9add9;background:${C.panel};border:1px solid ${C.line};padding:7px 14px;border-radius:100px;cursor:pointer">${esc(c.label)}</button>`,
        )
        .join("");
    const featured = reg.packages.slice(0, 6).map((p) => featuredCard(p)).join("");
    const recent = [...reg.packages]
        .sort((a, b) => (b.updatedAt || "").localeCompare(a.updatedAt || ""))
        .slice(0, 5)
        .map((p) => recentRow(p))
        .join("");
    const totalLabel = reg.stats[0]?.value ?? "1,240";

    return `
<div>
  <section style="text-align:center;padding:72px 0 40px">
    <div style="display:inline-flex;align-items:center;gap:8px;font-family:'JetBrains Mono',monospace;font-size:12px;color:${C.lilac};background:${C.panel};border:1px solid ${C.line};padding:6px 14px;border-radius:100px;margin-bottom:24px">✦ the wago package registry</div>
    <h1 style="font-weight:900;font-size:clamp(36px,5.5vw,58px);line-height:1.02;letter-spacing:-2px;margin:0 0 16px">Extend your runtime.<br><span style="color:${C.lilac}">One import away.</span></h1>
    <p style="font-size:18px;line-height:1.6;color:${C.soft};margin:0 auto 34px;max-width:560px">Host-import bundles, WASI shims, debuggers and codegen backends — drop-in Go modules for the wago engine.</p>
    <div style="display:flex;align-items:center;gap:11px;background:${C.deep};border:1px solid ${C.line};border-radius:14px;padding:14px 18px;max-width:600px;margin:0 auto 18px">
      <span style="color:${C.muted};font-size:20px">⌕</span>
      <input value="${escAttr(s.query)}" data-act="query" data-enter="search" placeholder="Search ${esc(totalLabel)} packages…" style="flex:1;background:transparent;border:none;outline:none;color:${C.text};font-size:17px" />
      <button data-act="search" style="font-family:'JetBrains Mono',monospace;font-size:13px;font-weight:700;color:${C.bg};background:${C.lilac};border:none;padding:9px 18px;border-radius:9px;cursor:pointer">Search</button>
    </div>
    <div style="display:flex;justify-content:center;gap:9px;flex-wrap:wrap">${cats}</div>
  </section>

  <section style="display:grid;grid-template-columns:repeat(3,1fr);gap:16px;margin:16px 0 56px">${stats}</section>

  <section style="margin-bottom:52px">
    <div style="display:flex;align-items:baseline;justify-content:space-between;margin-bottom:18px">
      <h2 style="font-weight:800;font-size:24px;letter-spacing:-0.6px;margin:0">Featured packages</h2>
      <a href="/search" data-act="search" style="text-decoration:none;font-family:'JetBrains Mono',monospace;font-size:12.5px;color:${C.lilac}">browse all →</a>
    </div>
    <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:16px">${featured || emptyCatalogNote()}</div>
  </section>

  <section style="margin-bottom:72px">
    <h2 style="font-weight:800;font-size:24px;letter-spacing:-0.6px;margin:0 0 18px">Recently updated</h2>
    <div style="display:grid;gap:1px;border:1px solid ${C.line};border-radius:14px;overflow:hidden">${recent || emptyCatalogNote()}</div>
  </section>
</div>`;
}

// Stars are the headline quality metric (reviews aren't released yet). A small
// ★-count chip, reused across cards, search rows, the header and profiles.
function starMetric(p: Package, size = 12): string {
    return `<span style="display:inline-flex;align-items:center;gap:5px;font-family:'JetBrains Mono',monospace;font-size:${size}px;color:${C.lilac}"><span>★</span>${compactNum(p.stars)}</span>`;
}

// Shown wherever a package list is empty (nothing published yet).
function emptyCatalogNote(): string {
    return `<div style="grid-column:1/-1;padding:44px;text-align:center;color:${C.muted};font-size:14px;background:${C.panel};border:1px solid ${C.line};border-radius:14px">No packages published yet. <a href="https://github.com/wago-org/wago" target="_blank" rel="noopener" style="color:${C.lilac};text-decoration:none;font-weight:700">Publish the first one ↗</a></div>`;
}

function featuredCard(p: Package): string {
    const verified = p.verified ? `<span title="verified" style="color:${C.green};font-size:13px">✦</span>` : "";
    return `
        <a href="${pkgPath(p)}" data-act="open" data-arg="${escAttr(p.short)}" style="text-decoration:none;display:flex;flex-direction:column;background:${C.panel};border:1px solid ${C.line};border-radius:16px;padding:20px 22px">
          <div style="display:flex;align-items:center;gap:8px;margin-bottom:9px">
            <span style="font-family:'JetBrains Mono',monospace;font-weight:700;font-size:15.5px;color:${C.text}">${esc(p.short)}</span>
            ${verified}
          </div>
          <p style="font-size:13.5px;line-height:1.5;color:${C.soft};margin:0 0 16px;flex:1">${esc(p.description)}</p>
          <div style="display:flex;gap:6px;flex-wrap:wrap;margin-bottom:14px">${tagPills(p.tags)}</div>
          <div style="display:flex;align-items:center;gap:6px;margin-bottom:12px">${starMetric(p, 12.5)}</div>
          <div style="display:flex;align-items:center;justify-content:space-between;font-family:'JetBrains Mono',monospace;font-size:11.5px;color:${C.muted};border-top:1px solid ${C.line};padding-top:12px">
            <span>↓ ${esc(p.installsWeekLabel)}/wk</span>
            <span style="color:${C.dim}">${esc(p.version)}</span>
          </div>
        </a>`;
}

function recentRow(p: Package): string {
    const verified = p.verified ? `<span style="color:${C.green};font-size:12px">✦</span>` : "";
    return `
        <a href="${pkgPath(p)}" data-act="open" data-arg="${escAttr(p.short)}" style="text-decoration:none;display:grid;grid-template-columns:1fr auto;gap:16px;align-items:center;padding:16px 22px;background:${C.panel};border-top:1px solid ${C.line}">
          <div style="min-width:0">
            <div style="display:flex;align-items:center;gap:8px;margin-bottom:3px">
              <span style="font-family:'JetBrains Mono',monospace;font-weight:700;font-size:14.5px;color:${C.text}">${esc(p.short)}</span>
              ${verified}
              <span style="font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.muted}">${esc(p.version)}</span>
            </div>
            <p style="font-size:13px;color:${C.soft};margin:0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${esc(p.description)}</p>
          </div>
          <div style="text-align:right;white-space:nowrap">
            <div>${starMetric(p, 12)}</div>
            <div style="font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.muted};margin-top:3px">${esc(relative(p.updatedAt))}</div>
          </div>
        </a>`;
}

// ── search ───────────────────────────────────────────────────────────────────

// The result rows for the current query/filters/sort — extracted so the search
// bar can patch them live (see app.ts) without re-rendering the whole screen.
export function searchRows(s: AppState): string {
    return (
        filterPackages(s)
            .map((p) => searchRow(p))
            .join("") ||
        `<div style="padding:44px;text-align:center;color:${C.muted};font-size:14px;background:${C.panel};border:1px solid ${C.line};border-radius:14px">No packages${s.query ? ` matching “${esc(s.query)}”` : " published yet"}.</div>`
    );
}

// The "N results for X" summary line — also patched live.
export function searchSummary(s: AppState): string {
    const n = filterPackages(s).length;
    const shown = s.query || "all packages";
    return `${n} result${n === 1 ? "" : "s"} for <span style="color:${C.text};font-weight:700">"${esc(shown)}"</span>`;
}

export function searchScreen(s: AppState): string {
    const reg = s.registry!;
    const filterCats = reg.categories
        .map((f) => {
            const on = !!s.cats[f.key];
            return `
          <label style="display:flex;align-items:center;gap:9px;font-size:13.5px;color:${C.soft};cursor:pointer">
            <span data-act="filter-cat" data-arg="${escAttr(f.key)}" style="width:15px;height:15px;border-radius:4px;border:1px solid ${on ? C.lilac : C.line2};background:${on ? C.lilac : "transparent"};display:inline-flex;align-items:center;justify-content:center;color:${C.bg};font-size:10px;flex-shrink:0">${on ? "✓" : ""}</span>
            <span style="flex:1" data-act="filter-cat" data-arg="${escAttr(f.key)}">${esc(f.label)}</span>
            <span style="font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.muted}">${f.count}</span>
          </label>`;
        })
        .join("");
    const sorts = (["popular", "recent"] as const)
        .map((k) => {
            const on = s.sort === k;
            return `<button data-act="sort" data-arg="${k}" style="font-family:'JetBrains Mono',monospace;font-size:12px;font-weight:600;color:${on ? C.bg : C.dim};background:${on ? C.lilac : "transparent"};border:none;padding:6px 12px;border-radius:6px;cursor:pointer">${k}</button>`;
        })
        .join("");
    return `
<div style="display:grid;grid-template-columns:220px 1fr;gap:32px;padding:32px 0 72px;align-items:start">
  <aside style="position:sticky;top:78px;display:flex;flex-direction:column;gap:26px">
    <div>
      <div style="font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;letter-spacing:1px;color:${C.muted};text-transform:uppercase;margin-bottom:12px">Category</div>
      <div style="display:flex;flex-direction:column;gap:9px">${filterCats}</div>
    </div>
    <div>
      <div style="font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;letter-spacing:1px;color:${C.muted};text-transform:uppercase;margin-bottom:12px">Options</div>
      <label data-act="toggle-verified" style="display:flex;align-items:center;gap:9px;font-size:13.5px;color:${C.soft};cursor:pointer">
        <span style="width:32px;height:18px;border-radius:100px;background:${s.verified ? C.green : C.line};position:relative;flex-shrink:0;transition:background .2s"><span style="position:absolute;top:2px;left:${s.verified ? "16px" : "2px"};width:14px;height:14px;border-radius:50%;background:#fff;transition:left .2s"></span></span>
        Verified only
      </label>
    </div>
    <div>
      <div style="font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;letter-spacing:1px;color:${C.muted};text-transform:uppercase;margin-bottom:12px">License</div>
      <div style="display:flex;flex-direction:column;gap:8px;font-size:13.5px;color:${C.soft}">
        <span>Apache-2.0</span><span>MIT</span><span>BSD-3-Clause</span>
      </div>
    </div>
  </aside>

  <div style="min-width:0">
    <div style="display:flex;align-items:baseline;justify-content:space-between;margin-bottom:18px;flex-wrap:wrap;gap:10px">
      <div id="search-summary" style="font-size:15px;color:${C.soft}">${searchSummary(s)}</div>
      <div style="display:flex;align-items:center;gap:10px">
        <span style="font-family:'JetBrains Mono',monospace;font-size:12px;color:${C.muted}">sort</span>
        <div style="display:flex;gap:2px;background:${C.deep};border:1px solid ${C.line};border-radius:9px;padding:3px">${sorts}</div>
      </div>
    </div>
    <div id="pkg-results" style="display:flex;flex-direction:column;gap:12px">${searchRows(s)}</div>
  </div>
</div>`;
}

function searchRow(p: Package): string {
    const verified = p.verified
        ? `<span style="font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:700;color:${C.bg};background:${C.green};padding:2px 8px;border-radius:100px">✦ verified</span>`
        : "";
    return `
        <a href="${pkgPath(p)}" data-act="open" data-arg="${escAttr(p.short)}" style="text-decoration:none;display:block;background:${C.panel};border:1px solid ${C.line};border-radius:14px;padding:20px 22px">
          <div style="display:flex;align-items:center;gap:9px;margin-bottom:7px;flex-wrap:wrap">
            <span style="font-family:'JetBrains Mono',monospace;font-weight:700;font-size:16px;color:${C.lilac}">${esc(p.short)}</span>
            ${verified}
            <span style="font-family:'JetBrains Mono',monospace;font-size:11.5px;color:${C.muted}">${esc(p.version)}</span>
          </div>
          <p style="font-size:14px;line-height:1.55;color:${C.soft};margin:0 0 13px">${esc(p.description)}</p>
          <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-bottom:13px">${tagPills(p.tags)}</div>
          <div style="display:flex;align-items:center;gap:20px;font-family:'JetBrains Mono',monospace;font-size:11.5px;color:${C.muted};flex-wrap:wrap">
            <span style="display:flex;align-items:center;gap:6px">${starMetric(p, 12.5)}<span style="color:${C.dim}">stars</span></span>
            <span>↓ ${esc(p.installsWeekLabel)}/wk</span>
            <span>updated ${esc(relative(p.updatedAt))}</span>
          </div>
        </a>`;
}

export function filterPackages(s: AppState): Package[] {
    const reg = s.registry!;
    const q = s.query.trim().toLowerCase();
    const activeCats = Object.keys(s.cats).filter((k) => s.cats[k]);
    let list = reg.packages.filter((p) => {
        if (s.verified && !p.verified) return false;
        if (activeCats.length && !activeCats.includes(p.category)) return false;
        if (q) {
            // Use the haystack precomputed at load time; fall back for any package
            // that didn't go through normalizePackage.
            const hay =
                p.search ??
                `${p.short} ${p.name} ${p.description} ${p.tags.join(" ")} ${(p.keywords || []).join(" ")}`.toLowerCase();
            if (!hay.includes(q)) return false;
        }
        return true;
    });
    list = [...list];
    // Stars are the headline metric while reviews are unreleased.
    if (s.sort === "popular") list.sort((a, b) => b.stars - a.stars || b.installsWeek - a.installsWeek);
    else if (s.sort === "recent") list.sort((a, b) => (b.updatedAt || "").localeCompare(a.updatedAt || ""));
    return list;
}

// ── package ──────────────────────────────────────────────────────────────────

export function packageScreen(s: AppState): string {
    const p = s.pkg!;
    const openIssueCount = (p.issues || []).filter((i) => i.state === "open").length;
    const tabEl = (k: string, label: string, extra = ""): string => {
        const on = s.pkgTab === k;
        return `<span data-act="tab" data-arg="${k}" style="font-size:14px;font-weight:${on ? "700" : "500"};color:${on ? C.text : C.muted};padding-bottom:12px;border-bottom:${on ? `2px solid ${C.lilac}` : "2px solid transparent"};margin-bottom:-1px;cursor:pointer;${extra}">${esc(label)}</span>`;
    };
    const tabs = [
        tabEl("readme", "Readme"),
        tabEl("comments", `Comments · ${s.comments.length}`),
        tabEl("issues", `Issues · ${openIssueCount}`),
        tabEl("versions", `Versions · ${p.versions.length}`),
    ].join("");
    // Subpackages + Settings tabs are anchored to the far right of the tab bar.
    const subTab = p.subpackages.length
        ? tabEl("subpackages", `Subpackages · ${p.subpackages.length}`, "margin-left:auto")
        : "";
    const settingsTabEl = canManagePackage(s)
        ? tabEl("settings", "⚙ Settings", subTab ? "" : "margin-left:auto")
        : "";

    const badges = `${p.verified ? `<span style="font-family:'JetBrains Mono',monospace;font-size:12px;font-weight:700;color:${C.bg};background:${C.green};padding:5px 11px;border-radius:100px">✦ verified</span>` : ""}<span style="font-family:'JetBrains Mono',monospace;font-size:13px;color:${C.lilac};border:1px solid ${C.line2};padding:5px 11px;border-radius:100px">${esc(p.version)}</span>`;

    // bookmark (save-for-later, per-browser) + star, to the right of the badges
    const bm = s.bookmarked;
    const bookmarkBtn = `<button data-act="bookmark" title="${bm ? "Saved" : "Save for later"}" style="display:inline-flex;align-items:center;gap:8px;font-family:'Outfit',sans-serif;font-weight:700;font-size:13.5px;color:${C.text};background:${bm ? C.panel : "transparent"};border:1px solid ${bm ? C.lilac : C.line2};padding:8px 14px;border-radius:9px;cursor:pointer;transition:all .15s">${bookmarkIcon(15, bm ? C.lilac : C.muted, bm)} ${bm ? "Saved" : "Save"}</button>`;
    const starBtn = `<button data-act="star" title="${s.starred ? "In your stars — opens the repo on GitHub" : "Star this repository on GitHub"}" style="display:inline-flex;align-items:center;gap:8px;font-family:'Outfit',sans-serif;font-weight:700;font-size:13.5px;color:${C.text};background:${s.starred ? C.panel : "transparent"};border:1px solid ${s.starred ? C.lilac : C.line2};padding:8px 14px;border-radius:9px;cursor:pointer;transition:all .15s"><span style="font-size:15px;color:${s.starred ? C.lilac : C.muted}">★</span> ${s.starred ? "Starred" : "Star"} <span style="font-family:'JetBrains Mono',monospace;font-weight:700;font-size:12.5px;background:${C.deep};padding:2px 8px;border-radius:6px">${s.starCount.toLocaleString()}</span></button>`;
    // Opens the source repository on GitHub, alongside save + star. Omitted when
    // the package has no repository (an empty href would navigate to the site root).
    const repoBtn = p.repository
        ? `<a href="${escAttr(p.repository)}" target="_blank" rel="noopener" title="Open the repository on GitHub" style="display:inline-flex;align-items:center;gap:8px;text-decoration:none;font-family:'Outfit',sans-serif;font-weight:700;font-size:13.5px;color:${C.text};background:transparent;border:1px solid ${C.line2};padding:8px 14px;border-radius:9px;cursor:pointer;transition:all .15s"><span style="font-size:14px;color:${C.muted}">⎇</span> Repo ↗</a>`
        : "";

    return `
<div style="padding:28px 0 72px">
  <div style="font-family:'JetBrains Mono',monospace;font-size:12.5px;color:${C.muted};margin-bottom:16px"><a href="/" data-act="home" style="text-decoration:none;color:${C.muted}">packages</a> <span style="color:${C.line2}">/</span> <a href="/search" data-act="cat" data-arg="${escAttr(p.category)}" style="text-decoration:none;color:${C.muted}">${esc(p.category)}</a> <span style="color:${C.line2}">/</span> <span style="color:${C.lilac}">${esc(p.short)}</span></div>

  <div style="display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin-bottom:12px">
    <h1 style="font-family:'JetBrains Mono',monospace;font-weight:700;font-size:clamp(24px,3.4vw,34px);letter-spacing:-1px;margin:0;word-break:break-all">${esc(p.name)}</h1>
    ${badges}
    <div style="margin-left:auto;display:flex;gap:8px;flex-shrink:0">${bookmarkBtn}${starBtn}${repoBtn}</div>
  </div>
  <p style="font-size:17px;line-height:1.6;color:${C.soft};margin:0 0 14px;max-width:680px">${esc(p.description)}</p>
  <div style="display:flex;align-items:center;gap:18px;font-family:'JetBrains Mono',monospace;font-size:12.5px;color:${C.muted};margin:0 0 28px;flex-wrap:wrap">
    <span style="display:inline-flex;align-items:center;gap:7px">${starMetric(p, 14)}<span>stars</span></span>
    <span>${p.subpackages.length} subpackage${p.subpackages.length === 1 ? "" : "s"}</span>
    <span>${p.license || "—"}</span>
  </div>

  ${p.deprecatedMessage ? deprecationBanner(p.deprecatedMessage) : ""}
  <div style="display:grid;grid-template-columns:1fr 300px;gap:36px;align-items:start">
    <main style="min-width:0">
      ${
          s.sub
              ? subpackageDetail(s)
              : `<div style="display:flex;gap:24px;border-bottom:1px solid ${C.line};margin-bottom:28px;flex-wrap:wrap">${tabs}${subTab}${settingsTabEl}</div>
      ${pkgTabBody(s)}`
      }
    </main>
    ${pkgSidebar(s)}
  </div>
  ${s.starPrompt ? starConsentModal(s) : ""}
  ${s.reportOpen ? reportModal(s) : ""}
</div>`;
}

// Consent panel shown the first time a signed-in user without star permission
// tries to star a package. They can grant permission (so we star the real repo
// for them from then on), just be sent to GitHub to star manually, and tick
// "don't ask again" to skip this next time.
function starConsentModal(s: AppState): string {
    const repo = s.pkg?.short ? esc(s.pkg.short) : "this package";
    const check = s.starPromptDontAsk;
    return `
    <div data-act="star-cancel" style="position:fixed;inset:0;z-index:120;background:rgba(11,8,32,0.66);backdrop-filter:blur(3px);display:flex;align-items:center;justify-content:center;padding:20px">
      <div data-act="noop" style="width:100%;max-width:420px;background:${C.panel};border:1px solid ${C.line2};border-radius:18px;padding:24px 24px 20px;box-shadow:0 30px 60px -20px rgba(0,0,0,.75)">
        <div style="display:flex;align-items:center;gap:10px;margin-bottom:12px">
          <span style="font-size:22px;color:${C.lilac}">★</span>
          <h2 style="font-weight:800;font-size:18px;margin:0">Star ${repo} on GitHub</h2>
        </div>
        <p style="font-size:14px;line-height:1.6;color:${C.soft};margin:0 0 8px">Stars here are real GitHub stars. Let wago star the repository for you and it happens in one click — we only ask for permission to star public repos on your behalf, nothing else.</p>
        <p style="font-size:12.5px;line-height:1.55;color:${C.muted};margin:0 0 18px">Prefer to do it yourself? We can just open the repo on GitHub so you can hit ★ there.</p>
        <label data-act="star-dontask" style="display:flex;align-items:center;gap:9px;font-size:13px;color:${C.soft};cursor:pointer;margin-bottom:18px">
          <span style="width:17px;height:17px;border-radius:5px;border:1px solid ${check ? C.lilac : C.line2};background:${check ? C.lilac : "transparent"};display:inline-flex;align-items:center;justify-content:center;color:${C.bg};font-size:11px;flex-shrink:0">${check ? "✓" : ""}</span>
          Don't ask again on this browser
        </label>
        <div style="display:flex;flex-direction:column;gap:9px">
          <button data-act="star-allow" style="width:100%;font-family:'Outfit',sans-serif;font-weight:700;font-size:14px;color:${C.bg};background:${C.lilac};border:none;padding:12px;border-radius:11px;cursor:pointer">Allow wago to star for me</button>
          <button data-act="star-just-github" style="width:100%;font-family:'Outfit',sans-serif;font-weight:700;font-size:14px;color:${C.text};background:transparent;border:1px solid ${C.line2};padding:12px;border-radius:11px;cursor:pointer">Just open GitHub ↗</button>
          <button data-act="star-cancel" style="width:100%;font-family:'JetBrains Mono',monospace;font-weight:600;font-size:12.5px;color:${C.muted};background:transparent;border:none;padding:6px;border-radius:8px;cursor:pointer">Cancel</button>
        </div>
      </div>
    </div>`;
}

function deprecationBanner(message: string): string {
    return `<div style="display:flex;gap:12px;align-items:flex-start;background:#3a1f34;border:1px solid #6b3453;border-radius:12px;padding:14px 18px;margin:0 0 24px">
    <span style="color:#ffb4d2;font-size:16px;flex-shrink:0">⚠</span>
    <div><span style="font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;letter-spacing:1px;color:#ffb4d2;text-transform:uppercase">Deprecated</span><p style="font-size:14px;line-height:1.55;color:#f0c9dc;margin:4px 0 0">${esc(message)}</p></div>
  </div>`;
}

function pkgTabBody(s: AppState): string {
    switch (s.pkgTab) {
        case "reviews":
            return reviewsTab(s);
        case "comments":
            return commentsTab(s);
        case "issues":
            return issuesTab(s);
        case "versions":
            return versionsTab(s);
        case "subpackages":
            return subpackagesTab(s);
        case "settings":
            return settingsTab(s);
        default:
            return readmeTab(s);
    }
}

// The Subpackages tab: intro + the grid of subpackage cards (each opens its own
// page). Replaces the old block that lived at the bottom of the readme.
function subpackagesTab(s: AppState): string {
    const p = s.pkg!;
    if (!p.subpackages?.length) {
        return `<div style="padding:20px;color:${C.muted};font-size:14px;background:${C.panel};border-radius:12px">This module ships no subpackages.</div>`;
    }
    return `
  <h2 style="font-weight:800;font-size:22px;letter-spacing:-0.5px;margin:0 0 8px">Subpackages <span style="font-family:'JetBrains Mono',monospace;font-size:14px;color:${C.muted};font-weight:500">${p.subpackages.length}</span></h2>
  <p style="font-size:14px;line-height:1.6;color:${C.soft};margin:0 0 18px;max-width:640px">Each is an independently importable wago extension, version-stamped and documented on its own page.</p>
  <div style="display:flex;flex-direction:column;gap:12px">${subpackageCards(p)}</div>`;
}

// A single subpackage's page: its own readme (or the module readme as a
// fallback), reached by clicking a card or via /{owner}/{short}/{id}.
function subpackageDetail(s: AppState): string {
    const p = s.pkg!;
    const e = p.subpackages.find((x) => x.id === s.sub);
    if (!e) {
        // Unknown subpackage id — fall back to the subpackages listing.
        return subpackagesTab(s);
    }
    const body = e.readme || p.readme || "";
    const readmeHtml = body
        ? mdBlock(body)
        : `<p style="font-size:15px;line-height:1.65;color:${C.soft};margin:0 0 20px">${esc(e.description)}</p>
       <div style="font-size:13px;color:${C.muted};background:${C.panel};border:1px solid ${C.line};border-radius:12px;padding:16px 18px">No dedicated readme was provided for this subpackage.</div>`;
    return `
  <div style="font-family:'JetBrains Mono',monospace;font-size:12.5px;color:${C.muted};margin-bottom:18px">
    <span data-act="tab" data-arg="subpackages" style="color:${C.lilac};cursor:pointer">← ${esc(p.short)} subpackages</span>
  </div>
  <div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin-bottom:8px">
    <h2 style="font-family:'JetBrains Mono',monospace;font-weight:700;font-size:22px;letter-spacing:-0.5px;margin:0">${esc(e.id)}</h2>
    ${stabilityPill(e.stability)}
    <span style="font-family:'JetBrains Mono',monospace;font-size:12px;color:${C.muted}">v${esc(e.version)}</span>
  </div>
  <div style="font-family:'JetBrains Mono',monospace;font-size:12px;color:${C.muted};margin-bottom:20px">import <span style="color:${C.lilac}">"${esc(e.import)}"</span> · <a href="https://pkg.go.dev/${escAttr(e.import)}" target="_blank" rel="noopener" style="color:${C.lilac};text-decoration:none">docs ↗</a></div>
  ${readmeHtml}`;
}

// The Readme tab shows the package repository's real README.md, fetched from
// GitHub client-side and rendered as markdown.
function readmeTab(s: AppState): string {
    const p = s.pkg!;
    if (s.readme) {
        // Render like GitHub: images/task-lists kept, relative URLs resolved to
        // the repo, @-mentions left literal (not in-app profile links).
        return `<div class="readme">${mdBlock(s.readme, { images: true, mentions: false, base: s.readmeBase ?? undefined })}</div>`;
    }
    if (s.readmeLoading) {
        return `<div style="padding:22px;color:${C.muted};font-size:14px">Loading README from GitHub…</div>`;
    }
    const viewLink = p.repository
        ? ` <a href="${escAttr(p.repository)}" target="_blank" rel="noopener" style="color:${C.lilac};text-decoration:none">View on GitHub ↗</a>`
        : "";
    return `<div style="padding:24px;background:${C.panel};border:1px solid ${C.line};border-radius:12px;color:${C.muted};font-size:14px">No README found in the repository.${viewLink}</div>`;
}

// The grid of subpackage cards. Each opens the subpackage's own page in-app
// (data-act=open-sub) at /{owner}/{short}/{id}.
function subpackageCards(p: Package): string {
    return p.subpackages
        .map(
            (e: Subpackage) => `
      <a href="${pkgPath(p)}/${escAttr(e.id)}" data-act="open-sub" data-arg="${escAttr(e.id)}" class="ext-card" style="text-decoration:none;display:block;background:${C.deep};border:1px solid ${C.line};border-radius:12px;padding:16px 18px;cursor:pointer">
        <div style="display:flex;align-items:center;gap:9px;flex-wrap:wrap;margin-bottom:6px">
          <span style="font-family:'JetBrains Mono',monospace;font-weight:700;font-size:14px;color:${C.lilac}">${esc(e.id)}</span>
          ${stabilityPill(e.stability)}
          <span style="font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.muted}">v${esc(e.version)}</span>
          <span style="margin-left:auto;color:${C.muted};font-size:12px">open →</span>
        </div>
        <p style="font-size:13.5px;line-height:1.55;color:${C.soft};margin:0 0 10px">${esc(e.description)}</p>
        <div style="font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.muted};margin-bottom:8px">import <span style="color:${C.lilac}">"${esc(e.import)}"</span></div>
        <div style="display:flex;gap:6px;flex-wrap:wrap">${tagPills(e.tags || [])}</div>
      </a>`,
        )
        .join("");
}

function reviewsTab(s: AppState): string {
    const p = s.pkg!;
    const activeRating = s.hoverRating || s.draftRating;
    const composer = s.user
        ? s.composerOpen
            ? composerOpenBlock(s, activeRating)
            : composerClosedBlock()
        : signInPrompt("star this package or leave a review");
    const list = s.reviewsLoading
        ? loading("Loading reviews…")
        : s.reviews.map((r) => reviewCard(r)).join("") || empty("No reviews yet. Be the first.");
    return `
<div>
  <div style="display:flex;align-items:center;gap:22px;background:${C.panel};border:1px solid ${C.line};border-radius:14px;padding:20px 24px;margin-bottom:22px;flex-wrap:wrap">
    <div style="text-align:center">
      <div style="font-weight:900;font-size:42px;line-height:1;color:${C.text}">${p.rating}</div>
      <div style="font-size:16px;letter-spacing:2px;color:${tier(p.score)};margin-top:4px">${starStr(p.rating)}</div>
      <div style="font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.muted};margin-top:5px">${s.reviewsSummary.count || p.ratingCount} ratings</div>
    </div>
    <div style="flex:1;min-width:220px;font-size:14px;line-height:1.6;color:${C.soft}">Developers rate this package highly for reliability and documentation. Share how it worked for you.</div>
  </div>
  ${composer}
  <div style="display:flex;flex-direction:column;gap:14px">${list}</div>
</div>`;
}

function signInPrompt(what: string): string {
    return `
  <div style="display:flex;align-items:center;justify-content:space-between;gap:12px;background:${C.deep};border:1px solid ${C.line};border-radius:14px;padding:14px 20px;margin-bottom:24px;flex-wrap:wrap">
    <span style="font-size:13.5px;color:${C.soft}">Sign in with GitHub to ${esc(what)}.</span>
    <a href="/auth" data-act="auth" style="text-decoration:none;font-family:'JetBrains Mono',monospace;font-size:12.5px;font-weight:700;color:${C.lilac};background:transparent;border:1px solid ${C.line2};padding:8px 15px;border-radius:9px;white-space:nowrap">Sign in</a>
  </div>`;
}

function loading(txt: string): string {
    return `<div style="color:${C.muted};font-size:14px;padding:20px">${esc(txt)}</div>`;
}
function empty(txt: string): string {
    return `<div style="color:${C.muted};font-size:14px;padding:20px">${esc(txt)}</div>`;
}

function composerClosedBlock(): string {
    return `
  <div style="display:flex;align-items:center;justify-content:space-between;gap:12px;background:${C.deep};border:1px solid ${C.line};border-radius:14px;padding:14px 20px;margin-bottom:24px;flex-wrap:wrap">
    <span style="font-size:13.5px;color:${C.soft}">Used this package? A quick ★ star above helps others — or leave a full review.</span>
    <button data-act="composer-open" style="font-family:'JetBrains Mono',monospace;font-size:12.5px;font-weight:700;color:${C.lilac};background:transparent;border:1px solid ${C.line2};padding:8px 15px;border-radius:9px;cursor:pointer;white-space:nowrap">Write a review</button>
  </div>`;
}

function composerOpenBlock(s: AppState, activeRating: number): string {
    const stars = [1, 2, 3, 4, 5]
        .map(
            (n) =>
                `<span class="pick-star" data-n="${n}" data-act="rate" data-arg="${n}" style="color:${n <= activeRating ? C.lilac : "#4b407e"};cursor:pointer">${n <= activeRating ? "★" : "☆"}</span>`,
        )
        .join("");
    const editor = s.reviewPreview
        ? previewPane(s.draftText, 80)
        : `<textarea data-act="draft" placeholder="How did it work for you? Markdown supported." style="width:100%;min-height:80px;background:${C.panel};border:1px solid ${C.line};border-radius:10px;padding:12px 14px;color:${C.text};font-size:14px;font-family:'Outfit',sans-serif;resize:vertical;outline:none;box-sizing:border-box">${esc(s.draftText)}</textarea>`;
    return `
  <div style="background:${C.deep};border:1px solid ${C.line};border-radius:14px;padding:18px 20px;margin-bottom:24px">
    <div style="font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;letter-spacing:1px;color:${C.muted};text-transform:uppercase;margin-bottom:12px">${s.reviewEditing ? "Edit your review" : "Write a review"}</div>
    <div id="star-picker" style="display:flex;gap:4px;font-size:26px;margin-bottom:12px;width:max-content">${stars}</div>
    ${writePreviewTabs(s.reviewPreview, "review-write", "review-preview")}
    ${editor}
    <div style="display:flex;justify-content:flex-end;gap:10px;margin-top:12px">
      <button data-act="composer-close" style="font-family:'JetBrains Mono',monospace;font-size:13px;font-weight:600;color:${C.dim};background:transparent;border:1px solid ${C.line};padding:10px 18px;border-radius:9px;cursor:pointer">Cancel</button>
      <button data-act="review-submit" style="font-family:'JetBrains Mono',monospace;font-size:13px;font-weight:700;color:${C.bg};background:${C.lilac};border:none;padding:10px 20px;border-radius:9px;cursor:pointer">${s.reviewEditing ? "Update review" : "Post review"}</button>
    </div>
  </div>`;
}

function reviewCard(r: Review): string {
    const v = r.myVote;
    const upOn = v === "up";
    const downOn = v === "down";
    const id = escAttr(r.id || "");
    return `
            <div style="background:${C.panel};border:1px solid ${C.line};border-radius:14px;padding:18px 20px">
              <div style="display:flex;align-items:center;gap:11px;margin-bottom:9px">
                ${ghAvatarSpan(r.login, r.author, r.initial || "?", r.bg || C.lilac, r.avatarUrl, 34, 13)}
                <div style="flex:1;min-width:0">
                  <div style="font-weight:700;font-size:14.5px;color:${C.text}">${esc(r.author)}${r.mine ? ` <span style="font-family:'JetBrains Mono',monospace;font-size:10px;color:${C.muted}">you</span>` : ""}</div>
                  <div style="font-size:13px;letter-spacing:1px;color:${C.lilac}">${starStr(r.rating)}</div>
                </div>
                <span style="font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.muted}">${esc(relative(r.createdAt))}</span>
              </div>
              <div style="margin:0 0 12px">${mdBlock(r.body)}</div>
              <div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap">
                <button data-act="vote-up" data-arg="${id}" style="display:inline-flex;align-items:center;gap:6px;font-family:'JetBrains Mono',monospace;font-size:12px;font-weight:700;color:${upOn ? C.bg : C.dim};background:${upOn ? C.green : "transparent"};border:1px solid ${upOn ? C.green : C.line};padding:5px 11px;border-radius:8px;cursor:pointer">▲ ${r.score ?? 0}</button>
                <button data-act="vote-down" data-arg="${id}" style="display:inline-flex;align-items:center;justify-content:center;font-family:'JetBrains Mono',monospace;font-size:12px;font-weight:700;color:${downOn ? C.bg : C.dim};background:${downOn ? C.pink : "transparent"};border:1px solid ${downOn ? C.pink : C.line};padding:5px 11px;border-radius:8px;cursor:pointer">▼</button>
                ${
                    r.mine
                        ? `<span style="flex:1"></span>
                <button data-act="review-edit" data-arg="${id}" style="background:none;border:none;color:${C.lilac};cursor:pointer;font-family:'JetBrains Mono',monospace;font-size:11.5px;padding:0">edit</button>
                <button data-act="review-delete" data-arg="${id}" style="background:none;border:none;color:${C.pink};cursor:pointer;font-family:'JetBrains Mono',monospace;font-size:11.5px;padding:0">delete</button>`
                        : `<span style="font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.muted}">was this helpful?</span>`
                }
              </div>
            </div>`;
}

// ── comments ─────────────────────────────────────────────────────────────────

function commentsTab(s: AppState): string {
    const commentEditor = s.commentPreview
        ? previewPane(s.commentDraft, 70)
        : `<textarea data-act="comment-draft" placeholder="Add to the discussion… Markdown supported." style="width:100%;min-height:70px;background:${C.panel};border:1px solid ${C.line};border-radius:10px;padding:12px 14px;color:${C.text};font-size:14px;font-family:'Outfit',sans-serif;resize:vertical;outline:none;box-sizing:border-box">${esc(s.commentDraft)}</textarea>`;
    const composer = s.user
        ? `
    <div style="background:${C.deep};border:1px solid ${C.line};border-radius:14px;padding:16px 18px;margin-bottom:22px">
      ${writePreviewTabs(s.commentPreview, "comment-write", "comment-preview")}
      ${commentEditor}
      <div style="display:flex;justify-content:flex-end;margin-top:10px">
        <button data-act="comment-submit" style="font-family:'JetBrains Mono',monospace;font-size:13px;font-weight:700;color:${C.bg};background:${C.lilac};border:none;padding:9px 18px;border-radius:9px;cursor:pointer">Comment</button>
      </div>
    </div>`
        : signInPrompt("join the discussion");

    const list = s.commentsLoading
        ? loading("Loading comments…")
        : commentThread(s) || empty("No comments yet — start the conversation.");

    return `<div>${composer}<div style="display:flex;flex-direction:column;gap:12px">${list}</div></div>`;
}

// A hidden (archived) comment is only visible to its author or a moderator; to
// everyone else it's filtered out of the thread entirely.
function commentVisible(c: Comment): boolean {
    return !c.archived || !!c.mine || !!c.canModerate;
}

function commentThread(s: AppState): string {
    const tops = s.comments.filter((c) => !c.parentId && commentVisible(c));
    return tops
        .map((c) => {
            const replies = s.comments.filter((r) => r.parentId === c.id && commentVisible(r));
            return commentCard(s, c, replies);
        })
        .join("");
}

function commentCard(s: AppState, c: Comment, replies: Comment[]): string {
    const replyEditor = s.replyPreview
        ? previewPane(s.replyDraft, 56)
        : `<textarea data-act="reply-draft" placeholder="Reply… Markdown supported." style="width:100%;min-height:56px;background:${C.deep};border:1px solid ${C.line};border-radius:10px;padding:10px 12px;color:${C.text};font-size:13.5px;font-family:'Outfit',sans-serif;resize:vertical;outline:none;box-sizing:border-box">${esc(s.replyDraft)}</textarea>`;
    const replyBox =
        s.user && s.replyTo === c.id
            ? `
        <div style="margin-top:12px">
          ${writePreviewTabs(s.replyPreview, "reply-write", "reply-preview")}
          ${replyEditor}
          <div style="display:flex;justify-content:flex-end;gap:8px;margin-top:8px">
            <button data-act="reply-cancel" style="font-family:'JetBrains Mono',monospace;font-size:12px;font-weight:600;color:${C.dim};background:transparent;border:1px solid ${C.line};padding:7px 14px;border-radius:8px;cursor:pointer">Cancel</button>
            <button data-act="reply-submit" data-arg="${escAttr(c.id)}" style="font-family:'JetBrains Mono',monospace;font-size:12px;font-weight:700;color:${C.bg};background:${C.lilac};border:none;padding:7px 15px;border-radius:8px;cursor:pointer">Reply</button>
          </div>
        </div>`
            : "";
    const replyRows = replies
        .map(
            (r) => `
        <div style="margin-top:12px;margin-left:22px;padding-left:16px;border-left:2px solid ${C.line}">${commentBody(s, r)}</div>`,
        )
        .join("");
    return `
      <div style="background:${C.panel};border:1px solid ${C.line};border-radius:14px;padding:16px 18px">
        ${commentBody(s, c)}
        ${replyRows}
        ${replyBox}
      </div>`;
}

function commentBody(s: AppState, c: Comment): string {
    const id = escAttr(c.id);
    const v = c.myVote;
    const upOn = v === "up";
    const downOn = v === "down";
    const votes = `
        <button data-act="comment-vote-up" data-arg="${id}" style="display:inline-flex;align-items:center;gap:5px;font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;color:${upOn ? C.bg : C.dim};background:${upOn ? C.green : "transparent"};border:1px solid ${upOn ? C.green : C.line};padding:4px 9px;border-radius:7px;cursor:pointer">▲ ${c.score ?? 0}</button>
        <button data-act="comment-vote-down" data-arg="${id}" style="display:inline-flex;align-items:center;justify-content:center;font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;color:${downOn ? C.bg : C.dim};background:${downOn ? C.pink : "transparent"};border:1px solid ${downOn ? C.pink : C.line};padding:4px 9px;border-radius:7px;cursor:pointer">▼</button>`;
    // "hide" soft-archives a comment; the author or a moderator (package/org
    // owner) may do it. Moderators who aren't the author see it as a moderation
    // action; restoring is "unhide".
    const canHide = c.mine || c.canModerate;
    const linkBtn = (act: string, label: string, color: string): string =>
        `<button data-act="${act}" data-arg="${id}" style="background:none;border:none;color:${color};cursor:pointer;font-family:inherit;font-size:inherit;padding:0">${label}</button>`;
    const hideBtn = c.archived
        ? canHide
            ? linkBtn("comment-unarchive", "unhide", C.lilac)
            : ""
        : canHide
          ? linkBtn("comment-archive", c.mine ? "hide" : "hide (moderator)", C.pink)
          : "";
    const actions = `
      <div style="display:flex;align-items:center;gap:10px;margin-top:8px;font-family:'JetBrains Mono',monospace;font-size:11.5px">
        ${votes}
        ${s.user && !c.parentId ? `<button data-act="reply-open" data-arg="${escAttr(c.id)}" style="background:none;border:none;color:${C.lilac};cursor:pointer;font-family:inherit;font-size:inherit;padding:0">reply</button>` : ""}
        ${c.mine ? `<button data-act="comment-edit-open" data-arg="${escAttr(c.id)}" style="background:none;border:none;color:${C.lilac};cursor:pointer;font-family:inherit;font-size:inherit;padding:0">edit</button>` : ""}
        ${c.mine ? `<button data-act="comment-delete" data-arg="${escAttr(c.id)}" style="background:none;border:none;color:${C.pink};cursor:pointer;font-family:inherit;font-size:inherit;padding:0">delete</button>` : ""}
        ${hideBtn}
      </div>`;
    const header = `
        <div style="display:flex;align-items:center;gap:10px;margin-bottom:8px">
          ${ghAvatarSpan(c.login, c.author, c.initial || "?", c.bg || C.lilac, c.avatarUrl, 30, 12)}
          <div style="flex:1;min-width:0">
            <div style="font-weight:700;font-size:13.5px;color:${C.text}">${esc(c.author)}${c.mine ? ` <span style="font-family:'JetBrains Mono',monospace;font-size:10px;color:${C.muted}">you</span>` : ""}</div>
          </div>
          <span style="font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.muted}">${esc(relative(c.createdAt))}</span>
        </div>`;
    // Inline edit mode for the caller's own comment.
    if (c.mine && s.commentEditing === c.id) {
        return `
        ${header}
        <textarea data-act="comment-edit-draft" placeholder="Edit your comment… Markdown supported." style="width:100%;min-height:64px;background:${C.deep};border:1px solid ${C.line};border-radius:10px;padding:10px 12px;color:${C.text};font-size:13.5px;font-family:'Outfit',sans-serif;resize:vertical;outline:none;box-sizing:border-box">${esc(s.commentEditDraft)}</textarea>
        <div style="display:flex;justify-content:flex-end;gap:8px;margin-top:8px">
          <button data-act="comment-edit-cancel" style="font-family:'JetBrains Mono',monospace;font-size:12px;font-weight:600;color:${C.dim};background:transparent;border:1px solid ${C.line};padding:7px 14px;border-radius:8px;cursor:pointer">Cancel</button>
          <button data-act="comment-edit-save" data-arg="${escAttr(c.id)}" style="font-family:'JetBrains Mono',monospace;font-size:12px;font-weight:700;color:${C.bg};background:${C.lilac};border:none;padding:7px 15px;border-radius:8px;cursor:pointer">Save</button>
        </div>`;
    }
    if (c.archived) {
        const note = `<div style="display:inline-flex;align-items:center;gap:6px;font-family:'JetBrains Mono',monospace;font-size:10.5px;font-weight:700;color:${C.muted};background:${C.deep};border:1px solid ${C.line2};padding:2px 9px;border-radius:100px;margin-bottom:8px">⊘ hidden${c.mine ? "" : " by a moderator"}</div>`;
        return `
        ${header}
        ${note}
        <div style="opacity:0.5">${mdBlock(c.body)}</div>
        ${actions}`;
    }
    return `
        ${header}
        ${mdBlock(c.body)}
        ${actions}`;
}

// ── issues ───────────────────────────────────────────────────────────────────

function issuesTab(s: AppState): string {
    const p = s.pkg!;
    // Live GitHub issues once synced (even if empty); else the seed sample.
    const live = s.ghIssues !== null;
    const issues = s.ghIssues ?? (p.issues || []);
    const openCount = issues.filter((i) => i.state === "open").length;
    const closedCount = issues.length - openCount;
    const view = issues.filter((i) =>
        s.issueFilter === "open" ? i.state === "open" : i.state === "closed",
    );
    const filters = [
        { k: "open", l: `Open · ${openCount}` },
        { k: "closed", l: `Closed · ${closedCount}` },
    ]
        .map((f) => {
            const on = s.issueFilter === f.k;
            return `<button data-act="issue-filter" data-arg="${f.k}" style="font-family:'JetBrains Mono',monospace;font-size:12px;font-weight:700;color:${on ? C.bg : C.dim};background:${on ? C.lilac : "transparent"};border:none;padding:6px 13px;border-radius:6px;cursor:pointer">${esc(f.l)}</button>`;
        })
        .join("");
    const syncLabel = s.ghIssuesLoading ? "⟳ Syncing…" : live ? "⟳ Re-sync" : "⟳ Sync from GitHub";
    const syncBtn = `<button data-act="sync-issues"${s.ghIssuesLoading ? " disabled" : ""} style="font-family:'JetBrains Mono',monospace;font-size:12px;font-weight:700;color:${C.text};background:transparent;border:1px solid ${C.line2};padding:6px 13px;border-radius:8px;cursor:${s.ghIssuesLoading ? "default" : "pointer"};opacity:${s.ghIssuesLoading ? "0.6" : "1"}">${syncLabel}</button>`;
    // A subtle note when a sync failed / rate-limited and we're on seed data.
    const note = s.ghIssuesError
        ? `<div style="font-size:12px;color:${C.muted};margin-bottom:12px">Showing sample data — couldn't reach GitHub (it may be rate-limited). Try again later.</div>`
        : live
          ? `<div style="font-size:12px;color:${C.muted};margin-bottom:12px">Live issues from GitHub.</div>`
          : "";
    const rows =
        view.map((it, i) => issueRow(p, it, i === 0)).join("") ||
        `<div style="padding:20px;color:${C.muted};font-size:14px;background:${C.panel}">No ${s.issueFilter} issues.</div>`;
    return `
<div>
  <div style="display:flex;align-items:center;gap:14px;margin-bottom:18px;flex-wrap:wrap">
    <div style="display:flex;gap:2px;background:${C.deep};border:1px solid ${C.line};border-radius:9px;padding:3px">${filters}</div>
    <div style="margin-left:auto;display:flex;align-items:center;gap:14px;flex-wrap:wrap">
      ${syncBtn}
      <a href="${escAttr(p.repository)}/issues" target="_blank" rel="noopener" style="text-decoration:none;font-family:'JetBrains Mono',monospace;font-size:12px;font-weight:700;color:${C.lilac}">new issue on GitHub ↗</a>
    </div>
  </div>
  ${note}
  <div style="border:1px solid ${C.line};border-radius:14px;overflow:hidden">${rows}</div>
</div>`;
}

const LABEL_STYLE: Record<string, { color: string; bg: string; border: string }> = {
    tinygo: { color: "#ffb4d2", bg: "#3a1f34", border: "#6b3453" },
    bug: { color: "#ffb4d2", bg: "#3a1f34", border: "#6b3453" },
    enhancement: { color: "#c3a8ff", bg: "#221c52", border: "#443a8c" },
    docs: { color: "#74e0ad", bg: "#16322c", border: "#2e5a48" },
    "good first issue": { color: "#74e0ad", bg: "#16322c", border: "#2e5a48" },
};

function issueRow(p: Package, it: Issue, first: boolean): string {
    const labels = it.labels
        .map((l) => {
            const st = LABEL_STYLE[l] || { color: C.lilac, bg: C.panel, border: C.line2 };
            return `<span style="font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:700;color:${st.color};background:${st.bg};border:1px solid ${st.border};padding:2px 8px;border-radius:100px">${esc(l)}</span>`;
        })
        .join("");
    const open = it.state === "open";
    return `
            <a href="${escAttr(p.repository)}/issues/${it.num}" target="_blank" rel="noopener" style="text-decoration:none;display:grid;grid-template-columns:auto 1fr auto;gap:13px;align-items:center;padding:15px 18px;border-top:${first ? "none" : `1px solid ${C.line}`};background:${C.panel}">
              <span style="font-size:15px;color:${open ? C.green : "#8d7fc7"};margin-top:1px">${open ? "◉" : "✓"}</span>
              <div style="min-width:0">
                <div style="display:flex;align-items:center;gap:9px;flex-wrap:wrap;margin-bottom:4px">
                  <span style="font-size:14.5px;font-weight:600;color:${C.text}">${esc(it.title)}</span>
                  ${labels}
                </div>
                <div style="font-family:'JetBrains Mono',monospace;font-size:11.5px;color:${C.muted}">#${it.num} · opened ${esc(it.age)} by ${esc(it.author)}</div>
              </div>
              <span style="font-family:'JetBrains Mono',monospace;font-size:12px;color:${C.muted};white-space:nowrap">${it.comments} · comments</span>
            </a>`;
}

// ── versions ─────────────────────────────────────────────────────────────────

function versionsTab(s: AppState): string {
    const p = s.pkg!;
    const versions = p.versions || [];
    const rows =
        versions.map((v, i) => versionRow(p, v, i === 0)).join("") ||
        `<div style="padding:20px;color:${C.muted};font-size:14px;background:${C.panel}">No version history.</div>`;
    return `<div><div style="border:1px solid ${C.line};border-radius:14px;overflow:hidden">${rows}</div></div>`;
}

function versionRow(p: Package, v: VersionRow, first: boolean): string {
    const latest = v.latest
        ? `<span style="font-family:'JetBrains Mono',monospace;font-size:9.5px;font-weight:700;color:${C.bg};background:${C.green};padding:2px 7px;border-radius:100px">latest</span>`
        : "";
    const deprecated = v.deprecated
        ? `<span style="font-family:'JetBrains Mono',monospace;font-size:9.5px;font-weight:700;color:#ffb4d2;background:#3a1f34;border:1px solid #6b3453;padding:2px 7px;border-radius:100px">deprecated</span>`
        : "";
    const hidden = v.hidden
        ? `<span title="Placeholder release — freely re-published and replaced by the first real version" style="font-family:'JetBrains Mono',monospace;font-size:9.5px;font-weight:700;color:${C.muted};background:${C.deep};border:1px solid ${C.line2};padding:2px 7px;border-radius:100px">hidden</span>`
        : "";
    const commit = v.commit
        ? `<a href="${escAttr(p.repository)}/commit/${escAttr(v.commit)}" target="_blank" rel="noopener" title="${escAttr(v.commit)}" style="text-decoration:none;font-family:'JetBrains Mono',monospace;font-size:10.5px;color:${C.lilac};background:${C.deep};border:1px solid ${C.line};padding:2px 7px;border-radius:6px">⑂ ${esc(shortHash(v.commit))}</a>`
        : "";
    return `
            <div style="display:grid;grid-template-columns:150px 1fr;gap:16px;padding:18px 20px;border-top:${first ? "none" : `1px solid ${C.line}`};background:${C.panel}">
              <div>
                <div style="display:flex;align-items:center;gap:8px;margin-bottom:5px;flex-wrap:wrap">
                  <span style="font-family:'JetBrains Mono',monospace;font-weight:700;font-size:15px;color:${C.text}">${esc(v.version)}</span>
                  ${latest}${deprecated}${hidden}
                </div>
                <div style="font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.muted};margin-bottom:7px">${esc(relative(v.publishedAt))}</div>
                ${commit}
              </div>
              <div style="min-width:0">
                <p style="font-size:13.5px;line-height:1.55;color:${C.soft};margin:0 0 10px">${esc(v.notes)}</p>
                <div style="display:flex;align-items:center;gap:10px">
                  <span style="flex:1;max-width:180px;height:5px;background:${C.deep};border-radius:3px;overflow:hidden"><span style="display:block;height:100%;width:${v.installShare}%;background:linear-gradient(90deg,${C.violet},${C.lilac})"></span></span>
                  <span style="font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.muted}">${v.installShare}% of installs</span>
                </div>
              </div>
            </div>`;
}

// ── sidebar ──────────────────────────────────────────────────────────────────

function pkgSidebar(s: AppState): string {
    const p = s.pkg!;
    // The install trend: ~a year of history bucketed into weekly totals.
    const weeks = weeklyBuckets(s.installSeries);
    const counts = weeks.map((b) => b.count);
    const hasInstalls = counts.some((c) => c > 0);
    const max = Math.max(1, ...counts);
    const spark = hasInstalls ? sparkline(counts.map((c) => (c / max) * 100)) : null;
    // Per-point hover data (viewBox coords + the week's total/start-date) for the tooltip.
    const sparkPts = weeks.map((b, i) => ({
        x: weeks.length > 1 ? +((i / (weeks.length - 1)) * 100).toFixed(2) : 100,
        y: +(38 - (b.count / max) * 34).toFixed(2),
        c: b.count,
        d: b.date,
    }));
    const installCmd = `wago pkg add ${p.name}`;

    // A metadata cell (label + value); rendered full-width or half-width.
    const metaCell = (label: string, value: string): string =>
        `<div><div style="font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;letter-spacing:1px;color:${C.muted};text-transform:uppercase;margin-bottom:6px">${esc(label)}</div><div style="font-size:14.5px;color:${C.text};font-weight:600;word-break:break-all">${esc(value)}</div></div>`;
    const metaRow = (inner: string, top: boolean): string =>
        `<div style="padding:15px 0;border-top:${top ? `1px solid ${C.line}` : "none"}">${inner}</div>`;
    const splitRow = (a: string, b: string, top: boolean): string =>
        `<div style="display:grid;grid-template-columns:1fr 1fr;gap:16px;padding:15px 0;border-top:${top ? `1px solid ${C.line}` : "none"}">${a}${b}</div>`;

    const openIssues = (p.issues || []).filter((i) => i.state === "open").length;
    const metaRows =
        metaRow(metaCell("Last publish", relative(p.updatedAt)), false) +
        metaRow(metaCell("Unpacked size", p.unpackedKB ? `${p.unpackedKB} kB` : "—"), true) +
        // Forks + Issues share a single split row.
        splitRow(metaCell("Forks", (p.forks ?? 0).toLocaleString()), metaCell("Issues", openIssues.toLocaleString()), true) +
        metaRow(metaCell("Owner", p.ownerLogin ? `@${p.ownerLogin}` : "—"), true);

    // compatibility (engines + platforms)
    const engines = p.compatibility?.engines || {};
    const order = ["wago", "tinygo", "go"];
    const engKeys = Object.keys(engines).sort((a, b) => order.indexOf(a) - order.indexOf(b));
    const engineRows = engKeys
        .map(
            (k) =>
                `<div style="display:flex;align-items:center;justify-content:space-between;gap:10px;padding:3px 0"><span style="font-family:'JetBrains Mono',monospace;font-size:12.5px;color:${C.dim}">${esc(k)}</span><span style="font-family:'JetBrains Mono',monospace;font-size:12px;font-weight:600;color:${engines[k] === "*" ? C.green : C.text}">${esc(engines[k] === "*" ? "any" : engines[k])}</span></div>`,
        )
        .join("");
    const platforms = p.compatibility?.platforms || [];
    const compatSection = `
      <div style="padding:15px 0;border-top:1px solid ${C.line}">
        <div style="font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;letter-spacing:1px;color:${C.muted};text-transform:uppercase;margin-bottom:8px">Compatibility</div>
        ${engineRows || `<span style="font-size:13px;color:${C.muted}">any engine</span>`}
        <div style="margin-top:8px;font-family:'JetBrains Mono',monospace;font-size:11.5px;color:${C.muted}">${platforms.length ? platforms.map((pf) => esc(pf)).join(", ") : "platform-independent"}</div>
      </div>`;

    // capabilities
    const capChips = (p.capabilities || [])
        .map(
            (c) =>
                `<span style="font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.pink};background:#2a1230;border:1px solid #6b3453;padding:3px 9px;border-radius:6px">⚑ ${esc(c)}</span>`,
        )
        .join("");
    const capSection = capChips
        ? `<div style="padding:15px 0;border-top:1px solid ${C.line}"><div style="font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;letter-spacing:1px;color:${C.muted};text-transform:uppercase;margin-bottom:10px">Capabilities</div><div style="display:flex;gap:6px;flex-wrap:wrap">${capChips}</div></div>`
        : "";

    // keywords — moved out of the header, npm-style at the bottom
    const kwChips = (p.keywords || p.tags || [])
        .map(
            (k) =>
                `<span data-act="kw" data-arg="${escAttr(k)}" style="font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.lilac};background:${C.deep};border:1px solid ${C.line};padding:3px 9px;border-radius:100px;cursor:pointer">${esc(k)}</span>`,
        )
        .join("");
    const kwSection = kwChips
        ? `<div style="padding:15px 0;border-top:1px solid ${C.line}"><div style="font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;letter-spacing:1px;color:${C.muted};text-transform:uppercase;margin-bottom:10px">Keywords</div><div style="display:flex;gap:6px;flex-wrap:wrap">${kwChips}</div></div>`
        : "";

    const authors = (p.authors || [])
        .map((a) => {
            const av = ghAvatarSpan(
                a.github,
                a.name,
                (a.name || "?")[0].toUpperCase(),
                avatarBgFor(a.github || a.name),
                undefined,
                34,
                13,
            );
            // Clickable → wago profile when we have a GitHub login for them.
            return a.github ? profileLinkWrap(a.github, av) : av;
        })
        .join("");
    const contributors = (p.contributors || [])
        .map((login) =>
            profileLinkWrap(
                login,
                ghAvatarSpan(login, login, login[0].toUpperCase(), avatarBgFor(login), undefined, 30, 11),
            ),
        )
        .join("");
    const monthLabel = compactNum(s.installSeries.slice(-30).reduce((a, b) => a + b.count, 0) || p.installsWeek);

    return `
    <aside style="display:flex;flex-direction:column;gap:0;background:${C.panel};border:1px solid ${C.line};border-radius:16px;padding:4px 22px 22px">
      <div style="padding:16px 0 4px">
        <div style="font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;letter-spacing:1px;color:${C.muted};text-transform:uppercase;margin-bottom:9px">Install</div>
        <div style="background:${C.deep};border:1px solid ${C.line};border-radius:10px;display:flex;align-items:center;gap:8px;padding:10px 12px">
          <span style="flex:1;min-width:0;font-family:'JetBrains Mono',monospace;font-size:12px;color:#e7e0ff;overflow-x:auto;white-space:nowrap"><span style="color:#6f64a8">$</span> ${esc(installCmd)}</span>
          <button data-copy="${escAttr(installCmd)}" data-act="copy-install" style="flex-shrink:0;font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.dim};background:transparent;border:1px solid ${C.line};padding:4px 10px;border-radius:6px;cursor:pointer"><span data-copy-label>copy</span></button>
        </div>
      </div>
      <div style="padding:15px 0;border-top:1px solid ${C.line}">
        <div style="display:flex;align-items:baseline;justify-content:space-between;margin-bottom:9px">
          <div style="font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;letter-spacing:1px;color:${C.muted};text-transform:uppercase">Monthly installs</div>
          <div style="font-family:'JetBrains Mono',monospace;font-size:12px;font-weight:700;color:${C.lilac}">${esc(monthLabel)}</div>
        </div>
        <div class="spark-wrap" data-spark="${escAttr(JSON.stringify(sparkPts))}" style="position:relative;height:44px;display:flex;align-items:center">
          ${
              spark
                  ? `<svg viewBox="0 0 100 40" preserveAspectRatio="none" style="width:100%;height:100%;overflow:visible">
            <defs><linearGradient id="sparkfill" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${C.lilac}" stop-opacity="0.35"></stop><stop offset="1" stop-color="${C.lilac}" stop-opacity="0"></stop></linearGradient></defs>
            <path d="${spark.area}" fill="url(#sparkfill)"></path>
            <polyline points="${spark.points}" fill="none" stroke="${C.lilac}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" vector-effect="non-scaling-stroke"></polyline>
          </svg>`
                  : `<span style="font-size:12px;color:${C.muted}">No install history yet.</span>`
          }
        </div>
      </div>
      ${metaRows}
      ${compatSection}
      ${capSection}
      <div style="padding:15px 0;border-top:1px solid ${C.line}">
        <div style="font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;letter-spacing:1px;color:${C.muted};text-transform:uppercase;margin-bottom:10px">Authors</div>
        <div style="display:flex;gap:8px;flex-wrap:wrap">${authors || `<span style="font-size:13px;color:${C.muted}">—</span>`}</div>
      </div>
      ${
          contributors
              ? `<div style="padding:15px 0;border-top:1px solid ${C.line}">
        <div style="font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;letter-spacing:1px;color:${C.muted};text-transform:uppercase;margin-bottom:10px">Contributors</div>
        <div style="display:flex;gap:8px;flex-wrap:wrap">${contributors}</div>
      </div>`
              : ""
      }
      ${kwSection}
      ${
          p.repository
              ? `<a href="${escAttr(p.repository)}" target="_blank" rel="noopener" style="text-decoration:none;text-align:center;margin-top:14px;margin-bottom:6px;font-family:'JetBrains Mono',monospace;font-size:13px;font-weight:700;color:${C.bg};background:${C.lilac};padding:11px;border-radius:10px">Repository ↗</a>`
              : ""
      }
      ${reportLink(s)}
    </aside>`;
}

// canManagePackage reports whether the viewer may open the package's Settings tab:
// its owner login, or a site admin.
function canManagePackage(s: AppState): boolean {
    const me = s.user?.login?.toLowerCase();
    if (!me) return false;
    return me === (s.pkg?.ownerLogin || "").toLowerCase() || !!s.user?.admin;
}

// reportLink is the subtle "Report package" affordance in the sidebar — available
// to any signed-in user (owner/admin controls live in the Settings tab instead).
function reportLink(s: AppState): string {
    if (!s.user) return "";
    return `<div style="margin-top:16px;padding-top:14px;border-top:1px solid ${C.line}">
      <button data-act="report-open" style="display:flex;align-items:center;gap:7px;width:100%;justify-content:center;font-family:'Outfit',sans-serif;font-weight:600;font-size:12.5px;color:${C.muted};background:transparent;border:1px solid ${C.line};padding:8px;border-radius:9px;cursor:pointer">⚑ Report package</button>
    </div>`;
}

// settingsTab consolidates every owner/admin control for a package: who may
// publish, deprecation, and removal. Gated by canManagePackage (the tab only
// renders for owner/admin, but re-check here defensively).
function settingsTab(s: AppState): string {
    if (!canManagePackage(s)) {
        return `<div style="color:${C.muted};font-size:14px;padding:20px 0">You don't manage this package.</div>`;
    }
    const p = s.pkg!;
    const isOwner = (s.user?.login || "").toLowerCase() === (p.ownerLogin || "").toLowerCase();
    const panel = (title: string, desc: string, body: string): string =>
        `<section style="background:${C.panel};border:1px solid ${C.line};border-radius:14px;padding:20px 22px">
        <h3 style="font-family:'Outfit',sans-serif;font-weight:800;font-size:15px;margin:0 0 4px;color:${C.text}">${title}</h3>
        <p style="font-size:12.5px;line-height:1.5;color:${C.muted};margin:0 0 14px">${desc}</p>${body}</section>`;
    return `<div style="display:flex;flex-direction:column;gap:18px;max-width:660px">
      ${panel("Publishers", "Extra GitHub logins allowed to publish new versions, beyond the repo's authors/admins. Publishing is author-only by default.", publishersBody(s))}
      ${panel("Deprecation", "Flag this package as deprecated — a banner warns users on the package page.", deprecateBody(s))}
      ${dangerPanel(isOwner)}
    </div>`;
}

// publishersBody is the allowed-publishers editor: chips with remove buttons + an
// add input.
function publishersBody(s: AppState): string {
    const p = s.pkg!;
    const list = p.allowedPublishers || [];
    const chips = list.length
        ? list
              .map(
                  (login) =>
                      `<span style="display:inline-flex;align-items:center;gap:5px;font-size:12.5px;color:${C.text};background:${C.deep};border:1px solid ${C.line2};border-radius:8px;padding:3px 6px 3px 9px">@${esc(login)}<button data-act="publisher-remove" data-arg="${escAttr(login)}" title="Remove" style="font-family:'Outfit',sans-serif;font-size:14px;line-height:1;color:${C.muted};background:transparent;border:none;padding:0 2px;cursor:pointer">×</button></span>`,
              )
              .join("")
        : `<span style="font-size:12.5px;color:${C.muted}">Author-only — only @${esc(p.ownerLogin || "")} and the repo's admins can publish.</span>`;
    return `<div style="display:flex;flex-wrap:wrap;gap:6px;margin-bottom:12px">${chips}</div>
      <div data-pub-search style="position:relative;max-width:360px">
        <div style="display:flex;gap:6px">
          <input data-act="publisher-draft" value="${escAttr(s.publisherDraft)}" placeholder="search GitHub username" spellcheck="false" autocomplete="off" style="flex:1;min-width:0;font-family:'JetBrains Mono',monospace;font-size:12.5px;color:${C.text};background:${C.deep};border:1px solid ${C.line2};border-radius:9px;padding:8px 10px" />
          <button data-act="publisher-add" style="flex-shrink:0;font-family:'Outfit',sans-serif;font-weight:700;font-size:12.5px;color:${C.bg};background:${C.lilac};border:none;padding:8px 14px;border-radius:9px;cursor:pointer">Add</button>
        </div>
        <div class="pub-dropdown" style="position:absolute;top:calc(100% + 4px);left:0;right:0;z-index:30">${publisherDropdownHtml(s)}</div>
      </div>`;
}

// publisherDropdownHtml builds the GitHub-search autocomplete under the publisher
// input. Exported so app.ts can patch just this slot as results arrive, without a
// full re-render (which would steal the input's focus). Already-added logins and
// the owner are filtered out.
export function publisherDropdownHtml(s: AppState): string {
    const p = s.pkg;
    if (!p) return "";
    const already = new Set((p.allowedPublishers || []).map((x) => x.toLowerCase()).concat((p.ownerLogin || "").toLowerCase()));
    const results = (s.publisherResults || []).filter((u) => !already.has(u.login.toLowerCase()));
    if (!results.length) return "";
    return `<div style="background:${C.panel};border:1px solid ${C.line2};border-radius:10px;box-shadow:0 20px 40px -16px rgba(0,0,0,.7);overflow:hidden">
      ${results
          .map(
              (u, i) =>
                  `<button data-act="publisher-pick" data-arg="${escAttr(u.login)}" style="display:flex;align-items:center;gap:9px;width:100%;text-align:left;background:transparent;border:none;${i ? `border-top:1px solid ${C.line};` : ""}padding:7px 11px;cursor:pointer;color:${C.text}"><img src="${escAttr(u.avatarUrl)}" alt="" style="width:22px;height:22px;border-radius:50%;flex-shrink:0" /><span style="font-family:'JetBrains Mono',monospace;font-size:12.5px">@${esc(u.login)}</span></button>`,
          )
          .join("")}
    </div>`;
}

// deprecateBody toggles the package's deprecation notice.
function deprecateBody(s: AppState): string {
    const p = s.pkg!;
    if (p.deprecatedMessage) {
        return `<div style="font-size:13px;line-height:1.5;color:${C.soft};margin-bottom:12px">Deprecated: “${esc(p.deprecatedMessage)}”</div>
      <button data-act="undeprecate" style="font-family:'Outfit',sans-serif;font-weight:700;font-size:12.5px;color:${C.text};background:transparent;border:1px solid ${C.line2};padding:8px 14px;border-radius:9px;cursor:pointer">Remove deprecation</button>`;
    }
    return `<div style="display:flex;gap:6px;max-width:460px">
        <input data-act="deprecate-draft" value="${escAttr(s.deprecateDraft)}" placeholder="Reason (optional)" style="flex:1;min-width:0;font-family:'Outfit',sans-serif;font-size:13px;color:${C.text};background:${C.deep};border:1px solid ${C.line2};border-radius:9px;padding:8px 10px" />
        <button data-act="deprecate" style="flex-shrink:0;font-family:'Outfit',sans-serif;font-weight:700;font-size:12.5px;color:${C.text};background:transparent;border:1px solid ${C.line2};padding:8px 14px;border-radius:9px;cursor:pointer">Mark deprecated</button>
      </div>`;
}

// dangerPanel holds the destructive action: unpublish (owner) / take down (admin).
function dangerPanel(isOwner: boolean): string {
    const label = isOwner ? "Unpublish package" : "Take down package";
    const desc = isOwner
        ? "Remove this package and all its versions from the registry. This can't be undone."
        : "Remove this package from the registry as a site admin. This can't be undone.";
    return `<section style="background:#2a1522;border:1px solid #6b3453;border-radius:14px;padding:20px 22px">
        <h3 style="font-family:'Outfit',sans-serif;font-weight:800;font-size:15px;margin:0 0 4px;color:#ffb4d2">Danger zone</h3>
        <p style="font-size:12.5px;line-height:1.5;color:#e0a9c4;margin:0 0 14px">${desc}</p>
        <button data-act="takedown" style="font-family:'Outfit',sans-serif;font-weight:700;font-size:13px;color:#fff;background:#8a2f52;border:1px solid #b0426b;padding:9px 16px;border-radius:9px;cursor:pointer">${label}</button>
      </section>`;
}

// reportModal is the flag-for-moderation panel: pick a reason, add optional
// detail, submit. Mirrors starConsentModal's overlay styling.
function reportModal(s: AppState): string {
    const shell = (inner: string) =>
        `<div data-act="report-cancel" style="position:fixed;inset:0;z-index:120;background:rgba(11,8,32,0.66);backdrop-filter:blur(3px);display:flex;align-items:center;justify-content:center;padding:20px">
      <div data-act="noop" style="width:100%;max-width:440px;background:${C.panel};border:1px solid ${C.line2};border-radius:18px;padding:24px;box-shadow:0 30px 60px -20px rgba(0,0,0,.75)">${inner}</div>
    </div>`;
    if (s.reportDone) {
        return shell(
            `<div style="display:flex;align-items:center;gap:10px;margin-bottom:10px"><span style="font-size:20px;color:${C.green}">✓</span><h2 style="font-weight:800;font-size:18px;margin:0">Report sent</h2></div>
       <p style="font-size:14px;line-height:1.6;color:${C.soft};margin:0 0 18px">Thanks — the wago moderators will take a look.</p>
       <button data-act="report-cancel" style="width:100%;font-family:'Outfit',sans-serif;font-weight:700;font-size:14px;color:${C.text};background:transparent;border:1px solid ${C.line2};padding:12px;border-radius:11px;cursor:pointer">Close</button>`,
        );
    }
    const reasons: [string, string][] = [
        ["malware", "Malware or security risk"],
        ["impersonation", "Impersonation / not the author's repo"],
        ["spam", "Spam or low quality"],
        ["broken", "Broken or misleading"],
        ["other", "Something else"],
    ];
    const opts = reasons
        .map(([v, label]) => {
            const on = s.reportReason === v;
            return `<label data-act="report-reason" data-arg="${v}" style="display:flex;align-items:center;gap:9px;font-size:13.5px;color:${on ? C.text : C.soft};cursor:pointer;padding:8px 10px;border-radius:9px;border:1px solid ${on ? C.lilac : C.line};background:${on ? C.deep : "transparent"}">
        <span style="width:15px;height:15px;border-radius:50%;border:1px solid ${on ? C.lilac : C.line2};display:inline-flex;align-items:center;justify-content:center;flex-shrink:0">${on ? `<span style="width:8px;height:8px;border-radius:50%;background:${C.lilac}"></span>` : ""}</span>${label}</label>`;
        })
        .join("");
    const canSend = !!s.reportReason && !s.reportSending;
    return shell(
        `<div style="display:flex;align-items:center;gap:10px;margin-bottom:6px"><span style="font-size:18px;color:${C.pink}">⚑</span><h2 style="font-weight:800;font-size:18px;margin:0">Report ${esc(s.pkg?.short || "package")}</h2></div>
     <p style="font-size:13px;line-height:1.55;color:${C.muted};margin:0 0 16px">This goes to the wago moderators. Not for bugs — use the repo's issues for those.</p>
     <div style="display:flex;flex-direction:column;gap:7px;margin-bottom:14px">${opts}</div>
     <textarea data-act="report-detail" placeholder="Add details (optional)" style="width:100%;box-sizing:border-box;min-height:70px;resize:vertical;font-family:'Outfit',sans-serif;font-size:13.5px;color:${C.text};background:${C.deep};border:1px solid ${C.line2};border-radius:11px;padding:11px;margin-bottom:16px">${esc(s.reportDetail)}</textarea>
     <div style="display:flex;flex-direction:column;gap:9px">
       <button data-act="report-submit" ${canSend ? "" : "disabled"} style="width:100%;font-family:'Outfit',sans-serif;font-weight:700;font-size:14px;color:${C.bg};background:${canSend ? C.lilac : C.line2};border:none;padding:12px;border-radius:11px;cursor:${canSend ? "pointer" : "not-allowed"}">${s.reportSending ? "Sending…" : "Send report"}</button>
       <button data-act="report-cancel" style="width:100%;font-family:'JetBrains Mono',monospace;font-weight:600;font-size:12.5px;color:${C.muted};background:transparent;border:none;padding:6px;border-radius:8px;cursor:pointer">Cancel</button>
     </div>`,
    );
}

// A small pill linking to another account's wago profile (org membership or
// org member), showing its avatar + login.
function accountChip(login: string, avatarUrl?: string): string {
    const initial = (login || "?").trim()[0]?.toUpperCase() || "?";
    const av = avatarSpan(login, initial, avatarBgFor(login), avatarUrl, 24, 10);
    return profileLinkWrap(
        login,
        `<span style="display:inline-flex;align-items:center;gap:9px;background:${C.panel};border:1px solid ${C.line};border-radius:100px;padding:5px 15px 5px 6px">${av}<span style="font-family:'JetBrains Mono',monospace;font-size:12.5px;font-weight:700;color:${C.text}">${esc(login)}</span></span>`,
    );
}

// Wrap an avatar (or any inline content) in a link to a user's wago profile.
function profileLinkWrap(login: string, inner: string): string {
    return `<a href="/${escAttr(login)}" data-act="user" data-arg="${escAttr(login)}" title="@${escAttr(login)}" style="text-decoration:none;cursor:pointer">${inner}</a>`;
}

function avatarBgFor(seed: string): string {
    const palette = ["#c3a8ff", "#74e0ad", "#ff9ec4", "#8d7fc7", "#7bd0ff"];
    let h = 0;
    for (let i = 0; i < seed.length; i++) h = (h * 31 + seed.charCodeAt(i)) | 0;
    return palette[Math.abs(h) % palette.length];
}

// ── auth ─────────────────────────────────────────────────────────────────────

export function authScreen(s: AppState): string {
    const err = s.authError
        ? `<div style="background:#3a1f34;border:1px solid #6b3453;border-radius:11px;padding:12px 14px;margin-bottom:16px;font-size:13px;color:#ffb4d2">${esc(s.authError)}</div>`
        : "";
    return `
<div style="display:flex;justify-content:center;padding:72px 0 90px">
  <div style="width:100%;max-width:410px">
    <div style="text-align:center;margin-bottom:26px">
      <img src="/assets/wago-logo.png" alt="wago" style="width:52px;height:52px;border-radius:13px;margin-bottom:16px" />
      <h1 style="font-weight:800;font-size:26px;letter-spacing:-0.8px;margin:0 0 6px">Sign in to wago</h1>
      <p style="font-size:14.5px;line-height:1.55;color:${C.muted};margin:0">Use your GitHub account to star, review and publish packages — no separate password to manage.</p>
    </div>
    ${err}
    <div style="background:${C.panel};border:1px solid ${C.line};border-radius:18px;padding:30px 26px">
      <button data-act="signin" style="width:100%;display:flex;align-items:center;justify-content:center;gap:11px;background:${C.deep};border:1px solid ${C.line2};border-radius:11px;padding:15px;font-family:'Outfit',sans-serif;font-weight:700;font-size:15px;color:${C.text};cursor:pointer">
        ${githubIcon(19)}
        Continue with GitHub
      </button>
      <div style="display:flex;gap:10px;align-items:flex-start;margin-top:18px">
        <span style="color:${C.green};font-size:14px;margin-top:1px;flex-shrink:0">✦</span>
        <p style="font-size:12.5px;line-height:1.55;color:${C.muted};margin:0">We only read your public profile and email. wago never sees your password or private repositories.</p>
      </div>
    </div>
    <p style="font-size:11.5px;line-height:1.55;color:${C.faint};text-align:center;margin:18px 0 0">By continuing you agree to the Terms of Service and Privacy Policy.</p>
    <div style="text-align:center;margin-top:14px">
      <a href="/" data-act="home" style="text-decoration:none;font-family:'JetBrains Mono',monospace;font-size:12.5px;color:${C.muted}">← back to browse</a>
    </div>
  </div>
</div>`;
}

function githubIcon(size: number): string {
    return `<svg width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="${C.text}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="6" cy="6" r="2.5"></circle><circle cx="6" cy="18" r="2.5"></circle><circle cx="18" cy="9" r="2.5"></circle><path d="M18 11.5v.5a3 3 0 0 1-3 3H8.5"></path><path d="M6 8.5v7"></path></svg>`;
}

function bookmarkIcon(size: number, color: string, filled: boolean): string {
    return `<svg width="${size}" height="${size}" viewBox="0 0 24 24" fill="${filled ? color : "none"}" stroke="${color}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z"></path></svg>`;
}

// ── account ──────────────────────────────────────────────────────────────────

export function accountScreen(s: AppState): string {
    const u = s.user!;
    const nav = [
        { k: "profile", l: "Profile", icon: "◉" },
        { k: "plugins", l: "Your packages", icon: "▤" },
        { k: "stars", l: "Your stars", icon: "★" },
        { k: "settings", l: "Settings", icon: "⚙" },
    ]
        .map((n) => {
            const on = s.acctTab === n.k;
            return `<a href="#/account" data-act="acct-tab" data-arg="${n.k}" style="display:flex;align-items:center;gap:11px;text-decoration:none;padding:11px 14px;border-radius:10px;font-size:14px;font-weight:600;color:${on ? C.text : C.soft};background:${on ? C.panel : "transparent"}"><span style="width:18px;text-align:center;color:${on ? C.lilac : C.muted}">${n.icon}</span> ${n.l}</a>`;
        })
        .join("");
    let body = "";
    if (s.acctTab === "plugins") body = acctPlugins(s);
    else if (s.acctTab === "stars") body = acctStars(s);
    else if (s.acctTab === "settings") body = acctSettings(s);
    else body = acctProfile(s);

    return `
<div style="padding:32px 0 72px">
  <div style="display:grid;grid-template-columns:230px 1fr;gap:32px;align-items:start">
    <aside style="position:sticky;top:78px;display:flex;flex-direction:column;gap:14px">
      <div style="display:flex;align-items:center;gap:12px;background:${C.panel};border:1px solid ${C.line};border-radius:14px;padding:15px">
        ${avatarSpan(u.name, u.initial, u.bg, u.avatarUrl, 44, 17)}
        <div style="min-width:0">
          <div style="font-size:15px;font-weight:700;color:${C.text};white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${esc(u.name)}</div>
          <div style="font-family:'JetBrains Mono',monospace;font-size:11.5px;color:${C.muted}">@${esc(u.login)}</div>
        </div>
      </div>
      <div style="display:flex;flex-direction:column;gap:3px">${nav}</div>
    </aside>
    <div style="min-width:0">${body}</div>
  </div>
</div>`;
}

function ownedPlugins(s: AppState): (Package & { role: string })[] {
    const reg = s.registry;
    const u = s.user;
    if (!reg || !u) return [];
    return reg.packages
        .filter((p) => p.ownerLogin === u.login || (p.contributors || []).includes(u.login))
        .map((p) => Object.assign({}, p, { role: p.ownerLogin === u.login ? "owner" : "maintainer" }));
}

const ROLE_STYLE: Record<string, { color: string; bg: string; border: string }> = {
    owner: { color: "#1a1547", bg: "#74e0ad", border: "#74e0ad" },
    author: { color: "#1a1547", bg: "#c3a8ff", border: "#c3a8ff" },
    maintainer: { color: "#c3a8ff", bg: "#221c52", border: "#443a8c" },
};

function acctProfile(s: AppState): string {
    const u = s.user!;
    const owned = ownedPlugins(s);
    const rows =
        owned
            .map((p, i) => {
                const rs = ROLE_STYLE[p.role];
                return `
            <a href="${pkgPath(p)}" data-act="open" data-arg="${escAttr(p.short)}" style="text-decoration:none;display:grid;grid-template-columns:1fr auto;gap:14px;align-items:center;padding:15px 18px;border-top:${i === 0 ? "none" : `1px solid ${C.line}`};background:${C.panel}">
              <div style="min-width:0">
                <div style="display:flex;align-items:center;gap:9px;margin-bottom:3px;flex-wrap:wrap">
                  <span style="font-family:'JetBrains Mono',monospace;font-weight:700;font-size:14.5px;color:${C.lilac}">${esc(p.short)}</span>
                  <span style="font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.muted}">${esc(p.version)}</span>
                  <span style="font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:700;color:${rs.color};background:${rs.bg};border:1px solid ${rs.border};padding:2px 8px;border-radius:100px">${p.role}</span>
                </div>
                <div style="font-family:'JetBrains Mono',monospace;font-size:11.5px;color:${C.muted}">↓ ${esc(p.installsWeekLabel)}/wk · ★ ${p.stars.toLocaleString()}</div>
              </div>
              <span style="font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.muted};white-space:nowrap">updated ${esc(relative(p.updatedAt))}</span>
            </a>`;
            })
            .join("") || `<div style="padding:18px;color:${C.muted};font-size:13.5px;background:${C.panel}">You haven't published any packages yet.</div>`;
    const bio = s.bioDraft != null ? s.bioDraft : u.bio || "";
    return `
      <div>
        <div style="display:flex;align-items:flex-start;gap:20px;background:${C.panel};border:1px solid ${C.line};border-radius:18px;padding:26px;margin-bottom:18px;flex-wrap:wrap">
          ${avatarSpan(u.name, u.initial, u.bg, u.avatarUrl, 76, 30)}
          <div style="flex:1;min-width:200px">
            <h1 style="font-weight:800;font-size:26px;letter-spacing:-0.6px;margin:0 0 3px">${esc(u.name)}</h1>
            <div style="font-family:'JetBrains Mono',monospace;font-size:13px;color:${C.lilac};margin-bottom:12px">@${esc(u.login)}</div>
            ${bio ? `<p style="font-size:14.5px;line-height:1.6;color:${C.soft};margin:0 0 12px;max-width:520px">${esc(bio)}</p>` : ""}
            <div style="display:flex;gap:16px;flex-wrap:wrap;font-family:'JetBrains Mono',monospace;font-size:12px;color:${C.muted};margin-bottom:${profileStats(u) ? "12px" : "0"}">
              ${u.company ? `<span>⌂ ${esc(u.company)}</span>` : ""}
              ${u.location ? `<span>⌖ ${esc(u.location)}</span>` : ""}
              ${u.blog ? `<a href="${escAttr(profileHref(u.blog))}" target="_blank" rel="noopener" style="color:${C.lilac};text-decoration:none">↗ ${esc(u.blog)}</a>` : ""}
              ${u.twitterUsername ? `<a href="https://twitter.com/${escAttr(u.twitterUsername)}" target="_blank" rel="noopener" style="color:${C.lilac};text-decoration:none">@${esc(u.twitterUsername)}</a>` : ""}
              ${u.createdAt && fullDate(u.createdAt) ? `<span>Member since ${esc(fullDate(u.createdAt))}</span>` : ""}
              ${u.githubCreatedAt ? `<span>On GitHub since ${esc(fullDate(u.githubCreatedAt))}</span>` : ""}
              <a href="${escAttr(u.htmlUrl || `https://github.com/${u.login}`)}" target="_blank" rel="noopener" style="color:${C.muted};text-decoration:none">⎇ github.com/${esc(u.login)}</a>
            </div>
            ${profileStats(u)}
          </div>
          <div style="display:flex;gap:8px;flex-shrink:0">
            <button data-act="sync-github" style="font-family:'JetBrains Mono',monospace;font-size:13px;font-weight:700;color:${C.text};background:transparent;border:1px solid ${C.line2};padding:9px 15px;border-radius:9px;cursor:pointer">⟳ Sync from GitHub</button>
            <a href="#/account" data-act="acct-tab" data-arg="settings" style="text-decoration:none;font-family:'JetBrains Mono',monospace;font-size:13px;font-weight:700;color:${C.text};border:1px solid ${C.line2};padding:9px 15px;border-radius:9px">Edit profile</a>
          </div>
        </div>

        <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:14px;margin-bottom:26px">
          <div style="background:${C.panel};border:1px solid ${C.line};border-radius:14px;padding:18px 20px;text-align:center"><div style="font-family:'JetBrains Mono',monospace;font-weight:700;font-size:28px;color:${C.lilac};line-height:1">${owned.length}</div><div style="font-size:12.5px;color:${C.muted};margin-top:5px">packages published</div></div>
          <a href="#/account" data-act="acct-tab" data-arg="stars" style="text-decoration:none;background:${C.panel};border:1px solid ${C.line};border-radius:14px;padding:18px 20px;text-align:center;display:block"><div style="font-family:'JetBrains Mono',monospace;font-weight:700;font-size:28px;color:${C.green};line-height:1">${s.starShorts ? s.starShorts.length : "—"}</div><div style="font-size:12.5px;color:${C.muted};margin-top:5px">★ stars given</div></a>
          <div style="background:${C.panel};border:1px solid ${C.line};border-radius:14px;padding:18px 20px;text-align:center"><div style="font-family:'JetBrains Mono',monospace;font-weight:700;font-size:28px;color:${C.pink};line-height:1">${compactNum(owned.reduce((a, b) => a + b.installsWeek, 0))}</div><div style="font-size:12.5px;color:${C.muted};margin-top:5px">installs / week</div></div>
        </div>

        <div style="display:flex;align-items:baseline;justify-content:space-between;margin-bottom:14px">
          <h2 style="font-weight:800;font-size:20px;letter-spacing:-0.5px;margin:0">Your packages</h2>
          <a href="#/account" data-act="acct-tab" data-arg="plugins" style="text-decoration:none;font-family:'JetBrains Mono',monospace;font-size:12.5px;color:${C.lilac}">manage all →</a>
        </div>
        <div style="border:1px solid ${C.line};border-radius:14px;overflow:hidden">${rows}</div>
      </div>`;
}

function acctPlugins(s: AppState): string {
    const owned = ownedPlugins(s);
    const rows =
        owned
            .map((p, i) => {
                const rs = ROLE_STYLE[p.role];
                return `
            <div style="display:grid;grid-template-columns:1fr auto;gap:16px;align-items:center;padding:18px 20px;border-top:${i === 0 ? "none" : `1px solid ${C.line}`};background:${C.panel}">
              <div style="min-width:0">
                <div style="display:flex;align-items:center;gap:9px;margin-bottom:4px;flex-wrap:wrap">
                  <a href="${pkgPath(p)}" data-act="open" data-arg="${escAttr(p.short)}" style="text-decoration:none;font-family:'JetBrains Mono',monospace;font-weight:700;font-size:15px;color:${C.lilac}">${esc(p.short)}</a>
                  <span style="font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.muted}">${esc(p.version)}</span>
                  <span style="font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:700;color:${rs.color};background:${rs.bg};border:1px solid ${rs.border};padding:2px 8px;border-radius:100px">${p.role}</span>
                </div>
                <p style="font-size:13px;color:${C.soft};margin:0 0 6px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${esc(p.description)}</p>
                <div style="font-family:'JetBrains Mono',monospace;font-size:11.5px;color:${C.muted}">↓ ${esc(p.installsWeekLabel)}/wk · ★ ${p.stars.toLocaleString()} · updated ${esc(relative(p.updatedAt))}</div>
              </div>
              <div style="display:flex;gap:8px">
                <a href="${escAttr(p.repository)}" target="_blank" rel="noopener" style="text-decoration:none;font-family:'JetBrains Mono',monospace;font-size:12px;font-weight:700;color:${C.text};background:transparent;border:1px solid ${C.line2};padding:8px 13px;border-radius:8px">Manage</a>
              </div>
            </div>`;
            })
            .join("") || `<div style="padding:18px;color:${C.muted};font-size:13.5px;background:${C.panel}">Nothing published yet. Run <span style="font-family:'JetBrains Mono',monospace;color:${C.lilac}">wago pkg publish</span> in your module.</div>`;
    return `
      <div>
        <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:18px;flex-wrap:wrap;gap:10px">
          <h1 style="font-weight:800;font-size:24px;letter-spacing:-0.6px;margin:0">Your packages <span style="font-family:'JetBrains Mono',monospace;font-size:15px;color:${C.muted};font-weight:500">${owned.length}</span></h1>
          <a href="https://github.com/wago-org/wago" target="_blank" rel="noopener" style="text-decoration:none;font-size:13px;font-weight:700;color:${C.bg};background:${C.lilac};padding:9px 16px;border-radius:9px">Publish a package ↗</a>
        </div>
        <div style="border:1px solid ${C.line};border-radius:14px;overflow:hidden">${rows}</div>
      </div>`;
}

// "Your stars" — the packages the current user has starred, resolved from the
// registry index. starShorts is null while loading.
function acctStars(s: AppState): string {
    const reg = s.registry;
    const shorts = s.starShorts;
    const byShort = new Map((reg?.packages || []).map((p) => [p.short, p]));
    const starred = (shorts || []).map((sh) => byShort.get(sh)).filter((p): p is Package => !!p);

    let body: string;
    if (shorts === null) {
        body = `<div style="padding:20px;color:${C.muted};font-size:14px;background:${C.panel}">Loading your stars…</div>`;
    } else if (starred.length === 0) {
        body = `<div style="padding:26px;color:${C.muted};font-size:14px;background:${C.panel};text-align:center">You haven't starred any packages yet. Browse the registry and hit ★ on ones you like.<div style="margin-top:12px"><a href="/search" data-act="search" style="text-decoration:none;font-family:'JetBrains Mono',monospace;font-size:12.5px;font-weight:700;color:${C.lilac};border:1px solid ${C.line2};padding:8px 15px;border-radius:9px">Browse packages →</a></div></div>`;
    } else {
        const list = starred
            .map(
                (p, i) => `
            <a href="${pkgPath(p)}" data-act="open" data-arg="${escAttr(p.short)}" style="text-decoration:none;display:grid;grid-template-columns:1fr auto;gap:14px;align-items:center;padding:16px 20px;border-top:${i === 0 ? "none" : `1px solid ${C.line}`};background:${C.panel}">
              <div style="min-width:0">
                <div style="display:flex;align-items:center;gap:9px;margin-bottom:3px;flex-wrap:wrap">
                  <span style="font-family:'JetBrains Mono',monospace;font-weight:700;font-size:14.5px;color:${C.lilac}">${esc(p.short)}</span>
                  <span style="font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.muted}">${esc(p.version)}</span>
                  ${p.verified ? `<span style="color:${C.green};font-size:12px">✦</span>` : ""}
                </div>
                <p style="font-size:13px;color:${C.soft};margin:0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${esc(p.description)}</p>
              </div>
              <div style="text-align:right;white-space:nowrap">
                <div style="font-family:'JetBrains Mono',monospace;font-size:12px;color:${C.lilac}">★ ${p.stars.toLocaleString()}</div>
                <div style="font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.muted};margin-top:3px">↓ ${esc(p.installsWeekLabel)}/wk</div>
              </div>
            </a>`,
            )
            .join("");
        body = `<div style="border:1px solid ${C.line};border-radius:14px;overflow:hidden">${list}</div>`;
    }

    const count = shorts === null ? "" : ` <span style="font-family:'JetBrains Mono',monospace;font-size:15px;color:${C.muted};font-weight:500">${starred.length}</span>`;
    return `
      <div>
        <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:18px;flex-wrap:wrap;gap:10px">
          <h1 style="font-weight:800;font-size:24px;letter-spacing:-0.6px;margin:0">Your stars${count}</h1>
          <a href="/search" data-act="search" style="text-decoration:none;font-family:'JetBrains Mono',monospace;font-size:12.5px;font-weight:700;color:${C.lilac}">browse all →</a>
        </div>
        ${body}
      </div>`;
}

// ── public user profile (#/u/{login}) ─────────────────────────────────────────

// Packages a given login is part of, with their role (owner > author > maintainer).
function userPackages(s: AppState, login: string): (Package & { role: string })[] {
    const reg = s.registry;
    if (!reg) return [];
    const key = login.toLowerCase();
    const out: (Package & { role: string })[] = [];
    for (const p of reg.packages) {
        const isOwner = (p.ownerLogin || "").toLowerCase() === key;
        const isAuthor = (p.authors || []).some((a) => (a.github || "").toLowerCase() === key);
        const isContrib = (p.contributors || []).some((c) => c.toLowerCase() === key);
        if (!isOwner && !isAuthor && !isContrib) continue;
        out.push(Object.assign({}, p, { role: isOwner ? "owner" : isAuthor ? "author" : "maintainer" }));
    }
    return out;
}

export function userScreen(s: AppState): string {
    const vu = s.viewUser;
    if (!vu) {
        return `<div style="padding:120px 0;text-align:center;color:${C.muted};font-size:15px">Loading profile…</div>`;
    }
    const login = vu.login;
    const pkgs = userPackages(s, login);
    const totalStars = pkgs.reduce((a, b) => a + (b.stars || 0), 0);
    const totalInstalls = pkgs.reduce((a, b) => a + (b.installsWeek || 0), 0);
    const initial = (vu.name || login || "?").trim()[0]?.toUpperCase() || "?";

    // The "toggle": claimed wago member vs a profile generated from public data.
    const badge = vu.claimed
        ? `<span style="font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;color:${C.bg};background:${C.green};padding:4px 11px;border-radius:100px">✦ wago member</span>`
        : `<span title="This person hasn't signed in to wago yet" style="font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;color:${C.muted};background:${C.deep};border:1px solid ${C.line2};padding:4px 11px;border-radius:100px">generated profile</span>`;

    const meta = [
        vu.company ? `<span>⌂ ${esc(vu.company)}</span>` : "",
        vu.location ? `<span>⌖ ${esc(vu.location)}</span>` : "",
        vu.blog
            ? `<a href="${escAttr(profileHref(vu.blog))}" target="_blank" rel="noopener" style="color:${C.lilac};text-decoration:none">↗ ${esc(vu.blog)}</a>`
            : "",
        vu.twitterUsername
            ? `<a href="https://twitter.com/${escAttr(vu.twitterUsername)}" target="_blank" rel="noopener" style="color:${C.lilac};text-decoration:none">@${esc(vu.twitterUsername)}</a>`
            : "",
        vu.claimed && vu.createdAt && fullDate(vu.createdAt)
            ? `<span>Member since ${esc(fullDate(vu.createdAt))}</span>`
            : "",
        vu.githubCreatedAt ? `<span>On GitHub since ${esc(fullDate(vu.githubCreatedAt))}</span>` : "",
        `<a href="${escAttr(vu.htmlUrl || `https://github.com/${login}`)}" target="_blank" rel="noopener" style="color:${C.muted};text-decoration:none">⎇ github.com/${esc(login)}</a>`,
    ]
        .filter(Boolean)
        .join("");

    const statCard = (value: string, label: string, color: string): string =>
        `<div style="background:${C.panel};border:1px solid ${C.line};border-radius:14px;padding:18px 20px;text-align:center"><div style="font-family:'JetBrains Mono',monospace;font-weight:700;font-size:28px;color:${color};line-height:1">${esc(value)}</div><div style="font-size:12.5px;color:${C.muted};margin-top:5px">${esc(label)}</div></div>`;

    const rows =
        pkgs
            .map((p, i) => {
                const rs = ROLE_STYLE[p.role] || ROLE_STYLE.maintainer;
                return `
            <a href="${pkgPath(p)}" data-act="open" data-arg="${escAttr(p.short)}" style="text-decoration:none;display:grid;grid-template-columns:1fr auto;gap:14px;align-items:center;padding:15px 18px;border-top:${i === 0 ? "none" : `1px solid ${C.line}`};background:${C.panel}">
              <div style="min-width:0">
                <div style="display:flex;align-items:center;gap:9px;margin-bottom:3px;flex-wrap:wrap">
                  <span style="font-family:'JetBrains Mono',monospace;font-weight:700;font-size:14.5px;color:${C.lilac}">${esc(p.short)}</span>
                  <span style="font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.muted}">${esc(p.version)}</span>
                  <span style="font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:700;color:${rs.color};background:${rs.bg};border:1px solid ${rs.border};padding:2px 8px;border-radius:100px">${esc(p.role)}</span>
                </div>
                <p style="font-size:13px;color:${C.soft};margin:0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${esc(p.description)}</p>
              </div>
              <div style="text-align:right;white-space:nowrap"><div style="font-family:'JetBrains Mono',monospace;font-size:11px;color:${C.muted}">★ ${p.stars.toLocaleString()} · ↓ ${esc(p.installsWeekLabel)}/wk</div></div>
            </a>`;
            })
            .join("") ||
        `<div style="padding:20px;color:${C.muted};font-size:14px;background:${C.panel}">No packages found for this user in the registry.</div>`;

    // Organizations to surface: for a person, their GitHub orgs that own a wago
    // package (the "attached to a wago package" rule); for an org profile, its
    // public members. Both link to the account's wago profile.
    const orgPackageOwners = new Set(
        (s.registry?.packages || []).map((p) => (p.ownerLogin || "").toLowerCase()).filter(Boolean),
    );
    const accounts = vu.isOrg
        ? vu.members || []
        : (vu.orgs || []).filter((o) => orgPackageOwners.has(o.login.toLowerCase()));
    const accountsSection =
        accounts.length > 0
            ? `
  <h2 style="font-weight:800;font-size:20px;letter-spacing:-0.5px;margin:0 0 14px">${vu.isOrg ? "Members" : "Organizations"}</h2>
  <div style="display:flex;flex-wrap:wrap;gap:10px;margin-bottom:26px">
    ${accounts.map((o) => accountChip(o.login, o.avatarUrl)).join("")}
  </div>`
            : "";

    const loadingHint = s.viewUserLoading
        ? `<div style="font-size:12px;color:${C.muted};margin-top:8px">Loading profile…</div>`
        : "";
    const unclaimedNote = !vu.claimed
        ? `<div style="background:${C.deep};border:1px solid ${C.line};border-radius:12px;padding:12px 16px;margin-bottom:18px;font-size:13px;color:${C.soft}">This profile was generated from public registry and GitHub data — <b style="color:${C.text}">${esc(vu.name)}</b> hasn't signed in to wago yet. If this is you, sign in with GitHub to claim it.</div>`
        : "";

    return `
<div style="padding:32px 0 72px">
  <div style="font-family:'JetBrains Mono',monospace;font-size:12.5px;color:${C.muted};margin-bottom:16px"><a href="/" data-act="home" style="text-decoration:none;color:${C.muted}">packages</a> <span style="color:${C.line2}">/</span> <span style="color:${C.lilac}">@${esc(login)}</span></div>
  ${unclaimedNote}
  <div style="display:flex;align-items:flex-start;gap:20px;background:${C.panel};border:1px solid ${C.line};border-radius:18px;padding:26px;margin-bottom:18px;flex-wrap:wrap">
    ${avatarSpan(vu.name, initial, avatarBgFor(login), vu.avatarUrl, 76, 30)}
    <div style="flex:1;min-width:200px">
      <div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin-bottom:3px">
        <h1 style="font-weight:800;font-size:26px;letter-spacing:-0.6px;margin:0">${esc(vu.name)}</h1>
        ${badge}
      </div>
      <div style="font-family:'JetBrains Mono',monospace;font-size:13px;color:${C.lilac};margin-bottom:12px">@${esc(login)}</div>
      ${vu.bio ? `<p style="font-size:14.5px;line-height:1.6;color:${C.soft};margin:0 0 12px;max-width:520px">${esc(vu.bio)}</p>` : ""}
      <div style="display:flex;gap:16px;flex-wrap:wrap;font-family:'JetBrains Mono',monospace;font-size:12px;color:${C.muted}">${meta}</div>
      ${loadingHint}
    </div>
  </div>

  <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:14px;margin-bottom:26px">
    ${statCard(String(pkgs.length), "packages", C.lilac)}
    ${statCard(compactNum(totalStars), "★ stars received", C.green)}
    ${statCard(compactNum(totalInstalls), "installs / week", C.pink)}
  </div>

  ${accountsSection}
  <h2 style="font-weight:800;font-size:20px;letter-spacing:-0.5px;margin:0 0 14px">Packages</h2>
  <div style="border:1px solid ${C.line};border-radius:14px;overflow:hidden">${rows}</div>
</div>`;
}

function acctSettings(s: AppState): string {
    const u = s.user!;
    const bio = s.bioDraft != null ? s.bioDraft : u.bio || "";
    const toggles = [
        { k: "releases", l: "New releases from packages you use" },
        { k: "security", l: "Security advisories" },
        { k: "digest", l: "Weekly digest" },
    ]
        .map((t, i) => {
            const on = s.settings[t.k as keyof AppState["settings"]];
            return `
            <label data-act="setting" data-arg="${t.k}" style="display:flex;align-items:center;justify-content:space-between;gap:14px;padding:12px 0;border-top:${i === 0 ? "none" : `1px solid ${C.line}`};cursor:pointer">
              <span style="font-size:14px;color:#d8cef5">${esc(t.l)}</span>
              <span style="width:34px;height:19px;border-radius:100px;background:${on ? C.green : C.line};position:relative;flex-shrink:0;transition:background .2s"><span style="position:absolute;top:2px;left:${on ? "17px" : "2px"};width:15px;height:15px;border-radius:50%;background:#fff;transition:left .2s"></span></span>
            </label>`;
        })
        .join("");
    return `
      <div style="display:flex;flex-direction:column;gap:18px">
        <h1 style="font-weight:800;font-size:24px;letter-spacing:-0.6px;margin:0">Settings</h1>

        <div style="background:${C.panel};border:1px solid ${C.line};border-radius:16px;padding:22px 24px">
          <div style="font-weight:700;font-size:16px;margin-bottom:16px">Public profile</div>
          <div style="margin-bottom:14px">
            <label style="display:block;font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;letter-spacing:0.5px;color:${C.muted};text-transform:uppercase;margin-bottom:7px">Display name</label>
            <input value="${escAttr(u.name)}" style="width:100%;background:${C.deep};border:1px solid ${C.line};border-radius:10px;padding:12px 14px;color:${C.text};font-size:14.5px;outline:none;box-sizing:border-box" />
          </div>
          <div>
            <label style="display:block;font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;letter-spacing:0.5px;color:${C.muted};text-transform:uppercase;margin-bottom:7px">Bio</label>
            <textarea data-act="bio" style="width:100%;min-height:70px;background:${C.deep};border:1px solid ${C.line};border-radius:10px;padding:12px 14px;color:${C.text};font-size:14px;font-family:'Outfit',sans-serif;resize:vertical;outline:none;box-sizing:border-box">${esc(bio)}</textarea>
          </div>
          <div style="display:flex;justify-content:flex-end;margin-top:16px"><button data-act="save-profile" style="font-family:'JetBrains Mono',monospace;font-size:13px;font-weight:700;color:${C.bg};background:${C.lilac};border:none;padding:10px 18px;border-radius:9px;cursor:pointer">Save changes</button></div>
        </div>

        <div style="background:${C.panel};border:1px solid ${C.line};border-radius:16px;padding:22px 24px">
          <div style="font-weight:700;font-size:16px;margin-bottom:16px">Connected account</div>
          <div style="display:flex;align-items:center;gap:13px;background:${C.deep};border:1px solid ${C.line};border-radius:12px;padding:14px 16px;margin-bottom:14px">
            ${githubIcon(20)}
            <div style="flex:1;min-width:0"><div style="font-size:14px;font-weight:700;color:${C.text}">GitHub</div><div style="font-family:'JetBrains Mono',monospace;font-size:11.5px;color:${C.muted}">@${esc(u.login)} · your only sign-in method</div></div>
          </div>
          <button data-act="sync-github" style="width:100%;display:flex;align-items:center;justify-content:center;gap:9px;font-family:'JetBrains Mono',monospace;font-size:14px;font-weight:700;color:${C.bg};background:${C.lilac};border:none;padding:13px;border-radius:11px;cursor:pointer">⟳ Sync from GitHub</button>
          <div style="font-size:12px;color:${C.muted};margin-top:10px;text-align:center">Refreshes your profile, avatar and email from GitHub.</div>
        </div>

        ${emailsSection(s)}

        <div style="background:${C.panel};border:1px solid ${C.line};border-radius:16px;padding:22px 24px">
          <div style="font-weight:700;font-size:16px;margin-bottom:6px">Notifications</div>
          ${toggles}
        </div>

        <div style="background:#2a1230;border:1px solid #6b3453;border-radius:16px;padding:22px 24px">
          <div style="font-weight:700;font-size:16px;color:#ffb4d2;margin-bottom:14px">Danger zone</div>
          <div style="display:flex;align-items:center;justify-content:space-between;gap:14px;flex-wrap:wrap">
            <div style="min-width:200px"><div style="font-size:14px;font-weight:600;color:${C.text};margin-bottom:2px">Delete account</div><div style="font-size:12.5px;color:#b78ba3">Permanently removes your account and unpublishes your packages.</div></div>
            <button data-act="signout" style="font-family:'JetBrains Mono',monospace;font-size:13px;font-weight:700;color:#ffb4d2;background:transparent;border:1px solid #6b3453;padding:10px 16px;border-radius:9px;cursor:pointer">Delete account</button>
          </div>
        </div>
      </div>`;
}
