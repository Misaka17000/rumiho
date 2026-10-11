# Bundled components

- **mihomo v1.19.17** — GPL-3.0, https://github.com/MetaCubeX/mihomo/tree/v1.19.17.
  The unmodified binary comes from the pinned official Docker image. Corresponding
  source: https://github.com/MetaCubeX/mihomo/archive/refs/tags/v1.19.17.tar.gz.
  Its license is included in `MIHOMO_LICENSE`.
- **zashboard v3.29.1** — MIT, https://github.com/Zephyruso/zashboard.
  Rumiho builds a customized source fork from `frontend/`, based on commit
  `9b867b76a8cbca014423e93af59f7ce0996bc4f4`. Changes integrate login, same-origin
  session authentication, subscription/access management and navigation.
  PWA caching and independent dashboard/core upgrades are disabled.
  Copyright/license and asset notices are retained in `frontend/LICENSE` and
  `frontend/public/THIRD_PARTY_NOTICES.md`, and copied into the built image.
- **MetaCubeX/meta-rules-dat** — https://github.com/MetaCubeX/meta-rules-dat.
  The CN domain and IP rule artifacts are bundled with checksums in
  `assets.lock.json`. They are updated with the application image.

The Go dependencies and their versions are listed in `go.mod` / `go.sum`.
