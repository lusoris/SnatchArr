# Changelog

## [0.5.2](https://github.com/lusoris/SnatchArr/compare/v0.5.1...v0.5.2) (2026-10-20)


### Features

* **prowlarr:** enforce indexer grab limits and show them per indexer ([#480](https://github.com/lusoris/SnatchArr/issues/480)) ([45b7b00](https://github.com/lusoris/SnatchArr/commit/45b7b0066e75b7204f66af4488ee96b400c17cf7))
* **web:** own shell with command palette and phone bottom bar ([8a5456b](https://github.com/lusoris/SnatchArr/commit/8a5456bc093803279137629594eb108a4fe8f2ab))
* **web:** replace the M3 vesica with an anatomical vulva mark ([#477](https://github.com/lusoris/SnatchArr/issues/477)) ([68739b7](https://github.com/lusoris/SnatchArr/commit/68739b72340093506239b3770e7811241c983053))


### Bug Fixes

* **arrclient:** explain auth proxy redirects and HTML responses ([#471](https://github.com/lusoris/SnatchArr/issues/471)) ([9e3307e](https://github.com/lusoris/SnatchArr/commit/9e3307e1c2f59af26fbf0781fd0aa37a5b0fd594))
* **configarr:** detect the Whisparr kind from the reported version ([#467](https://github.com/lusoris/SnatchArr/issues/467)) ([02e1255](https://github.com/lusoris/SnatchArr/commit/02e125597ff3e7bc82fba954247e2b6ad4a32c8b))
* **dlclients:** name qBittorrent login failures ([#473](https://github.com/lusoris/SnatchArr/issues/473)) ([4703dc0](https://github.com/lusoris/SnatchArr/commit/4703dc0677292e37c66e9377bc1ef71a0111a4e5))
* **dlclients:** observe download clients concurrently, at most 8 ([3a6840a](https://github.com/lusoris/SnatchArr/commit/3a6840aec5c4080a7f041bbaea3f1010c85d5982))
* **events:** open the event stream through header-holding hops ([#481](https://github.com/lusoris/SnatchArr/issues/481)) ([acfd398](https://github.com/lusoris/SnatchArr/commit/acfd398a4a300586d62f721716419db1cc390dce))
* **seerr:** match Seerr servers to instances by API key when URLs differ ([#475](https://github.com/lusoris/SnatchArr/issues/475)) ([6ad29b4](https://github.com/lusoris/SnatchArr/commit/6ad29b4829c3e705ebe1587c730cc18f2f173a38))
* **web:** page language follows the locale, no page overflow on logs and history ([#476](https://github.com/lusoris/SnatchArr/issues/476)) ([53b6f49](https://github.com/lusoris/SnatchArr/commit/53b6f49c4faf849f4dbf217231db6859e1b5a2ec))
* **web:** send the user to login when the session expires ([#472](https://github.com/lusoris/SnatchArr/issues/472)) ([e89df93](https://github.com/lusoris/SnatchArr/commit/e89df939dac85f0cf4337f6a3ffe74fcb324299e))


### Documentation

* **adr:** ADR-0016 own layout and page redo for the web UI ([70344bd](https://github.com/lusoris/SnatchArr/commit/70344bdee7c0e2721fdb0a4594a056b666f859ba))

## [0.5.1](https://github.com/lusoris/SnatchArr/compare/v0.5.0...v0.5.1) (2026-10-20)


### Features

* **queue:** sweep Whisparr queues and force-import Lidarr and Readarr ([ceda19e](https://github.com/lusoris/SnatchArr/commit/ceda19e7af243d578a1856112b99b29759d62307))


### Bug Fixes

* **deps:** update @sveltesentio/ui to 0.8.1 ([#460](https://github.com/lusoris/SnatchArr/issues/460)) ([1dc3257](https://github.com/lusoris/SnatchArr/commit/1dc3257df1bffa8245f0dfe397b770e7cb414fcc))
* **deps:** update goenvoy lidarr, readarr and whisparr modules ([ceda19e](https://github.com/lusoris/SnatchArr/commit/ceda19e7af243d578a1856112b99b29759d62307))
* **e2e:** stop the nav icon test racing the admin entries ([#459](https://github.com/lusoris/SnatchArr/issues/459)) ([936ff0e](https://github.com/lusoris/SnatchArr/commit/936ff0e01f972361824f28c8a9a7cf1651112bae))
* **workergrpc:** rely on the golusoris unlimited grace default ([#462](https://github.com/lusoris/SnatchArr/issues/462)) ([8699c25](https://github.com/lusoris/SnatchArr/commit/8699c258219ddd6a9cb7dc0c44021e9bea75ada9))


### Refactoring

* **leader:** run the periodic loops on golusoris leader/gate ([#463](https://github.com/lusoris/SnatchArr/issues/463)) ([04a246e](https://github.com/lusoris/SnatchArr/commit/04a246ea4d970eafb711c3d1ac6ec175a61b151d))
* **storetest:** run the shared Postgres on golusoris testutil/pg ([#465](https://github.com/lusoris/SnatchArr/issues/465)) ([9f40d92](https://github.com/lusoris/SnatchArr/commit/9f40d92c6bf51e201716c84c20e29738466ba024))
* **web:** check locale parity with @sveltesentio/testing 0.4.0 ([#466](https://github.com/lusoris/SnatchArr/issues/466)) ([a060889](https://github.com/lusoris/SnatchArr/commit/a0608894a5f7b370020478cbbbbcc6ce4dec1d71))

## [0.5.0](https://github.com/lusoris/SnatchArr/compare/v0.4.2...v0.5.0) (2026-10-19)


### ⚠ BREAKING CHANGES

* **metrics:** rename the instance label to arr_instance ([#455](https://github.com/lusoris/SnatchArr/issues/455))

### Features

* **configarr:** declarative snatch and queue policies from a file ([#458](https://github.com/lusoris/SnatchArr/issues/458)) ([15d0496](https://github.com/lusoris/SnatchArr/commit/15d0496f065c2fc81cdb87a2256fd90ca7a65d21))
* **queue:** reason-based classification and rules ([#456](https://github.com/lusoris/SnatchArr/issues/456)) ([d175ae7](https://github.com/lusoris/SnatchArr/commit/d175ae76875390bde85266d24e570ecfbca97267))
* **web:** adopt palette A and a semantic warning token ([#457](https://github.com/lusoris/SnatchArr/issues/457)) ([6e78588](https://github.com/lusoris/SnatchArr/commit/6e78588e3189d6240702660d7035f7e2b19c6c63))
* **web:** grouped navigation shell, M3 brand mark, never-block layout ([#451](https://github.com/lusoris/SnatchArr/issues/451)) ([288d08f](https://github.com/lusoris/SnatchArr/commit/288d08f01f93dd496d2f2ce994c8c0609fecfdaa)), closes [#442](https://github.com/lusoris/SnatchArr/issues/442) [#443](https://github.com/lusoris/SnatchArr/issues/443) [#446](https://github.com/lusoris/SnatchArr/issues/446)
* **web:** split instance detail into child-route subtabs ([#454](https://github.com/lusoris/SnatchArr/issues/454)) ([b9c5490](https://github.com/lusoris/SnatchArr/commit/b9c5490346e17872f4bbd521940cbd2340841bff)), closes [#444](https://github.com/lusoris/SnatchArr/issues/444) [#433](https://github.com/lusoris/SnatchArr/issues/433)


### Bug Fixes

* **metrics:** rename the instance label to arr_instance ([#455](https://github.com/lusoris/SnatchArr/issues/455)) ([5ced7db](https://github.com/lusoris/SnatchArr/commit/5ced7dbf69960de45800b78f4d066e686fe2c3ce))
* **web:** keep every table inside the page at phone width ([#448](https://github.com/lusoris/SnatchArr/issues/448)) ([5749735](https://github.com/lusoris/SnatchArr/commit/5749735692ff39a74803f179c6c4b2e133806982))


### Documentation

* **adr:** record SnatchArr's own UI design and layout (ADR-0015) ([#450](https://github.com/lusoris/SnatchArr/issues/450)) ([798a985](https://github.com/lusoris/SnatchArr/commit/798a98508800aff76fb4cc0d051f38cc8400bc3c))

## [0.4.2](https://github.com/lusoris/SnatchArr/compare/v0.4.1...v0.4.2) (2026-10-19)


### Features

* **configarr:** provision download clients from a file ([#416](https://github.com/lusoris/SnatchArr/issues/416)) ([0f500fa](https://github.com/lusoris/SnatchArr/commit/0f500faab3d8d0695b7fd626da1ac9f5ba47f193))


### Bug Fixes

* **deps:** update module github.com/go-jose/go-jose/v4 to v4.1.6 ([#412](https://github.com/lusoris/SnatchArr/issues/412)) ([4b17d5d](https://github.com/lusoris/SnatchArr/commit/4b17d5dbf3dfbb30cfa793da5b49452f278608e3))
* **e2e:** bound every planted run and every live compose call ([#408](https://github.com/lusoris/SnatchArr/issues/408)) ([7389816](https://github.com/lusoris/SnatchArr/commit/73898161f511c69b62ec2c9a15f2a86f32888003))
* **workergrpc:** stop a woken lease call's wait with its token ([#405](https://github.com/lusoris/SnatchArr/issues/405)) ([5bb3a49](https://github.com/lusoris/SnatchArr/commit/5bb3a49997f1fcdcd0bc5dd7844f614920614fbb))

## [0.4.1](https://github.com/lusoris/SnatchArr/compare/v0.4.0...v0.4.1) (2026-10-18)


### Features

* **queue:** force-import ID-matched releases (ADR-0013) ([#394](https://github.com/lusoris/SnatchArr/issues/394)) ([9c4a08e](https://github.com/lusoris/SnatchArr/commit/9c4a08e23acfea660e406cb0d3499bdbc19acfbf))
* **queue:** shadow the id_matched parity week (ADR-0014) ([#395](https://github.com/lusoris/SnatchArr/issues/395)) ([e9dd2d2](https://github.com/lusoris/SnatchArr/commit/e9dd2d29e6074498d3255a8709ead06e04aa5f86))


### Documentation

* **adr:** record force import of ID-matched releases ([#392](https://github.com/lusoris/SnatchArr/issues/392)) ([6665e77](https://github.com/lusoris/SnatchArr/commit/6665e770482f07427062058606b75c56a8b4344b))

## [0.4.0](https://github.com/lusoris/SnatchArr/compare/v0.3.0...v0.4.0) (2026-10-18)


### Features

* **queue:** make blocklisting a per-rule setting ([#385](https://github.com/lusoris/SnatchArr/issues/385)) ([22ed247](https://github.com/lusoris/SnatchArr/commit/22ed2470a4bea14e76cfa5d13c9a1c462873885e)), closes [#379](https://github.com/lusoris/SnatchArr/issues/379)
* **queue:** strike downloads that are no upgrade over the file on disk ([#386](https://github.com/lusoris/SnatchArr/issues/386)) ([cc555f8](https://github.com/lusoris/SnatchArr/commit/cc555f836f0c9d6e646fb2659e8a233f3384d57a)), closes [#377](https://github.com/lusoris/SnatchArr/issues/377)
* **queue:** strike duplicate downloads for one media item ([#388](https://github.com/lusoris/SnatchArr/issues/388)) ([658e149](https://github.com/lusoris/SnatchArr/commit/658e1498603b0f43bf4656ecf62f341b16e97dd7)), closes [#378](https://github.com/lusoris/SnatchArr/issues/378)


### Bug Fixes

* **deps:** update golusoris to v0.17.0 and core to v0.11.0 ([#389](https://github.com/lusoris/SnatchArr/issues/389)) ([34e9bdf](https://github.com/lusoris/SnatchArr/commit/34e9bdf4c8d1d3dba3d620a4fc3f06285f1642df))
* **queue:** leave ID-matched import blocks to the import unblocker ([#383](https://github.com/lusoris/SnatchArr/issues/383)) ([a7191b3](https://github.com/lusoris/SnatchArr/commit/a7191b3cc8d32d92fe249eab034dea843374060f)), closes [#380](https://github.com/lusoris/SnatchArr/issues/380)
* **queue:** match the Lidarr and Readarr failure states ([#382](https://github.com/lusoris/SnatchArr/issues/382)) ([736a3b4](https://github.com/lusoris/SnatchArr/commit/736a3b4dfad9e7c7f99afb806549d187e5c697e3)), closes [#376](https://github.com/lusoris/SnatchArr/issues/376)
* **security:** keep CSRF protection on and add web security tests ([#372](https://github.com/lusoris/SnatchArr/issues/372)) ([20852f8](https://github.com/lusoris/SnatchArr/commit/20852f8dbeb347e07857c4404b9fa2bf74784fda))


### Refactoring

* **seerr:** read Seerr through goenvoy's typed client ([#374](https://github.com/lusoris/SnatchArr/issues/374)) ([1225d24](https://github.com/lusoris/SnatchArr/commit/1225d24dbfcb3e51f7f4d87f3628d1d9ac79fede))


### Documentation

* **queue:** document queue management per state and test each state ([#375](https://github.com/lusoris/SnatchArr/issues/375)) ([853b97a](https://github.com/lusoris/SnatchArr/commit/853b97a919f95c4555614f7f4916dfa187619548)), closes [#362](https://github.com/lusoris/SnatchArr/issues/362)


### Chores

* **release:** release 0.4.0 ([#391](https://github.com/lusoris/SnatchArr/issues/391)) ([8032688](https://github.com/lusoris/SnatchArr/commit/8032688d5eaef4c60c363b286a327f46b27b5792))

## [0.3.0](https://github.com/lusoris/SnatchArr/compare/v0.2.2...v0.3.0) (2026-10-17)


### ⚠ BREAKING CHANGES

* **api:** remove the Cleanuparr link ([#366](https://github.com/lusoris/SnatchArr/issues/366))

### Features

* **api:** persist queue settings and strike state ([#369](https://github.com/lusoris/SnatchArr/issues/369)) ([b81841f](https://github.com/lusoris/SnatchArr/commit/b81841ff7f7c603827729cf6036add43fa9e33fb)), closes [#48](https://github.com/lusoris/SnatchArr/issues/48)
* **api:** remove the Cleanuparr link ([#366](https://github.com/lusoris/SnatchArr/issues/366)) ([3ce16b6](https://github.com/lusoris/SnatchArr/commit/3ce16b66794d3b03cf3d4a5fc783c67fc7f0ebbb))
* **api:** sweep *arr download queues on the leader ([08fb25b](https://github.com/lusoris/SnatchArr/commit/08fb25bc2a7f73812dd154323a664aaeb9c80a30))
* **contracts:** read the upstream spec pins from goenvoy ([#365](https://github.com/lusoris/SnatchArr/issues/365)) ([d29067f](https://github.com/lusoris/SnatchArr/commit/d29067f5f3e96d2cc1d3509fb461906260dd4882)), closes [#50](https://github.com/lusoris/SnatchArr/issues/50)
* **queue:** add the queue management rule engine ([#368](https://github.com/lusoris/SnatchArr/issues/368)) ([80e888b](https://github.com/lusoris/SnatchArr/commit/80e888b4c812d47a70e52222a4fb6c0062caf261)), closes [#48](https://github.com/lusoris/SnatchArr/issues/48)
* **queue:** edit queue policy and list strikes per instance ([#371](https://github.com/lusoris/SnatchArr/issues/371)) ([0b41d6e](https://github.com/lusoris/SnatchArr/commit/0b41d6e372df19ec76a1d76970842d7c71a8e86a)), closes [#48](https://github.com/lusoris/SnatchArr/issues/48)

## [0.2.2](https://github.com/lusoris/SnatchArr/compare/v0.2.1...v0.2.2) (2026-10-17)


### Features

* **api:** read Seerr through the typed goenvoy seerr client ([#364](https://github.com/lusoris/SnatchArr/issues/364)) ([4a7d237](https://github.com/lusoris/SnatchArr/commit/4a7d237b9b47649b53d3536f7a6125a42feed5e6)), closes [#46](https://github.com/lusoris/SnatchArr/issues/46)
* **chart:** scale the worker on queued runs with an optional KEDA ScaledObject ([#331](https://github.com/lusoris/SnatchArr/issues/331)) ([62b7bcb](https://github.com/lusoris/SnatchArr/commit/62b7bcbb2c9f800a2689fb87bf6d754c4efecfe8))
* **logs:** admin log viewer over a redacted in-memory ring ([#356](https://github.com/lusoris/SnatchArr/issues/356)) ([d0c08db](https://github.com/lusoris/SnatchArr/commit/d0c08db759435d5fc05380c548166fc071ab6d03))
* **notify:** add an Apprise API notification channel ([#354](https://github.com/lusoris/SnatchArr/issues/354)) ([2249c5f](https://github.com/lusoris/SnatchArr/commit/2249c5f6d947819322545540baed574e9e563277))
* ship worker log records to the in-app log viewer ([#357](https://github.com/lusoris/SnatchArr/issues/357)) ([b50f16f](https://github.com/lusoris/SnatchArr/commit/b50f16f0585fd16e2a514ad73a8e336e75af0a4d))
* skip Radarr movies that are not yet available ([#353](https://github.com/lusoris/SnatchArr/issues/353)) ([67a1db2](https://github.com/lusoris/SnatchArr/commit/67a1db227fa4cd7102211af3b57ca51b465a1a9d))
* **web:** show named API events on the live page ([#360](https://github.com/lusoris/SnatchArr/issues/360)) ([f2302b3](https://github.com/lusoris/SnatchArr/commit/f2302b303678f277d831d3ae3fe41e832481814d))


### Bug Fixes

* **api:** end idle LeaseRun calls on time and steady four flaky tests ([#355](https://github.com/lusoris/SnatchArr/issues/355)) ([9dabd4f](https://github.com/lusoris/SnatchArr/commit/9dabd4fa70274d7fcbf148a6e4e3cd84f3a35553))
* **api:** issue a fresh session id at sign-in and password change ([#336](https://github.com/lusoris/SnatchArr/issues/336)) ([f2babc3](https://github.com/lusoris/SnatchArr/commit/f2babc35d0d91b0eb4bcbc960762bb014d0475f4)), closes [#268](https://github.com/lusoris/SnatchArr/issues/268)
* **api:** observe real Deluge 2 web UIs and test every download client live ([#322](https://github.com/lusoris/SnatchArr/issues/322)) ([173e9a1](https://github.com/lusoris/SnatchArr/commit/173e9a1304fd54f7f4e619c139e1a0293b83adff))
* **api:** regenerate the sqlc queries in go generate ([#326](https://github.com/lusoris/SnatchArr/issues/326)) ([8d38bfe](https://github.com/lusoris/SnatchArr/commit/8d38bfeb3b709e524d79be38ba1e75cc561c6424))
* **api:** serve waiting LeaseRun calls in the order they arrived ([#344](https://github.com/lusoris/SnatchArr/issues/344)) ([8a8e688](https://github.com/lusoris/SnatchArr/commit/8a8e688713ae53a295d59c877a3751a874105bbf))
* **ci:** run the pinned govulncheck in make api-verify and CI ([#337](https://github.com/lusoris/SnatchArr/issues/337)) ([9e2b82d](https://github.com/lusoris/SnatchArr/commit/9e2b82db5079845de66a3848e610050daaaa560b))
* **deploy:** place every chart object in the release namespace ([#347](https://github.com/lusoris/SnatchArr/issues/347)) ([3942238](https://github.com/lusoris/SnatchArr/commit/394223854cc7c1b42a34786770b0def14c868a3d))
* **deploy:** refuse pinned images older than the chart's probes ([#332](https://github.com/lusoris/SnatchArr/issues/332)) ([b488e2d](https://github.com/lusoris/SnatchArr/commit/b488e2dcc6e6043ac2a89473e881c1647282249f))
* **e2e:** retry the crash-loop verdict while the API log is unreadable ([#307](https://github.com/lusoris/SnatchArr/issues/307)) ([ed8e118](https://github.com/lusoris/SnatchArr/commit/ed8e11864a02c03095bf243ef1acd3d783ef6940))
* **planner:** plan the instances past the tick bound in the next tick ([#316](https://github.com/lusoris/SnatchArr/issues/316)) ([e239ab0](https://github.com/lusoris/SnatchArr/commit/e239ab03472869f03c0fa82e230cbd5fc0137ab1))
* **web:** flush event-stream headers through the dev and preview proxy ([#359](https://github.com/lusoris/SnatchArr/issues/359)) ([b8878c5](https://github.com/lusoris/SnatchArr/commit/b8878c53f47499a41e08acb9be6a98a17d746522))
* **web:** follow the system colour scheme with the dark theme ([#340](https://github.com/lusoris/SnatchArr/issues/340)) ([f4b17e9](https://github.com/lusoris/SnatchArr/commit/f4b17e97bd9a48776182bc89381dba2e2307621a))
* **web:** give input borders 3:1 contrast in both themes ([#345](https://github.com/lusoris/SnatchArr/issues/345)) ([8e294a7](https://github.com/lusoris/SnatchArr/commit/8e294a7733d1474a2043055e414243733224ac70))
* **worker:** log the first refused lease of a series at info ([#335](https://github.com/lusoris/SnatchArr/issues/335)) ([b554479](https://github.com/lusoris/SnatchArr/commit/b554479378c94bd1ac118decc28ccc4cf9b1718c))
* **worker:** read Whisparr wanted records with their own wire types ([#308](https://github.com/lusoris/SnatchArr/issues/308)) ([1f16e81](https://github.com/lusoris/SnatchArr/commit/1f16e816998f88d0be8d6d7bcfe410eef4b7a2f8))
* **worker:** send a call again that a closing connection never sent ([#327](https://github.com/lusoris/SnatchArr/issues/327)) ([bce0f32](https://github.com/lusoris/SnatchArr/commit/bce0f327499da47582cb8ccfca2c6c7437f41d9a))
* **worker:** serve /readyz and probe it from the chart ([#329](https://github.com/lusoris/SnatchArr/issues/329)) ([dc8a212](https://github.com/lusoris/SnatchArr/commit/dc8a2121f47baad44bf5c851b79433685d3e5583))


### Refactoring

* **api:** read Cleanuparr status and strikes through goenvoy ([#352](https://github.com/lusoris/SnatchArr/issues/352)) ([32c42f2](https://github.com/lusoris/SnatchArr/commit/32c42f2e66e9038b2599d8071d086f6f745a4886)), closes [#47](https://github.com/lusoris/SnatchArr/issues/47)


### Documentation

* **adr:** keep the api and worker split, record the product definition ([#351](https://github.com/lusoris/SnatchArr/issues/351)) ([17f69b6](https://github.com/lusoris/SnatchArr/commit/17f69b604c8ade3afd483bd78b65ba200b674e2b))
* **adr:** SnatchArr manages the download queue; the Cleanuparr link goes ([#363](https://github.com/lusoris/SnatchArr/issues/363)) ([92d88b7](https://github.com/lusoris/SnatchArr/commit/92d88b79c0374b7b5b871caef029616c36622926)), closes [#48](https://github.com/lusoris/SnatchArr/issues/48)
* **deploy:** verify releases with 0.2.1, a version that exists ([#312](https://github.com/lusoris/SnatchArr/issues/312)) ([0010c11](https://github.com/lusoris/SnatchArr/commit/0010c1156365ece8cc71cdaf852dc48c528bacbc))

## [0.2.1](https://github.com/lusoris/SnatchArr/compare/v0.2.0...v0.2.1) (2026-10-17)


### Bug Fixes

* **e2e:** accept the API's current config error in the image check ([#300](https://github.com/lusoris/SnatchArr/issues/300)) ([187f024](https://github.com/lusoris/SnatchArr/commit/187f024805ce9bd727f54cda72cc66d2aab9b874))

## [0.2.0](https://github.com/lusoris/SnatchArr/compare/v0.1.9...v0.2.0) (2026-10-17)


### Features

* **api:** add API key management endpoints ([#272](https://github.com/lusoris/SnatchArr/issues/272)) ([aa109f7](https://github.com/lusoris/SnatchArr/commit/aa109f7523f18cb04868a4f514ae5f6dfabbb441))
* **auth:** add OIDC login and trusted proxy-header auth ([#273](https://github.com/lusoris/SnatchArr/issues/273)) ([129f1b2](https://github.com/lusoris/SnatchArr/commit/129f1b22b9d4a497b78b5d1a69d3ff951bfee2bd))
* **auth:** require a setup secret when set and end sessions on password change ([#267](https://github.com/lusoris/SnatchArr/issues/267)) ([8a2aa0d](https://github.com/lusoris/SnatchArr/commit/8a2aa0d321944c00abd221d49f65a3471f531bea))
* **auth:** throttle failed logins per user and per client address ([#266](https://github.com/lusoris/SnatchArr/issues/266)) ([f237d45](https://github.com/lusoris/SnatchArr/commit/f237d454ffea66903a0ea71eb5425cf575e86b09))
* **cleanuparr:** warn when Cleanuparr Seeker searches the same instance ([#287](https://github.com/lusoris/SnatchArr/issues/287)) ([ce3632f](https://github.com/lusoris/SnatchArr/commit/ce3632f421e6ebde0eb147b4a984bbdb77471b76)), closes [#39](https://github.com/lusoris/SnatchArr/issues/39)
* **deploy:** add the homelab compose bundle and health subcommands ([#294](https://github.com/lusoris/SnatchArr/issues/294)) ([c0da2af](https://github.com/lusoris/SnatchArr/commit/c0da2afb95a1593c5bf29501dabd02fd77e495b7))
* filter snatches by tag, quality profile and release delay ([#282](https://github.com/lusoris/SnatchArr/issues/282)) ([4a226a8](https://github.com/lusoris/SnatchArr/commit/4a226a89f3244c8c3dc7a0204ac3af8f123dedb5))
* **metrics:** export snatch metrics and ship a Grafana dashboard ([#274](https://github.com/lusoris/SnatchArr/issues/274)) ([1df24a5](https://github.com/lusoris/SnatchArr/commit/1df24a55c26025e92a56d80963adb0946e447d31))
* **notify:** notify on failures, open circuits and exhausted stamina ([#275](https://github.com/lusoris/SnatchArr/issues/275)) ([7d0d88e](https://github.com/lusoris/SnatchArr/commit/7d0d88e3a9f43f4070db394bf8339fb57d20cc13))
* **prowlarr:** defer snatches when every indexer is out of queries ([#286](https://github.com/lusoris/SnatchArr/issues/286)) ([dccf197](https://github.com/lusoris/SnatchArr/commit/dccf1971e7c9cf4ddf9f01005e3e059f769488a9)), closes [#38](https://github.com/lusoris/SnatchArr/issues/38)
* **runs:** preview a run without searching (dry run) ([#290](https://github.com/lusoris/SnatchArr/issues/290)) ([dd71d0e](https://github.com/lusoris/SnatchArr/commit/dd71d0e561e2b0dbc45fcba7b47344931ea20d91))
* **web:** account page with API keys and password change ([#280](https://github.com/lusoris/SnatchArr/issues/280)) ([0f5e0d6](https://github.com/lusoris/SnatchArr/commit/0f5e0d6800877a0810bac88771b47d75a9b0d21e))
* **web:** create, edit and delete schedule windows ([#277](https://github.com/lusoris/SnatchArr/issues/277)) ([74c254b](https://github.com/lusoris/SnatchArr/commit/74c254bf8a0b014d52af599c82d733a31a40d752))
* **web:** edit instances, list and cancel runs, reset memory ([#278](https://github.com/lusoris/SnatchArr/issues/278)) ([3a597ac](https://github.com/lusoris/SnatchArr/commit/3a597acd04e9c544da13790e5369c7e715d30c55)), closes [#30](https://github.com/lusoris/SnatchArr/issues/30)
* **web:** manage download clients and integration links ([#279](https://github.com/lusoris/SnatchArr/issues/279)) ([4aae3a8](https://github.com/lusoris/SnatchArr/commit/4aae3a87e89e43772f45b5ceed040fa27ddc0fc0)), closes [#31](https://github.com/lusoris/SnatchArr/issues/31)
* **web:** show the last connection test on every instance view ([#276](https://github.com/lusoris/SnatchArr/issues/276)) ([9f3e3a4](https://github.com/lusoris/SnatchArr/commit/9f3e3a4804b3bc05122b03f3df7147ff7c01e246))


### Bug Fixes

* **ci:** run the CNPG upgrade test on the images of the checkout ([#299](https://github.com/lusoris/SnatchArr/issues/299)) ([f7b5d5d](https://github.com/lusoris/SnatchArr/commit/f7b5d5dbe47df9628760b188341a62d9961ca78f))
* **ci:** show failing tests in api-cover and bound the admin statement ([#284](https://github.com/lusoris/SnatchArr/issues/284)) ([d94b466](https://github.com/lusoris/SnatchArr/commit/d94b46689859330629ef6424328ed2c0bcd64143))
* **deploy:** pin the development compose images by digest ([#293](https://github.com/lusoris/SnatchArr/issues/293)) ([2c2ce3f](https://github.com/lusoris/SnatchArr/commit/2c2ce3f6197cd2bab3df86fd7f6182565869708f))
* **deps:** update module github.com/go-chi/chi/v5 to v5.5.0 ([#262](https://github.com/lusoris/SnatchArr/issues/262)) ([2c9f6c4](https://github.com/lusoris/SnatchArr/commit/2c9f6c4120ee0d62af391a5e99beaacf5afc09d5))
* **release:** attest the release page and pin verification to the tag commit ([#255](https://github.com/lusoris/SnatchArr/issues/255)) ([6111de5](https://github.com/lusoris/SnatchArr/commit/6111de5a7bc656f1d3b4b73695ac2396a8399eae))
* **web:** route remaining markup literals through Paraglide ([#281](https://github.com/lusoris/SnatchArr/issues/281)) ([c6ce6fd](https://github.com/lusoris/SnatchArr/commit/c6ce6fd3053f1591ee4726bba80337869b12b7b6))
* **worker:** charge an artist or author search as one search ([#283](https://github.com/lusoris/SnatchArr/issues/283)) ([48ec730](https://github.com/lusoris/SnatchArr/commit/48ec7306c15979cfc74a035795c894f720da094a)), closes [#180](https://github.com/lusoris/SnatchArr/issues/180)


### Refactoring

* move test-only helpers out of production code ([#270](https://github.com/lusoris/SnatchArr/issues/270)) ([fca9d40](https://github.com/lusoris/SnatchArr/commit/fca9d40d31b9bce4a2bbcce9fd57116703861d7b))


### Documentation

* **deploy:** document and test the Postgres 18 upgrade of an installed database ([#257](https://github.com/lusoris/SnatchArr/issues/257)) ([c580a7b](https://github.com/lusoris/SnatchArr/commit/c580a7b59d6c838571023602f9c45bd08bb39c63)), closes [#252](https://github.com/lusoris/SnatchArr/issues/252)


### Chores

* **release:** release 0.2.0 ([#297](https://github.com/lusoris/SnatchArr/issues/297)) ([e340637](https://github.com/lusoris/SnatchArr/commit/e340637f91c7a5e172b49665d72552e0af36a4b3))

## [0.1.9](https://github.com/lusoris/SnatchArr/compare/v0.1.8...v0.1.9) (2026-10-15)


### Bug Fixes

* **api:** refuse deleting a Configarr instance that config.yml declares ([#224](https://github.com/lusoris/SnatchArr/issues/224)) ([2e2c6d2](https://github.com/lusoris/SnatchArr/commit/2e2c6d2029f2aed4197acd000d7391c00316b002))
* **contracts:** keep Radarr tags out of the Sonarr pin ([#244](https://github.com/lusoris/SnatchArr/issues/244)) ([7343f41](https://github.com/lusoris/SnatchArr/commit/7343f410e98f93f99dc17c9bc598ef3d3be4cab9))
* **deps:** update module github.com/go-chi/chi/v5 to v5.4.0 ([#232](https://github.com/lusoris/SnatchArr/issues/232)) ([5c86d3e](https://github.com/lusoris/SnatchArr/commit/5c86d3ef06ade5f835abe67361c915908db78573))
* **deps:** update module github.com/golusoris/golusoris to v0.16.5 ([#247](https://github.com/lusoris/SnatchArr/issues/247)) ([6e5397a](https://github.com/lusoris/SnatchArr/commit/6e5397ad73a3660729aee3338292fd7e081c11d3))
* **deps:** update module github.com/golusoris/golusoris/core to v0.10.2 ([#228](https://github.com/lusoris/SnatchArr/issues/228)) ([717aaa0](https://github.com/lusoris/SnatchArr/commit/717aaa06b319354ca0680aeffae355205da013b0))
* **deps:** update rustls to 0.23.46 for RUSTSEC-2026-0346..0349 ([#241](https://github.com/lusoris/SnatchArr/issues/241)) ([9c63653](https://github.com/lusoris/SnatchArr/commit/9c63653747f3f5789dc40480dc7eb0f6f9578b42))
* **deps:** update sveltesentio ([#233](https://github.com/lusoris/SnatchArr/issues/233)) ([d0f3921](https://github.com/lusoris/SnatchArr/commit/d0f39216115b6f55484158a91528631927d03a7e))

## [0.1.8](https://github.com/lusoris/SnatchArr/compare/v0.1.7...v0.1.8) (2026-10-14)


### Bug Fixes

* **api:** check credentials on the event stream route ([#222](https://github.com/lusoris/SnatchArr/issues/222)) ([d3a9119](https://github.com/lusoris/SnatchArr/commit/d3a9119b7c0539a4e67235f60c65b22e96f5e08a))
* **docker:** build the arm64 api binary for arm64 and smoke-test the images ([#220](https://github.com/lusoris/SnatchArr/issues/220)) ([52205ed](https://github.com/lusoris/SnatchArr/commit/52205ed02730f7178aaadccb700b2d985bf47b6e))

## [0.1.7](https://github.com/lusoris/SnatchArr/compare/v0.1.6...v0.1.7) (2026-10-14)


### Features

* report unconfirmed worker events and retry CompleteRun ([#207](https://github.com/lusoris/SnatchArr/issues/207)) ([6a1002d](https://github.com/lusoris/SnatchArr/commit/6a1002dada8e84296493c9236b08c8527e270597))


### Bug Fixes

* **api:** keep the test reaper alive between package processes ([#209](https://github.com/lusoris/SnatchArr/issues/209)) ([32a6840](https://github.com/lusoris/SnatchArr/commit/32a6840a10d8a3d94d70570adbfde9ca9ebaee95))

## [0.1.6](https://github.com/lusoris/SnatchArr/compare/v0.1.5...v0.1.6) (2026-10-13)


### Features

* **api:** count failed leader-loop ticks and unreadable schedules ([#203](https://github.com/lusoris/SnatchArr/issues/203)) ([d4ad5a9](https://github.com/lusoris/SnatchArr/commit/d4ad5a95b4b87c50334f11a2f25d5b4f584caaf8))
* **api:** count leased, finished and budgeted runs and worker events ([#204](https://github.com/lusoris/SnatchArr/issues/204)) ([c967e4f](https://github.com/lusoris/SnatchArr/commit/c967e4f7c7aa93160c4364833c5a3635740e1d93))
* **api:** keep the product a connection test found with the instance ([#175](https://github.com/lusoris/SnatchArr/issues/175)) ([5e93da3](https://github.com/lusoris/SnatchArr/commit/5e93da34d39ed1b647a19a189cea9a734df8fa44)), closes [#22](https://github.com/lusoris/SnatchArr/issues/22)
* **api:** name the product a Readarr-kind instance probe reached ([#173](https://github.com/lusoris/SnatchArr/issues/173)) ([ddcba32](https://github.com/lusoris/SnatchArr/commit/ddcba323c5a125adbfc838085e817c4e19d2e858)), closes [#22](https://github.com/lusoris/SnatchArr/issues/22)
* **api:** report the last worker contact in the system status ([#202](https://github.com/lusoris/SnatchArr/issues/202)) ([76fcfe4](https://github.com/lusoris/SnatchArr/commit/76fcfe4883c44ece058006b806e283b328c5815e))
* **web:** label Readarr as a frozen legacy target ([#167](https://github.com/lusoris/SnatchArr/issues/167)) ([729f412](https://github.com/lusoris/SnatchArr/commit/729f4128c5b2a570af2ce896fb449b1a98e3a43f)), closes [#21](https://github.com/lusoris/SnatchArr/issues/21)
* **worker:** serve /livez and API failure metrics ([#201](https://github.com/lusoris/SnatchArr/issues/201)) ([998199f](https://github.com/lusoris/SnatchArr/commit/998199fe99a7850df40ad16f40a0438d30cba919))
* **worker:** test three Readarr forks as targets of their own ([#182](https://github.com/lusoris/SnatchArr/issues/182)) ([520ebd3](https://github.com/lusoris/SnatchArr/commit/520ebd3a4672e073e38f3baac3b22f6d51a70b7a)), closes [#22](https://github.com/lusoris/SnatchArr/issues/22)


### Bug Fixes

* **api:** count the downloads rTorrent transfers ([#190](https://github.com/lusoris/SnatchArr/issues/190)) ([9e49e44](https://github.com/lusoris/SnatchArr/commit/9e49e4431d1875c2f4490f05edae9091213503c4)), closes [#184](https://github.com/lusoris/SnatchArr/issues/184)
* **api:** fail readiness while the leader loops cannot run ([#195](https://github.com/lusoris/SnatchArr/issues/195)) ([33ece09](https://github.com/lusoris/SnatchArr/commit/33ece09f142174b4cbf2c826b19f693cb7234680))
* **api:** stop gRPC connection rotation from cutting worker leases ([#194](https://github.com/lusoris/SnatchArr/issues/194)) ([f995828](https://github.com/lusoris/SnatchArr/commit/f995828d57a436e79b3c0b555adc7d75193cba4d))
* **ci:** fail gates closed on broken renders and missing inputs ([#196](https://github.com/lusoris/SnatchArr/issues/196)) ([a3aaa23](https://github.com/lusoris/SnatchArr/commit/a3aaa23333b71a4ecfef47330db9737b138fd35f))
* **deps:** move the download-client adapters to goenvoy v2 ([#185](https://github.com/lusoris/SnatchArr/issues/185)) ([e39f852](https://github.com/lusoris/SnatchArr/commit/e39f852533f67a3272dd354d9faf6cdf304af5c8))

## [0.1.5](https://github.com/lusoris/SnatchArr/compare/v0.1.4...v0.1.5) (2026-10-11)


### Bug Fixes

* **api:** take the migrator's TLS and password settings from golusoris 0.16.4 ([#157](https://github.com/lusoris/SnatchArr/issues/157)) ([1448142](https://github.com/lusoris/SnatchArr/commit/14481420eeaf15d2bfeff1a9a194839685cbd978))


### Refactoring

* **api:** one leader-loop runner for planner, Seerr sync and retention ([#158](https://github.com/lusoris/SnatchArr/issues/158)) ([f3bc418](https://github.com/lusoris/SnatchArr/commit/f3bc418b202c6240d345c7319d92ffc4c9340eaf)), closes [#118](https://github.com/lusoris/SnatchArr/issues/118)

## [0.1.4](https://github.com/lusoris/SnatchArr/compare/v0.1.3...v0.1.4) (2026-10-11)


### Bug Fixes

* **api:** debit stamina only for searches that can be sent ([#150](https://github.com/lusoris/SnatchArr/issues/150)) ([e813a5b](https://github.com/lusoris/SnatchArr/commit/e813a5b63c017be2dc3bacde34a9ee5addceb70f)), closes [#142](https://github.com/lusoris/SnatchArr/issues/142)
* **api:** let the dev profile start without APP_CRYPTO_KEY ([#143](https://github.com/lusoris/SnatchArr/issues/143)) ([866fdac](https://github.com/lusoris/SnatchArr/commit/866fdacc5d5a16efaf8f64a6e488e3bd17888df4)), closes [#138](https://github.com/lusoris/SnatchArr/issues/138)
* **api:** stop serving gRPC before the database pool closes ([#156](https://github.com/lusoris/SnatchArr/issues/156)) ([6d60571](https://github.com/lusoris/SnatchArr/commit/6d60571a3b7e46d11a169ac297921a4b752e8f60)), closes [#146](https://github.com/lusoris/SnatchArr/issues/146)
* **helm:** let an arrPeers entry name its ports ([#155](https://github.com/lusoris/SnatchArr/issues/155)) ([90cf73d](https://github.com/lusoris/SnatchArr/commit/90cf73d9c7294d9867aac433ca1cf2811850e9fe)), closes [#145](https://github.com/lusoris/SnatchArr/issues/145)
* **worker:** spend a partial stamina grant instead of deferring the whole target ([#137](https://github.com/lusoris/SnatchArr/issues/137)) ([c0602a3](https://github.com/lusoris/SnatchArr/commit/c0602a374cbee2776822b3e1e8680b51009478f1)), closes [#136](https://github.com/lusoris/SnatchArr/issues/136)

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
