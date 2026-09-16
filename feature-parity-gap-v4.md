# Yen / Theoses2 feature-parity gap audit — v4

Date: 2026-09-16  
Scope: remaining product-surface and provider-validation gaps after v2 and v3  
Oracle: Theoses2 TypeScript pinned by `docs/UPSTREAM.md` at
`3a910426a0db91570392c20c281ae8dfd82e01a1`

This is an audit only. It records remaining candidates from the parity report;
it does not implement them. Memory, channel/session behavior, and the v3
cross-platform portability findings are intentionally separate.

## Findings

### P1 — Native extension UI components are not fully implemented

Theoses2 exposes a broad native TUI component model used by extension UI,
including custom components, loaders, overlays, focus handling, dynamic
borders, status indicators, diff views, Mermaid rendering, and extension UI
state. The core component surface is represented by
`packages/tui/src/tui.ts:23+` and extension UI types by
`packages/coding-agent/src/core/extensions/types.ts:1067-1129,1209-1336`.

Yen supports extension message/entry renderers and correlated extension UI
requests, but its parity report still records native TUI-only extension
components as open (`docs/M10-PARITY-REPORT.md:1023-1025,1028`).

Impact: extensions can participate in text and basic UI protocol flows, but
cannot reproduce the oracle's interactive custom terminal components.

### P1 — Dashboard visual and interaction parity remains incomplete

Yen has a functional embedded dashboard with authentication, session listing,
history, branch switching, sending, stopping, SSE updates, and browser
acceptance coverage. The parity report nevertheless marks full dashboard
visual/browser parity as open (`docs/M10-PARITY-REPORT.md:547`).

Theoses2's dashboard surface has its own layout, loading/error states,
session presentation, interaction details, and browser behavior under
`packages/dashboard/src/index.ts` and `packages/dashboard/src/public/app.js`.

Impact: the Yen dashboard is behaviorally useful but is not yet a visual or
interaction-compatible replacement for the Theoses2 dashboard.

### P2 — Live provider acceptance is incomplete across the expanded matrix

Yen has local construction, wire-format, credential, catalog, and focused
provider tests for several providers. The current parity report still marks
live acceptance open for native image/API flows and providers including
Vertex, Bedrock, GitHub Copilot, and OpenAI Codex
(`docs/M10-PARITY-REPORT.md:1038-1042`).

This is primarily an evidence gap rather than proof that every provider
implementation is missing. Each provider needs an authorized live smoke test
covering authentication, one text turn, tool use where supported, usage/cache
metadata, errors, and image input where the model advertises image support.

Impact: local green tests do not yet establish production compatibility with
real provider accounts and endpoints.

### P2 — Native image/API live acceptance remains open

The parity report specifically leaves live native image/API acceptance open for
Cloudflare and related provider paths (`docs/M10-PARITY-REPORT.md:1038`). Yen
contains image-aware request and response code, but there is not yet a complete
live read-back proving the advertised provider/model combinations accept and
return image data correctly.

Impact: image support should not be treated as parity-certified solely from
unit fixtures.

## Recommended order

1. Define the minimum supported extension UI subset and add one real native
   component end-to-end before attempting the full component catalog.
2. Capture dashboard parity requirements as browser acceptance cases, then
   close the highest-impact visual and interaction differences.
3. Run provider-specific live smoke tests only with explicit credentials and
   record exact model, endpoint, request, response, and usage evidence.
4. Treat unsupported provider/model combinations as explicit catalog or
   capability metadata, not silent runtime failures.

No implementation, release, deployment, restart, memory, channel, or
session change was made for this audit.
