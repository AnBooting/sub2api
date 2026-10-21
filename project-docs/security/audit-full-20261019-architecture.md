# Architecture Summary - Sub2API Full Audit 2026-10-19

## Product
Sub2API is an LGPL-3.0 AI API gateway fork (origin AnBooting/sub2api, upstream Wei-Shaw/sub2api). It exposes OpenAI-, Anthropic-, Gemini-compatible HTTP/WebSocket APIs to subscribers behind API keys, while admins manage accounts, groups, pricing, video tasks, shop orders and billing. Sensitive resources include API keys, upstream credentials/tokens, user balances, pricing snapshots, shop/callback signatures, task settlement state and admin configuration.

## Principals and authority
- Anonymous/unauthenticated: only setup/health and public model metadata intended to be public.
- API-key holder: call gateway endpoints, list/use own keys, view own usage, opt into HA group fallback, buy/redeem shop products.
- Authenticated user: same as API-key holder plus account/profile operations and token refresh.
- Admin: manage groups, accounts, pricing controls, API keys, users, settings, orders, migrations.
- Deployed upstream/proxy: within its configured trust boundary; callbacks and outbound providers are external but must follow explicit config contracts.

## Stack and source-visible deployment
- Backend: Go 1.27.2, Gin, Ent, PostgreSQL, Redis. Entry points under backend/cmd/server and backend/internal/server.
- Frontend: Vue 3.5, Vite, Tailwind, Pinia, vue-i18n, pnpm 9.
- Deploy: root Dockerfile, deploy/ compose files, Caddyfile, entrypoint/install scripts, config.example.yaml, .env.example.
- Repository docs: project-docs/ contains architecture, PRD, reviews, security docs; README/README_CN are upstream-facing docs.

## Entry surfaces and main paths
- HTTP gateway: routes in backend/internal/server/routes; openai_* handlers, gemini handlers, messages/responses/chat completions, images/videos, admin API.
- WebSocket: OpenAI WS responses in openai_gateway_handler.go and related bridge/forwarder code.
- Callbacks: yunmao shop callback and payment/fulfillment callbacks.
- Admin API: route registrations in admin.go; handler/admin/* handlers.
- Cost/billing: pricing_* service, billing_service.go, gateway_locked_billing_helper.go, video_billing_* and usage billing repositories.
- Persistence: backend/ent/schema, backend/migrations (latest 250).

## Trust boundaries and strongest controls
- API key auth: middleware and service aPI key auth cache; API keys are scoped to users/groups.
- Admin auth: admin middleware and role checks; routes should require admin on admin group.
- Group authorization: api_key_group_chain and api_key_group_execution services enforce group chain/HA rights.
- Pricing/billing: request-locked pricing and billing snapshots are intended to prevent post-hoc price changes; pricing resolution should reject missing prices fail-closed.
- Callback integrity: yunmao service refuses unknown signatures/placeholders rather than accepting unauthenticated callbacks.
- Secret handling: credentials stored in account JSON; redaction helpers exist in DTO/mappers and ops log sanitization.

## Coverage starting paths
backend/internal/server, backend/internal/handler, backend/internal/service, backend/internal/repository, backend/ent/schema, backend/migrations, backend/internal/config, frontend/src, deploy, tools, project-docs.

## Prior coverage gaps
No prior full security ledgers found in this workspace. Prior targeted reviews fixed pricing/HA/video quote/ARTEX issues; those areas must be re-checked on current source because relevant code changed.