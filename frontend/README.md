# Rumiho dashboard

A source-level customization of [zashboard](https://github.com/Zephyruso/zashboard), pinned to the commit in [UPSTREAM.md](UPSTREAM.md). The Vue app serves login, subscription/access management and the original proxy/connection/rule views through one router and layout. There is no outer page or iframe.

```bash
corepack pnpm install --frozen-lockfile --ignore-scripts
corepack pnpm type-check
corepack pnpm build
corepack pnpm dev
```

Node 24 and pnpm 11.20.0 are build tools only. Docker uses a separate frontend build stage and copies `dist/` to the Go server. The running container does not include Node or pnpm. The development server forwards `/api` and `/mihomo` (including WebSockets) to the local Docker preview on port 18080. Open the development server over HTTP on 127.0.0.1, matching the preview host.

Rumiho changes are concentrated in `src/rumiho/`, `src/views/SetupPage.vue`, the router, authentication transport, backend store and navigation components. The backend destination is always the same origin's `/mihomo`; upstream backend URL parameters and persisted backend lists are ignored. Session and access credentials are held in memory only. Data APIs enforce the existing Go session/CSRF boundaries.

The public SPA shell contains no account state. It uses a cookie-authenticated session to load data. PWA registration and auto-upgrade are disabled; the app and core update with the Docker image. Upstream appearance and proxy/connection display settings remain available. The instance page contains subscription settings, an authenticated HTTP proxy URL with copying/reset, and a login password editor. Mode selection remains on the proxy page. The HTTP proxy uses the login username with a separate proxy password. All application settings and plaintext credentials are atomically saved to `/config/config.yaml` in a writable directory mount; only core caches and generated runtime files live under `/config/cache/`. There is no database or client-subscription generator.

License: upstream MIT, retained in [LICENSE](LICENSE). Bundled asset notices remain in `public/THIRD_PARTY_NOTICES.md`.
