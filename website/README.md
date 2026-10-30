# viva documentation website

Astro owns the static documentation and routes; Svelte islands own interactive
controls. This development website is not part of the native UI or release-linked
runtime. Buttons, tabs and search inputs use source-owned shadcn-svelte components
from the official Vega registry. There are no remote fonts, scripts or analytics;
typography uses the operating system's sans-serif and monospace fonts.

Use Node 22.12+ and npm. From this directory:

```sh
npm ci
npm run dev
npm run check
npm test
npm run build
npm run test:site
npm run preview
```

`dev` and `build` run the Go generator first. Generated inputs are
`../build/docs-data/catalog.json` and `../build/docs-data/public/`; the static
output is `../build/docs-site/`. The development plugin is maintained separately
in `viva-preview.mjs`. Catalog data is read afresh for each route render rather
than cached at module scope. Package versions are pinned in the lockfile.
`check` runs both Astro and Svelte diagnostics; `test` includes the separately
maintained preview-plugin tests, interaction-policy tests and i18n completeness
tests. Run `npm run generate` before tests in a clean checkout. After building,
`test:site` checks both locales' rendered routes, links, flat examples and unique IDs.

## Data and URLs

`src/lib/types.ts` preserves the generator's PascalCase `Page`, `Nav`, `Demo`,
`Section`, and `Capability` fields, and lowercase catalog and coverage fields.
No extra generator fields are required. Page membership comes exclusively from
the generated catalog; the website only checks that page IDs are valid, unique
URL slugs. Examples also come exclusively from generated data. All examples are
stacked vertically with independent previews and reset buttons; the production
component source is expanded once below them.

- `/` redirects to `/zh/`; `/zh/` and `/en/` open the home page.
- `/zh/showcase/` and `/en/showcase/` show applications built with Viva as
  cards: one window screenshot, the name, two sentences, the facts worth
  checking and links to the product and its source. Cards, not case studies:
  detail pages are added only when one application has more to say than a card
  can hold.
- `/zh/docs/<slug>/` and `/en/docs/<slug>/` hold the guides; `/zh/components/` and
  `/en/components/` index the catalog.
- `/zh/ui/<component>/` and `/en/ui/<component>/` document each generated page.
- `/zh/coverage/` and `/en/coverage/` list source components; pending entries are not page links.
- `/preview/index.html?component=<id>&example=<id>&theme=light|dark&source=<revision>` is
  the Go WASM runner; the source fingerprint versions the preview HTML request.
- `/sources/components/<id>.go` is the source-owned component recipe.
- `/sources/internal/componentdemo/<id>.go` is the shared Go example source, not
  a promise of an individual case file or the native command runner.
- `/coverage.json` and `/source-manifest.json` are served unchanged.

Revision labels mean **Go source fingerprint**, not a Git tag or a website asset
hash. Browser previews do not establish native platform support. All generated
platform details, section text and per-example web limitations remain visible.
The page never imitates a Go component using website CSS.

## Languages and documentation controls

`src/lib/i18n.ts` owns the supported locales, routes and typed message dictionaries.
Chinese is the default. The visible 中文 / EN links preserve the current component
or coverage page and work without JavaScript. The URL is the language authority;
there is no hidden browser-language redirect or server session.

Navigation, example titles/descriptions, guidance and interaction feedback are
localized. API names, shell commands, actual Go source and labels
drawn by the Go demo are intentionally unchanged. The first deeply documented
components use hand-written English copy; every additional source-owned recipe
receives bounded generic English guidance and explicit Chinese verification text,
so expanding the catalog cannot silently publish mixed-language prose. Tests cover
dictionary parity, all generated content and source preservation.

Official shadcn component source lives under `src/lib/components/ui/` with registry
URLs and original registry-response digests in `shadcn-sources.json`. `components.json`
records the registry configuration. Bits UI supplies keyboard/accessibility behavior;
Tailwind CSS, tailwind-variants and cn supply styling/composition. These dependencies
are confined to this website, not the Go runtime. The site uses a small local typed
dictionary rather than adding a client i18n framework.

## Interaction boundary

An IntersectionObserver activates each example's iframe when it approaches the
viewport and then disconnects. Already visited examples remain mounted until
leaving the page. Theme changes replace activated iframes; reset replaces only
its own example.
Messages
are accepted only from that exact active `contentWindow` and the page's exact
origin, with `viva-preview-ready` or `viva-preview-error` types. A page `load` event
is not readiness. One 60-second timeout permits explicit retry; there is no
polling or persistent heartbeat. Old readiness messages cannot revive a failed
or replaced iframe. Each example has an independent lifecycle and load timeout.

The native command uses its own generated example ID and appends `-dark` for
dark mode. Copy reports success or failure and
never executes a command. Source text is generated code, rendered as escaped
text (or build-time Shiki output), never assembled with untrusted HTML.

Search uses `/` or Ctrl/Command+K. Arrow keys move through filtered navigation.
Escape clears search or closes the mobile
navigation disclosure. There is no keyboard trap. Themes are stored locally;
storage denial leaves the toggle functional. Without JavaScript, documentation,
navigation, production source, coverage and native commands remain readable, but
interactive controls and WASM previews require JavaScript.

All production URLs are local. Serve the complete built directory with its
generated WASM and source assets to work without an internet connection.
`file://` is not a supported execution environment for the WASM runner.

## Deploying

The site is the Cloudflare Pages project `viva-mcp`, served at
<https://vivaui.dev>. `.github/workflows/website.yml` builds it on GitHub runners
and uploads it with Wrangler, so the build environment is the workflow's, not
Cloudflare's build image. The project name lives in `wrangler.toml`; the output
directory (`../build/docs-site`) is passed on the deploy command line.

Repository setup, once:

1. Add `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID` to the repository
   secrets. The token needs the Cloudflare Pages edit permission.
2. Disable automatic deployments in the Pages project (Settings → Build and
   deployments). Otherwise the Git integration and the workflow both deploy the
   same commit.
3. Keep the repository's Pages Git connection only for its custom domains and
   preview metadata; if automatic deployments are re-enabled, the Cloudflare
   build image also needs `GO_VERSION=1.26.5` because `go.mod` asks for Go 1.26.

The workflow runs `npm ci`, `npm run build`, `npm run test:site`, `npm test` and
`npm run check` on every pull request, so a broken page or payload fails before
it merges. A deploy is skipped on pull requests and when the token is unset.

`src/pages/404.astro` is required, not cosmetic: without a top-level `404.html`,
Cloudflare Pages assumes a single-page application and answers every unknown
path with the root document and a `200`, which turns a broken link into a
silent redirect. `site.test.mjs` fails if that file disappears.

## What a crawler reads

The build writes four files beside the HTML, derived from the pages that were
actually built (`siteFiles` in `astro.config.mjs`), so a new guide cannot be
missing from the sitemap and a deleted one cannot linger as a 404:

- `sitemap.xml` — every reader-facing page, with its `hreflang` translations and
  an `x-default`. About 1,060 entries at the time of writing; about 18 KB compressed.
- `robots.txt` — `User-agent: *`, `Allow: /` and the sitemap location. The
  content-signal terms are still set in the Cloudflare dashboard, because that
  managed file replaces this one when it is enabled.
- `llms.txt` — the guides and the reference indexes with their own summaries,
  which is what an assistant reads instead of crawling every page.
- `_headers` — HSTS, frame and MIME protection for every path, and a one-year
  immutable cache for the hashed `_astro/` assets.

Each page also states its canonical URL, its translations, its `x-default`, its
Open Graph and Twitter card, and a schema.org graph (`Organization` and
`WebSite` on every page, `SoftwareApplication` on the home page, `TechArticle`
and `BreadcrumbList` in the guides). `site.test.mjs` checks all of it against
the built output, so a new route that forgets a canonical fails `test:site`.

Email Address Obfuscation (Scrape Shield) rewrites anything that looks like an
address, and it served `go get github.com/vivaui/viva@v0.1.3` as
`github.com/vivaui/[email protected]` — what every reader copies and every crawler
reads. `BaseLayout.astro` wraps `<body>` in Cloudflare's documented
`<!--email_off-->` marker, which switches the rewrite off for these pages while
the zone setting stays on. Keep the marker: without it, turning the setting back
on silently breaks the install command again.

One setting lives in the Cloudflare dashboard rather than in this repository:

1. **Keep the managed `robots.txt` policy in sync** with the built file. If it
   is enabled, its content-signal block replaces the generated directives.

