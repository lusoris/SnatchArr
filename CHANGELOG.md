# Changelog

## 1.0.0 (2026-09-21)


### ⚠ BREAKING CHANGES

* rename hunt to snatch; docs site; go fix gate; 0.1.0

### Features

* **api:** control-plane skeleton on golusoris ([c54888d](https://github.com/lusoris/SnatchArr/commit/c54888d938b6ae51ba49fa7e605e6f258b1e1d3d))
* **api:** download-client backpressure and bandwidth pacing (ADR-0006) ([e5c6de9](https://github.com/lusoris/SnatchArr/commit/e5c6de9f93990ca485bf0f53813360470069abf0))
* **api:** planner refinements - jitter, circuit backoff, afterglow, global stamina ([1d1ca82](https://github.com/lusoris/SnatchArr/commit/1d1ca8292ee982a00c668cb672ab3010625f2e96))
* **api:** runs, history, hourly caps, memory reset and schedules over HTTP ([5b05713](https://github.com/lusoris/SnatchArr/commit/5b05713dffe9a8013c29b911f0b990400bcaad83))
* **worker:** lease loop, heartbeat and run executor ([3241e3c](https://github.com/lusoris/SnatchArr/commit/3241e3cf588d98d636c2556b510df95e739a04c5))
* **worker:** Rust workspace with hunt-core and arr-client; CI hardening ([d88caa2](https://github.com/lusoris/SnatchArr/commit/d88caa204d1e86dfd0fbe642d6d9073d1c7a533e))
* **worker:** skip queued and grabbed items, circuit breaker, query-cost stamina ([48c0874](https://github.com/lusoris/SnatchArr/commit/48c08744d7116600e51ce805ae28a2116e1a9b1e))


### Bug Fixes

* **api:** boot, session cookie contract, CI permissions ([302a8fa](https://github.com/lusoris/SnatchArr/commit/302a8fa48f894fece3706cf1c1488604401023c1))
* **worker:** bundle Mozilla roots so TLS setup survives empty cert store ([6b49de9](https://github.com/lusoris/SnatchArr/commit/6b49de90cc35e9330fbd0ad6eea1d6035cbccffd))


### Code Refactoring

* rename hunt to snatch; docs site; go fix gate; 0.1.0 ([77f58a4](https://github.com/lusoris/SnatchArr/commit/77f58a412dcce8f6082df157b5316ab1caaac67d))
