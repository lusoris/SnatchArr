# Changelog

## [0.1.3](https://github.com/lusoris/SnatchArr/compare/v0.1.2...v0.1.3) (2026-10-10)


### Features

* **helm:** select in-cluster *arr apps with networkPolicy.arrPeers ([#130](https://github.com/lusoris/SnatchArr/issues/130)) ([77708b0](https://github.com/lusoris/SnatchArr/commit/77708b046c4b7c80637687a1e095d13c49485df8)), closes [#121](https://github.com/lusoris/SnatchArr/issues/121)


### Bug Fixes

* **api:** end a run as failed after its lease expired three times ([#113](https://github.com/lusoris/SnatchArr/issues/113)) ([fa8cd61](https://github.com/lusoris/SnatchArr/commit/fa8cd61d198bb21c8cce5f9cfcdad94adcc54e8f)), closes [#17](https://github.com/lusoris/SnatchArr/issues/17)
* **api:** give the schema migrator the pool's TLS settings ([#126](https://github.com/lusoris/SnatchArr/issues/126)) ([896b84c](https://github.com/lusoris/SnatchArr/commit/896b84cfc7b351e1d1a5f827209bc1bdc104fbb4)), closes [#123](https://github.com/lusoris/SnatchArr/issues/123)
* **api:** make stamina a rolling 60-minute window ([#119](https://github.com/lusoris/SnatchArr/issues/119)) ([30aeaa2](https://github.com/lusoris/SnatchArr/commit/30aeaa2f0ce4825c5f76e93f22d7e414585abda9)), closes [#16](https://github.com/lusoris/SnatchArr/issues/16)
* **api:** start store users only after the schema migrations ([#128](https://github.com/lusoris/SnatchArr/issues/128)) ([692a652](https://github.com/lusoris/SnatchArr/commit/692a6521a35f688ccb51fc9b50a443c94837d961)), closes [#124](https://github.com/lusoris/SnatchArr/issues/124)
* **api:** trim events, runs, afterglow, buckets and sessions every hour ([#117](https://github.com/lusoris/SnatchArr/issues/117)) ([147363b](https://github.com/lusoris/SnatchArr/commit/147363bbf899901b0d478cb392a4da183beb668f)), closes [#15](https://github.com/lusoris/SnatchArr/issues/15)
* **helm:** an empty networkPolicy.arrCidrs no longer allows all egress ([#127](https://github.com/lusoris/SnatchArr/issues/127)) ([a410bad](https://github.com/lusoris/SnatchArr/commit/a410badddf1c71f54d559421fe3e88e5db34bdcb)), closes [#120](https://github.com/lusoris/SnatchArr/issues/120)
* **helm:** keep the Artifact Hub image tags at appVersion ([#129](https://github.com/lusoris/SnatchArr/issues/129)) ([5525b19](https://github.com/lusoris/SnatchArr/commit/5525b192203c0fb3f693ae4df71276cd9cc16a4b)), closes [#122](https://github.com/lusoris/SnatchArr/issues/122)
* **helm:** let release-please bump the chart with its generic updater only ([#133](https://github.com/lusoris/SnatchArr/issues/133)) ([5165bb4](https://github.com/lusoris/SnatchArr/commit/5165bb4e5aac98233f656326c13d17ae7c92452d)), closes [#122](https://github.com/lusoris/SnatchArr/issues/122)

## [0.1.2](https://github.com/lusoris/SnatchArr/compare/v0.1.1...v0.1.2) (2026-10-10)


### Features

* **helm:** mount extra volumes into the API for a database CA ([#109](https://github.com/lusoris/SnatchArr/issues/109)) ([019ad8b](https://github.com/lusoris/SnatchArr/commit/019ad8b42b7134a0367cf1d6e32b8a01e3aebfa0))

## [0.1.1](https://github.com/lusoris/SnatchArr/compare/v0.1.0...v0.1.1) (2026-10-09)


### Bug Fixes

* **helm:** exclude the metadata address only from CIDRs that contain it ([#90](https://github.com/lusoris/SnatchArr/issues/90)) ([343fd3b](https://github.com/lusoris/SnatchArr/commit/343fd3b49786345684891b2b5d4cc13a08a18c73)), closes [#12](https://github.com/lusoris/SnatchArr/issues/12)
* **helm:** generate and require a 32-byte CSRF secret ([#104](https://github.com/lusoris/SnatchArr/issues/104)) ([6da8211](https://github.com/lusoris/SnatchArr/commit/6da8211e78c0dd377a147650031c949eea296584)), closes [#12](https://github.com/lusoris/SnatchArr/issues/12)

## 0.1.0 (2026-10-09)


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
* **deps:** update golusoris to v0.15.0 ([#57](https://github.com/lusoris/SnatchArr/issues/57)) ([48a0fc5](https://github.com/lusoris/SnatchArr/commit/48a0fc5af6b4605e1dec002b5ca5d050c980e443)), closes [#10](https://github.com/lusoris/SnatchArr/issues/10)
* **web:** keep generated inlang files out of the Prettier check ([#51](https://github.com/lusoris/SnatchArr/issues/51)) ([b9f4d70](https://github.com/lusoris/SnatchArr/commit/b9f4d70e6c4915d6ae2ae8b35e8bcaabb1892e7b))
* **worker:** bundle Mozilla roots so TLS setup survives empty cert store ([6b49de9](https://github.com/lusoris/SnatchArr/commit/6b49de90cc35e9330fbd0ad6eea1d6035cbccffd))


### Refactoring

* **build:** move the Go module and Cargo workspace to the root ([#53](https://github.com/lusoris/SnatchArr/issues/53)) ([d333b59](https://github.com/lusoris/SnatchArr/commit/d333b59f78735e81d8da07c746dfd377e749b430))
* rename hunt to snatch; docs site; go fix gate; 0.1.0 ([77f58a4](https://github.com/lusoris/SnatchArr/commit/77f58a412dcce8f6082df157b5316ab1caaac67d))


### Documentation

* align ADRs, docs and the manifest with what v1 ships ([#58](https://github.com/lusoris/SnatchArr/issues/58)) ([2a1cdde](https://github.com/lusoris/SnatchArr/commit/2a1cdded5f8f1af8291bc500e60e468ab6ca61b2)), closes [#11](https://github.com/lusoris/SnatchArr/issues/11)
