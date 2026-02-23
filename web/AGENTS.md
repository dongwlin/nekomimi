# AGENTS.md

## Project Overview
- React + TypeScript + Vite frontend project.
- UI stack: `antd` + `tailwindcss` (v4).
- State management: `zustand` (with `persist` middleware).
- Routing: `react-router` v7 with route-level lazy loading.

## Key Entry Points
- App entry: `src/main.tsx`
- Providers: `src/app/providers.tsx`
- Router: `src/app/router.tsx`
- Root layout: `src/app/layouts/RootLayout.tsx`
- Auth layout: `src/app/layouts/AuthLayout.tsx`

## Directory Conventions
- `src/app`: app framework layer (router/providers/layout/guards)
- `src/features`: business modules by domain (`auth`, `dashboard`)
- `src/components`: shared cross-feature components
- `src/stores`: global zustand stores (e.g. theme)
- `src/styles`: global style entry (`index.css`)

## Current Runtime Conventions
- Path alias `@/*` maps to `src/*` (configured in `tsconfig.app.json` and `vite.config.ts`).
- Ant Design theme is configured in `src/app/providers.tsx` using:
  - `theme.defaultAlgorithm` for light
  - `theme.darkAlgorithm` for dark
- Global theme mode is stored in `src/stores/ui.store.ts`.
- Login/session state is stored in `src/features/auth/store.ts` using zustand persist.
- Auth model is single-passphrase login; no user management module/routes in this app.
- Security rotation page is routed at `/settings/security`.
- Route components/layouts are lazy-loaded in `src/app/router.tsx`.

## Build and Dev Commands
- Install deps: `pnpm install`
- Dev server: `pnpm dev`
- Build: `pnpm build`
- Lint: `pnpm lint`
- Preview build: `pnpm preview`

## Code Style and Change Rules
- Keep changes scoped to requested behavior; avoid unrelated refactors.
- Prefer extending existing feature modules over adding parallel patterns.
- New pages should be registered via `src/app/router.tsx` and follow lazy loading.
- New global state should use zustand under `src/stores` or feature-local `store.ts`.
- Prefer antd components for page/form/table scaffolding; use Tailwind utility classes as lightweight layout/style helpers.

## Definition of Done
- `pnpm lint` passes.
- `pnpm build` passes.
- If route behavior changed, validate navigation and auth guard flow manually.
- If theme-related UI changed, verify light/dark toggle in both auth and main layouts.
