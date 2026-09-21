# Changelog

## 0.1.0 (2026-09-21)


### ⚠ BREAKING CHANGES

* rename hunt to snatch; docs site; go fix gate; 0.1.0

### Features

* **api:** Cleanuparr links with live status and recent strikes (read-only) ([e684a1b](https://github.com/lusoris/SnatchArr/commit/e684a1b67739765b1c85d71b86a839fe5e19fb0d))
* **api:** Configarr import with linked-mode polling ([2a10385](https://github.com/lusoris/SnatchArr/commit/2a1038558d4a51318faaffecd01cd80ceeefadf4))
* **api:** control-plane skeleton on golusoris ([c54888d](https://github.com/lusoris/SnatchArr/commit/c54888d938b6ae51ba49fa7e605e6f258b1e1d3d))
* **api:** download-client backpressure and bandwidth pacing (ADR-0006) ([e5c6de9](https://github.com/lusoris/SnatchArr/commit/e5c6de9f93990ca485bf0f53813360470069abf0))
* **api:** planner refinements - jitter, circuit backoff, afterglow, global stamina ([1d1ca82](https://github.com/lusoris/SnatchArr/commit/1d1ca8292ee982a00c668cb672ab3010625f2e96))
* **api:** run periodic loops on one elected replica ([c6e5410](https://github.com/lusoris/SnatchArr/commit/c6e541039feb865fef61f99ed227fbfaa95ae5fc))
* **api:** runs, history, hourly caps, memory reset and schedules over HTTP ([5b05713](https://github.com/lusoris/SnatchArr/commit/5b05713dffe9a8013c29b911f0b990400bcaad83))
* **api:** Seerr links, request sync, dashboard and instance import ([a4d4d10](https://github.com/lusoris/SnatchArr/commit/a4d4d102329480f5f83abb1f304147863d6ed107))
* **deploy:** Helm chart, kind overlay, ArgoCD and the release pipeline ([892fb08](https://github.com/lusoris/SnatchArr/commit/892fb08840a9dc9ae4fd097d6b43bf8021e0222e))
* Seerr requests are snatched first; focused quickies per request ([abcf912](https://github.com/lusoris/SnatchArr/commit/abcf9124fcc10bde8d9fde9adaca349a1773ae89))
* **web:** SvelteKit SPA on sveltesentio ([5a224fd](https://github.com/lusoris/SnatchArr/commit/5a224fd1fbf8eb893c868584aec4b33dc9a5f0ec))
* **worker:** lease loop, heartbeat and run executor ([3241e3c](https://github.com/lusoris/SnatchArr/commit/3241e3cf588d98d636c2556b510df95e739a04c5))
* **worker:** Rust workspace with hunt-core and arr-client; CI hardening ([d88caa2](https://github.com/lusoris/SnatchArr/commit/d88caa204d1e86dfd0fbe642d6d9073d1c7a533e))
* **worker:** skip queued and grabbed items, circuit breaker, query-cost stamina ([48c0874](https://github.com/lusoris/SnatchArr/commit/48c08744d7116600e51ce805ae28a2116e1a9b1e))


### Bug Fixes

* **api:** boot, session cookie contract, CI permissions ([302a8fa](https://github.com/lusoris/SnatchArr/commit/302a8fa48f894fece3706cf1c1488604401023c1))
* **worker:** bundle Mozilla roots so TLS setup survives empty cert store ([6b49de9](https://github.com/lusoris/SnatchArr/commit/6b49de90cc35e9330fbd0ad6eea1d6035cbccffd))


### Refactoring

* rename hunt to snatch; docs site; go fix gate; 0.1.0 ([77f58a4](https://github.com/lusoris/SnatchArr/commit/77f58a412dcce8f6082df157b5316ab1caaac67d))
