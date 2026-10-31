# Changelog

## [0.8.1](https://github.com/lusoris/SnatchArr/compare/v0.8.0...v0.8.1) (2026-10-31)


### Documentation

* **roadmap:** record the 0.8.0 scope and the work moved to 0.9 ([#956](https://github.com/lusoris/SnatchArr/issues/956)) ([b3d3993](https://github.com/lusoris/SnatchArr/commit/b3d3993233e09a6f4132b64ec4b8773a2d4ec8a5))

## [0.8.0](https://github.com/lusoris/SnatchArr/compare/v0.7.1...v0.8.0) (2026-10-31)


### ⚠ BREAKING CHANGES

* **api:** name an unidentified Readarr fork ([#896](https://github.com/lusoris/SnatchArr/issues/896))
* **api:** name the readarr-rresurrected fork ([#891](https://github.com/lusoris/SnatchArr/issues/891))
* **api:** reject list filters that can never match ([#882](https://github.com/lusoris/SnatchArr/issues/882))

### Features

* **api:** add a settings registry and a parity table (ADR-0044) ([#904](https://github.com/lusoris/SnatchArr/issues/904)) ([97a5fd3](https://github.com/lusoris/SnatchArr/commit/97a5fd3abbfa2b1c6300fb70daed80bf758c97c9))
* **api:** name an unidentified Readarr fork ([#896](https://github.com/lusoris/SnatchArr/issues/896)) ([967596e](https://github.com/lusoris/SnatchArr/commit/967596eaa58acd6fd1849b8a2bba276f2e5ea6cc))
* **api:** name the readarr-rresurrected fork ([#891](https://github.com/lusoris/SnatchArr/issues/891)) ([5bedd68](https://github.com/lusoris/SnatchArr/commit/5bedd6808461019fb983a4024280ef456fdcdd3a))
* **api:** store runtime settings overrides in the database (ADR-0044) ([#907](https://github.com/lusoris/SnatchArr/issues/907)) ([7d1bac3](https://github.com/lusoris/SnatchArr/commit/7d1bac3f41ace9e5f9444731632cea3528136b81))
* **chart:** allow API egress to public HTTPS by default ([#892](https://github.com/lusoris/SnatchArr/issues/892)) ([7eba3fb](https://github.com/lusoris/SnatchArr/commit/7eba3fbf3dcfab2e1d69143a4e3eeedcdc705a1c))
* **idmatchparity:** ship the parity check as a read-only container image ([#885](https://github.com/lusoris/SnatchArr/issues/885)) ([37c1966](https://github.com/lusoris/SnatchArr/commit/37c1966fdbbfc1f484882f6c5be3be904309d4ab))
* **mcp:** wire the SnatchArr MCP server into the project .mcp.json ([#854](https://github.com/lusoris/SnatchArr/issues/854)) ([f6b59b1](https://github.com/lusoris/SnatchArr/commit/f6b59b170c539df28c70a67f9a5c4a6491880ad1))
* **metrics:** expose per-instance last finished run and run interval ([#945](https://github.com/lusoris/SnatchArr/issues/945)) ([2a252a1](https://github.com/lusoris/SnatchArr/commit/2a252a1fda2a3d89b573495bb616cd3b0bc0bf93))
* **metrics:** expose the per-instance eligible item pool ([#952](https://github.com/lusoris/SnatchArr/issues/952)) ([51e1667](https://github.com/lusoris/SnatchArr/commit/51e16670f80987e7d02ad4440f32b1d52b7516f7))
* **priority:** add named priority presets and a reorderable kind list ([#954](https://github.com/lusoris/SnatchArr/issues/954)) ([c5c1554](https://github.com/lusoris/SnatchArr/commit/c5c1554a3c85089a4dcfdafc78d0ff98c6611b2d))
* **release:** refuse a release without a recorded live dogfood pass ([#878](https://github.com/lusoris/SnatchArr/issues/878)) ([d19f3b4](https://github.com/lusoris/SnatchArr/commit/d19f3b4e51f509a412d3b4ffd7d39b7f86473564))
* **web:** add an Environment tab for runtime settings (ADR-0044) ([5fad17b](https://github.com/lusoris/SnatchArr/commit/5fad17bc28f0be57d5f39e1b1e90e9b875aad63c))
* **web:** derive internal links from one entity registry ([#902](https://github.com/lusoris/SnatchArr/issues/902)) ([518baab](https://github.com/lusoris/SnatchArr/commit/518baab9ecdaf57ff743a842fa1713ef5cf8dbc7))
* **web:** make instance cards and rows one click target ([#903](https://github.com/lusoris/SnatchArr/issues/903)) ([4a0a7af](https://github.com/lusoris/SnatchArr/commit/4a0a7afd133b1218d229b33a8e7dbf55402eca05))
* **web:** open events and runs in detail dialogs ([#884](https://github.com/lusoris/SnatchArr/issues/884)) ([b58bb49](https://github.com/lusoris/SnatchArr/commit/b58bb497e555c0b92c927a5620f89d6935ed9681))
* **web:** open the edit form from rows without a detail page ([ab2a6dd](https://github.com/lusoris/SnatchArr/commit/ab2a6ddfa33caf149ae50ada7f9512d3ad831355))
* **web:** show each integrated app's own logo ([#886](https://github.com/lusoris/SnatchArr/issues/886)) ([554367a](https://github.com/lusoris/SnatchArr/commit/554367a70083a1b3da09fd780f22b5c63fc8f3e7))


### Bug Fixes

* **api:** keep stored values on an update that omits an optional field ([#947](https://github.com/lusoris/SnatchArr/issues/947)) ([46e1222](https://github.com/lusoris/SnatchArr/commit/46e1222fb654fd0ba701947e672608b149b6ccdb))
* **api:** reject list filters that can never match ([#882](https://github.com/lusoris/SnatchArr/issues/882)) ([0078818](https://github.com/lusoris/SnatchArr/commit/007881861ced92a4aef35f2310f27ee0973c86a2))
* **api:** report environment-only secrets as locked ([5fad17b](https://github.com/lusoris/SnatchArr/commit/5fad17bc28f0be57d5f39e1b1e90e9b875aad63c))
* **api:** save runtime settings under framework environment variables ([#936](https://github.com/lusoris/SnatchArr/issues/936)) ([956716a](https://github.com/lusoris/SnatchArr/commit/956716a8c20cf7d9863db27af72040f9f3a66c65))
* **ci:** count non-served fixes and declared exceptions in the release e2e map ([#955](https://github.com/lusoris/SnatchArr/issues/955)) ([f35ffd1](https://github.com/lusoris/SnatchArr/commit/f35ffd1f3dd00aaf581445e3fbd89e6b9c5cb343))
* **deps:** update pulldown-cmark to 0.13.5 for RUSTSEC-2026-0414 ([#851](https://github.com/lusoris/SnatchArr/issues/851)) ([914d659](https://github.com/lusoris/SnatchArr/commit/914d6594b5a1469eba09549ece8f91f860197396))
* **notify:** keep the stored configuration on an update without config ([#942](https://github.com/lusoris/SnatchArr/issues/942)) ([b317bc5](https://github.com/lusoris/SnatchArr/commit/b317bc5db634b8d1b3ce749a6a412f79cf47f640)), closes [#940](https://github.com/lusoris/SnatchArr/issues/940)
* **release:** accept only a live cluster dogfood status in the release gate ([#948](https://github.com/lusoris/SnatchArr/issues/948)) ([6309a5b](https://github.com/lusoris/SnatchArr/commit/6309a5b0a398555586bc6736596efe46d8a4aa11))
* **tools:** keep parameter enums and referenced schemas in MCP tools ([#883](https://github.com/lusoris/SnatchArr/issues/883)) ([fbad667](https://github.com/lusoris/SnatchArr/commit/fbad6677594bf6403be7d6b54435626e0dbcf72b)), closes [#880](https://github.com/lusoris/SnatchArr/issues/880)
* **tools:** keep the committed licence URL when a lookup fails ([#933](https://github.com/lusoris/SnatchArr/issues/933)) ([f07c422](https://github.com/lusoris/SnatchArr/commit/f07c42251c6aeb633d5a0d1bb351f7b425e23b8c))
* **web:** ask before deleting instances, notification channels and rules ([#913](https://github.com/lusoris/SnatchArr/issues/913)) ([bfb0991](https://github.com/lusoris/SnatchArr/commit/bfb0991893339c68764c353c8b6526c2dd449d58))
* **web:** ask only admins for runtime settings on the Environment tab ([#911](https://github.com/lusoris/SnatchArr/issues/911)) ([3b4f357](https://github.com/lusoris/SnatchArr/commit/3b4f357a5add0d153a572ebf0844dc280a314e56))
* **web:** ask viewers for no admin-only data ([#900](https://github.com/lusoris/SnatchArr/issues/900)) ([74e46da](https://github.com/lusoris/SnatchArr/commit/74e46da10f9b83cb521095bcb20689059550f68d))
* **web:** classify routes for the scroll budget and fit download clients at 1280 ([#917](https://github.com/lusoris/SnatchArr/issues/917)) ([78b3c1a](https://github.com/lusoris/SnatchArr/commit/78b3c1af8b13d0e40541aa7ecb35236ee3f2aa97))
* **web:** fill skeleton bars and gate utility classes on the built CSS ([#924](https://github.com/lusoris/SnatchArr/issues/924)) ([f688792](https://github.com/lusoris/SnatchArr/commit/f688792767450826577e0e45ef2ccae51799d129))
* **web:** fit the About tab licence list into one screen ([#853](https://github.com/lusoris/SnatchArr/issues/853)) ([dd1615f](https://github.com/lusoris/SnatchArr/commit/dd1615f0b117d1a4a406b3068a589bc31e1f9f2c))
* **web:** fit the download client table at 1280px ([#898](https://github.com/lusoris/SnatchArr/issues/898)) ([ed53105](https://github.com/lusoris/SnatchArr/commit/ed531055df7954686ebc30da989ac409fc177a3b))
* **web:** gate utility classes inside Svelte expressions ([#926](https://github.com/lusoris/SnatchArr/issues/926)) ([73d85e5](https://github.com/lusoris/SnatchArr/commit/73d85e50bcf2990043bac2fc2158f248f8127e3b))
* **web:** keep dashboard and instance activity inside the scroll budget ([#879](https://github.com/lusoris/SnatchArr/issues/879)) ([ec780d4](https://github.com/lusoris/SnatchArr/commit/ec780d45f5f4176bc8fb1f9f2c56d4cb1442a549))
* **web:** keep stacked phone tables readable ([#895](https://github.com/lusoris/SnatchArr/issues/895)) ([1c37a55](https://github.com/lusoris/SnatchArr/commit/1c37a551b9808ce05c2edc47c25badac98090e84))
* **web:** page through image sets with a load more button ([#925](https://github.com/lusoris/SnatchArr/issues/925)) ([1bd0112](https://github.com/lusoris/SnatchArr/commit/1bd01126531d7c90d1830a08d323d5d8997e22bd)), closes [#918](https://github.com/lusoris/SnatchArr/issues/918)
* **web:** reflow at 320 px and add a screen-range a11y matrix ([#889](https://github.com/lusoris/SnatchArr/issues/889)) ([f845ad7](https://github.com/lusoris/SnatchArr/commit/f845ad77b3c752c86f47db3351ed626b4d959efe))
* **web:** resolve every minimatch to 11.0.3 for GHSA-g3hv-75wg-m249 ([#951](https://github.com/lusoris/SnatchArr/issues/951)) ([29776eb](https://github.com/lusoris/SnatchArr/commit/29776eb980bf57b81b3080803e01dc8fd3a9f954))
* **web:** save the favourite-sync checkbox on change ([#893](https://github.com/lusoris/SnatchArr/issues/893)) ([d57f998](https://github.com/lusoris/SnatchArr/commit/d57f9982cd44f66e12511deb15e3f05f3845b0c2))
* **web:** scroll every dashboard card to all of its rows ([#890](https://github.com/lusoris/SnatchArr/issues/890)) ([355a06b](https://github.com/lusoris/SnatchArr/commit/355a06bbf4663a15a9b33c301ecce1fb911843ae))
* **web:** use the Seerr square icon for the app logo ([#887](https://github.com/lusoris/SnatchArr/issues/887)) ([8b0dc1b](https://github.com/lusoris/SnatchArr/commit/8b0dc1b3561899ad26b603c2b4533cf6dbf8371e))


### Refactoring

* **sso:** adopt golusoris auth/oidc session flow ([#888](https://github.com/lusoris/SnatchArr/issues/888)) ([6c213fd](https://github.com/lusoris/SnatchArr/commit/6c213fd5b12fcd73e00b4c152daca4571391897b))


### Documentation

* **adr:** add ADR-0045 quality availability and split-quality series ([#922](https://github.com/lusoris/SnatchArr/issues/922)) ([c33404f](https://github.com/lusoris/SnatchArr/commit/c33404f06847fe517da1f9e3d5bbc3e4895644de))
* **adr:** correct ADR-0045 code symbols and contract scope ([#923](https://github.com/lusoris/SnatchArr/issues/923)) ([a740faa](https://github.com/lusoris/SnatchArr/commit/a740faa9ffa4ff86e8d471e08440a4d5342d0c79)), closes [#921](https://github.com/lusoris/SnatchArr/issues/921)

## [0.7.1](https://github.com/lusoris/SnatchArr/compare/v0.7.0...v0.7.1) (2026-10-28)


### Features

* **adult:** serve catalogue sources and favourite sync ([#842](https://github.com/lusoris/SnatchArr/issues/842)) ([fa9ec45](https://github.com/lusoris/SnatchArr/commit/fa9ec4572acc9e906d5c41977107a0f4b17ddda6))
* **adult:** serve synchronized performers and studios ([#843](https://github.com/lusoris/SnatchArr/issues/843)) ([06966c2](https://github.com/lusoris/SnatchArr/commit/06966c2de621701bb9045e1b933d4c1e280103e9))
* **arrclient:** force-import Whisparr v2 and v3 queue items ([#841](https://github.com/lusoris/SnatchArr/issues/841)) ([e0d0a51](https://github.com/lusoris/SnatchArr/commit/e0d0a51dacc41bb13e239c46292fc2a8be0a6206))
* **requests:** serve community request settings ([#846](https://github.com/lusoris/SnatchArr/issues/846)) ([1150310](https://github.com/lusoris/SnatchArr/commit/1150310ba35e6cc0ad39b52cb61b2c36f8459baa))


### Bug Fixes

* **adult:** serve image sets, item files and scans through the API ([#848](https://github.com/lusoris/SnatchArr/issues/848)) ([71327cb](https://github.com/lusoris/SnatchArr/commit/71327cbb73dc30a05f97379b3b4c9f6dd999d5c5))
* **deps:** update golang.org/x/net to v0.62.0 ([#845](https://github.com/lusoris/SnatchArr/issues/845)) ([4736fcd](https://github.com/lusoris/SnatchArr/commit/4736fcd2e959b8e21bd19b6fa0b15a70e0a58774))
* **deps:** update npm non-major ([#617](https://github.com/lusoris/SnatchArr/issues/617)) ([96f7e9a](https://github.com/lusoris/SnatchArr/commit/96f7e9a5b599fde0ab9d1add6212ef5819ac2ff2))
* **notify:** read the failure streak as of the finishing run ([#834](https://github.com/lusoris/SnatchArr/issues/834)) ([5e34679](https://github.com/lusoris/SnatchArr/commit/5e346799092fb13edf9d0434bad746911582ba80)), closes [#612](https://github.com/lusoris/SnatchArr/issues/612)
* **web:** show product display names for instance kinds ([#836](https://github.com/lusoris/SnatchArr/issues/836)) ([25a635c](https://github.com/lusoris/SnatchArr/commit/25a635ce1d080cbdf99283e8442519c5b90fd528))


### Refactoring

* **web:** share one catalogue list between adult pages ([#847](https://github.com/lusoris/SnatchArr/issues/847)) ([e5b0edb](https://github.com/lusoris/SnatchArr/commit/e5b0edb0588ca01323bb8f0b561c2a7c3af7a300))
* **web:** share the requests settings control lookup and admin session setup ([#849](https://github.com/lusoris/SnatchArr/issues/849)) ([f263028](https://github.com/lusoris/SnatchArr/commit/f2630287a7fdabe624c11dbc2647f5cee9a249dd))

## [0.7.0](https://github.com/lusoris/SnatchArr/compare/v0.6.8...v0.7.0) (2026-10-27)


### ⚠ BREAKING CHANGES

* **api:** add stash instance kind ([#823](https://github.com/lusoris/SnatchArr/issues/823))

### Features

* **adult:** add the image-set engine ([#679](https://github.com/lusoris/SnatchArr/issues/679)) ([8de967f](https://github.com/lusoris/SnatchArr/commit/8de967f5277faae07019e80dc78627f7a6df51a4))
* **adult:** promote favourites to canonical tables with external refs ([#824](https://github.com/lusoris/SnatchArr/issues/824)) ([27ea5cc](https://github.com/lusoris/SnatchArr/commit/27ea5cc0c7c895faad78a94e197459e3f3f7e9f9)), closes [#700](https://github.com/lusoris/SnatchArr/issues/700)
* **adult:** sync stash-box favourites across instances ([#674](https://github.com/lusoris/SnatchArr/issues/674)) ([03acc52](https://github.com/lusoris/SnatchArr/commit/03acc52c68076d163b7761d1fa39acae7b45e47f))
* **adult:** write .stashignore at the image set root ([#821](https://github.com/lusoris/SnatchArr/issues/821)) ([b36da1e](https://github.com/lusoris/SnatchArr/commit/b36da1efa23a0238920d75ba71e5d8fd300398de))
* **api:** add stash instance kind ([#823](https://github.com/lusoris/SnatchArr/issues/823)) ([ba11dd6](https://github.com/lusoris/SnatchArr/commit/ba11dd68f37f35e10f8727e36639be2a670e94d4))
* **auth:** link Jellyfin and proxy sign-ins to an existing account ([#671](https://github.com/lusoris/SnatchArr/issues/671)) ([0032ac8](https://github.com/lusoris/SnatchArr/commit/0032ac8da7ffc9e234d96b7f3e8b5bf7be808e31))
* **auth:** make Jellyfin sign-in the only user path ([#741](https://github.com/lusoris/SnatchArr/issues/741)) ([0980ec6](https://github.com/lusoris/SnatchArr/commit/0980ec6893eb5d999014cde6431a5b1a10281551))
* **notify:** add per-user notification targets ([#788](https://github.com/lusoris/SnatchArr/issues/788)) ([3726ca1](https://github.com/lusoris/SnatchArr/commit/3726ca1d7286e3885083afcc6a37f22476965025)), closes [#735](https://github.com/lusoris/SnatchArr/issues/735)
* **prowlarr:** grab path through Prowlarr for kinds without an arr ([#777](https://github.com/lusoris/SnatchArr/issues/777)) ([ada36bb](https://github.com/lusoris/SnatchArr/commit/ada36bbadfde0c5567bafa371aff2db03ad50c00))
* **requests:** add community request settings with operator defaults ([#750](https://github.com/lusoris/SnatchArr/issues/750)) ([b580e29](https://github.com/lusoris/SnatchArr/commit/b580e29d06e84ce7ef77bd94ca911e370889bb33))
* **settings:** persist brand mark and SFW mode per install and user ([#820](https://github.com/lusoris/SnatchArr/issues/820)) ([1398881](https://github.com/lusoris/SnatchArr/commit/1398881b27b3956bf76b7c93231144cc1f34d9ab))
* **snatches:** add item search with a per-user cooldown ([#775](https://github.com/lusoris/SnatchArr/issues/775)) ([b68b9aa](https://github.com/lusoris/SnatchArr/commit/b68b9aa1d4d2eadba49a23fd7ced91c083d06f9b)), closes [#706](https://github.com/lusoris/SnatchArr/issues/706)
* **web:** add hand and penis brand marks with SFW mode selector ([#818](https://github.com/lusoris/SnatchArr/issues/818)) ([5c74299](https://github.com/lusoris/SnatchArr/commit/5c742994afda39d137c3b7676a5f2374ae0470c1))
* **web:** adopt the Violet Amber palette ([#817](https://github.com/lusoris/SnatchArr/issues/817)) ([30eabfc](https://github.com/lusoris/SnatchArr/commit/30eabfc25daa1ea2f77552c65eb15305aca9680d))
* **web:** link Jellyfin and proxy sign-ins from the account page ([#673](https://github.com/lusoris/SnatchArr/issues/673)) ([ad42ec0](https://github.com/lusoris/SnatchArr/commit/ad42ec041878a494f47f142227c36634d04dc207))


### Bug Fixes

* **adult:** open image files through os.Root and keep index on empty mount ([#825](https://github.com/lusoris/SnatchArr/issues/825)) ([8aa9a6a](https://github.com/lusoris/SnatchArr/commit/8aa9a6ae8b74d3e235a151e9e1f80efe86a61e30))
* **adult:** stop merging performers and studios on name alone ([#745](https://github.com/lusoris/SnatchArr/issues/745)) ([10ca34e](https://github.com/lusoris/SnatchArr/commit/10ca34ee12a914f0e6992d3968864994b477688e))
* **deps:** update module github.com/golusoris/goenvoy/arr/seerr to v1.6.0 ([#616](https://github.com/lusoris/SnatchArr/issues/616)) ([e745a1a](https://github.com/lusoris/SnatchArr/commit/e745a1a7a6d4b2ca5dfef02f66abe17c2792e708))


### Refactoring

* **api:** share the empty 204 response of the notify target test ([#822](https://github.com/lusoris/SnatchArr/issues/822)) ([33846df](https://github.com/lusoris/SnatchArr/commit/33846df958d4a878632dc1e36966f04d9d5adee2))
* **store:** embed the request row in ListSeerrRequests ([#826](https://github.com/lusoris/SnatchArr/issues/826)) ([8ecdd9c](https://github.com/lusoris/SnatchArr/commit/8ecdd9ca7fe7fad4c494ecddb87f0d2ff9ff0cd9))
* **web:** fold the clone groups in the dashboard page test ([#829](https://github.com/lusoris/SnatchArr/issues/829)) ([2e88ce0](https://github.com/lusoris/SnatchArr/commit/2e88ce01206150d967e99168dafede01107d9a49))
* **web:** fold the clone groups in the redirect and Prowlarr tests ([#830](https://github.com/lusoris/SnatchArr/issues/830)) ([12efac8](https://github.com/lusoris/SnatchArr/commit/12efac85285e46e1e96f47fc5d94ac42d4c6c99d))
* **web:** fold the duplicated account sign-in rows ([#828](https://github.com/lusoris/SnatchArr/issues/828)) ([2bf15d4](https://github.com/lusoris/SnatchArr/commit/2bf15d49f0eca695707a999808bccd988e2160e1))
* **web:** move the compact table style into app.css ([#814](https://github.com/lusoris/SnatchArr/issues/814)) ([3bbbb7f](https://github.com/lusoris/SnatchArr/commit/3bbbb7f4e57042753504143142387d2c05531726))
* **web:** remove clone groups in the account route tests ([#812](https://github.com/lusoris/SnatchArr/issues/812)) ([fe3e927](https://github.com/lusoris/SnatchArr/commit/fe3e9275230be94644dfeb3b9e1f0d803dea8177))
* **web:** remove clone groups in web/src/lib ([#809](https://github.com/lusoris/SnatchArr/issues/809)) ([b44f661](https://github.com/lusoris/SnatchArr/commit/b44f661efee5fee2a027f9b3b4f69c967b07e9c4))
* **web:** share connection query pairs between cards and pages ([#810](https://github.com/lusoris/SnatchArr/issues/810)) ([e98cf0e](https://github.com/lusoris/SnatchArr/commit/e98cf0ea13c7c665d0150e483abd4e10e57c948f))
* **web:** share one legacy redirect loader and gate its clones ([#805](https://github.com/lusoris/SnatchArr/issues/805)) ([4e5fced](https://github.com/lusoris/SnatchArr/commit/4e5fcedf8480691e7bacb94bbbbef0f2efc5be85))
* **web:** share the credential fields and auth submit handler ([#816](https://github.com/lusoris/SnatchArr/issues/816)) ([92f46ed](https://github.com/lusoris/SnatchArr/commit/92f46edc07eb53acf855baa4597935735bc93a66))
* **web:** share the link list section of prowlarr and seerr ([#811](https://github.com/lusoris/SnatchArr/issues/811)) ([366fddc](https://github.com/lusoris/SnatchArr/commit/366fddc80b68d1781192ff0154f18904bda67101))
* **web:** share the nav list of drawer and sidebar ([#813](https://github.com/lusoris/SnatchArr/issues/813)) ([29602f4](https://github.com/lusoris/SnatchArr/commit/29602f41732d32d916681cd60a13a7606f1c1d1c))
* **web:** share the settings save and query guard in system forms ([#815](https://github.com/lusoris/SnatchArr/issues/815)) ([0eb2795](https://github.com/lusoris/SnatchArr/commit/0eb279564355b8f0aaf7accbf747489803f3f41f))


### Documentation

* **adr:** accept ADR-0031, creator content through FansDB ([#677](https://github.com/lusoris/SnatchArr/issues/677)) ([efd1efc](https://github.com/lusoris/SnatchArr/commit/efd1efcbc64b717fc2b98ce4e16111c47e502ab7))
* **adr:** accept ADR-0032, Stash stays an optional companion ([#676](https://github.com/lusoris/SnatchArr/issues/676)) ([d7e907d](https://github.com/lusoris/SnatchArr/commit/d7e907dbb10b94d3e6c1a73c9398d2401c95bac3))
* **adr:** accept ADR-0033 on adult identity and the normalized model ([#678](https://github.com/lusoris/SnatchArr/issues/678)) ([6d7a2ae](https://github.com/lusoris/SnatchArr/commit/6d7a2ae0dcff11dec8841aab0c07c8897b56d503))
* **adr:** accept ADR-0040 with the sign-in answer ([#799](https://github.com/lusoris/SnatchArr/issues/799)) ([3469937](https://github.com/lusoris/SnatchArr/commit/34699376a4c8178ee963b5aa993dca0373cf7b94))
* **adr:** accept ADR-0041 with answered questions and slices ([#798](https://github.com/lusoris/SnatchArr/issues/798)) ([8acc2a7](https://github.com/lusoris/SnatchArr/commit/8acc2a718cd4d6b112bcd3169bacec51c5fbd789))
* **adr:** add ADR-0034 adult matching engine ([#680](https://github.com/lusoris/SnatchArr/issues/680)) ([a25dea8](https://github.com/lusoris/SnatchArr/commit/a25dea8cd0be35fb3829ab23ec7df5975d6020e7))
* **adr:** open scope questions for six stash pain themes in ADR-0033 ([#752](https://github.com/lusoris/SnatchArr/issues/752)) ([e22b399](https://github.com/lusoris/SnatchArr/commit/e22b3991e043a7a446389eb3002f23e6f20ad83f))
* **adr:** propose ADR-0035 Jellyfin integration ([#740](https://github.com/lusoris/SnatchArr/issues/740)) ([868fc92](https://github.com/lusoris/SnatchArr/commit/868fc92dab2b7d3f4c60300a22bbbbcabad409e9))
* **adr:** propose ADR-0036 community requests and voting ([#739](https://github.com/lusoris/SnatchArr/issues/739)) ([3841e36](https://github.com/lusoris/SnatchArr/commit/3841e36f7a1f2fa4128923d2a1e687153d4a3e7b))
* **adr:** propose ADR-0038 VMAFx library quality scoring ([#746](https://github.com/lusoris/SnatchArr/issues/746)) ([e312f83](https://github.com/lusoris/SnatchArr/commit/e312f83f797e28f3e01c278ef27a4e7257ae7462))
* **adr:** propose ADR-0039 single metadata source ([#748](https://github.com/lusoris/SnatchArr/issues/748)) ([1e55af1](https://github.com/lusoris/SnatchArr/commit/1e55af139b9e0cad9aa197e6cdc7a328e4975dcf))
* **adr:** propose ADR-0040 Audiobookshelf integration ([#749](https://github.com/lusoris/SnatchArr/issues/749)) ([7d143b6](https://github.com/lusoris/SnatchArr/commit/7d143b6cdfca74403077fec19621fa427c3c8f17))
* **adr:** propose ADR-0041 rule-driven library processing ([#776](https://github.com/lusoris/SnatchArr/issues/776)) ([b6d326c](https://github.com/lusoris/SnatchArr/commit/b6d326c479a91ff3c63a26f5721228f7c9505ec5))
* **adr:** record ADR-0033 and ADR-0037 scope answers ([#778](https://github.com/lusoris/SnatchArr/issues/778)) ([f3212bd](https://github.com/lusoris/SnatchArr/commit/f3212bd60acac45fc8fc10c420bbd123b9318a2b))
* **adr:** record operator answers in ADR-0036 ([#747](https://github.com/lusoris/SnatchArr/issues/747)) ([7dabe51](https://github.com/lusoris/SnatchArr/commit/7dabe51b877dfe2126a92803cf5320b297b32dde))
* **adr:** record popup confirmation of item search and linking ([#751](https://github.com/lusoris/SnatchArr/issues/751)) ([bda7186](https://github.com/lusoris/SnatchArr/commit/bda7186d7e0cbdb8d8fd713f83c6b2b858b4d500))
* **adr:** turn open follow-up sections into issue slice tables ([#808](https://github.com/lusoris/SnatchArr/issues/808)) ([7726fe5](https://github.com/lusoris/SnatchArr/commit/7726fe5cfb3cb6c99e6a634ccd16a1891d7c890e))

## [0.6.8](https://github.com/lusoris/SnatchArr/compare/v0.6.7...v0.6.8) (2026-10-26)


### Features

* **auth:** revalidate Jellyfin sessions against the member policy ([#670](https://github.com/lusoris/SnatchArr/issues/670)) ([e308539](https://github.com/lusoris/SnatchArr/commit/e30853904e88fbe125aa9e928910b26807092b40))
* **modules:** register the adult module, disabled by default ([#668](https://github.com/lusoris/SnatchArr/issues/668)) ([6e3b791](https://github.com/lusoris/SnatchArr/commit/6e3b791de359d2fcccb215cc28fcac7111ed6e2f))


### Bug Fixes

* **auth:** retry the external user upsert on a username conflict ([#667](https://github.com/lusoris/SnatchArr/issues/667)) ([b710a6a](https://github.com/lusoris/SnatchArr/commit/b710a6ae2e155e1d8d168ec7e94b2b3a8cc40c02))
* **web:** give the bottom bar tabs a 44 px touch target ([#665](https://github.com/lusoris/SnatchArr/issues/665)) ([8b71d99](https://github.com/lusoris/SnatchArr/commit/8b71d99254994fd82f1bcc5db4cccef8482e862c))

## [0.6.7](https://github.com/lusoris/SnatchArr/compare/v0.6.6...v0.6.7) (2026-10-26)


### Features

* **auth:** add Jellyfin sign-in and Quick Connect ([#664](https://github.com/lusoris/SnatchArr/issues/664)) ([1c85c37](https://github.com/lusoris/SnatchArr/commit/1c85c37df6eb50a639a4db84fe26056e125044fb))
* **config:** add the Jellyfin default role setting ([#661](https://github.com/lusoris/SnatchArr/issues/661)) ([f43f6fb](https://github.com/lusoris/SnatchArr/commit/f43f6fb26945c61a8a70f792af0e2822f34ed9b9))
* **store:** add the adult module tables ([#663](https://github.com/lusoris/SnatchArr/issues/663)) ([cb261a2](https://github.com/lusoris/SnatchArr/commit/cb261a2293c2f9ee9699e23b747cb881d447b383))


### Bug Fixes

* **web:** let requesters manage their own API keys ([#659](https://github.com/lusoris/SnatchArr/issues/659)) ([16e4d6d](https://github.com/lusoris/SnatchArr/commit/16e4d6ddd47092076e064232e7432ce2af1ab81a))

## [0.6.6](https://github.com/lusoris/SnatchArr/compare/v0.6.5...v0.6.6) (2026-10-25)


### Features

* **auth:** add the requester role for local and single sign-on accounts ([#654](https://github.com/lusoris/SnatchArr/issues/654)) ([726d788](https://github.com/lusoris/SnatchArr/commit/726d788d9cffe8e6dacddb7cb4b164871e2165c7))
* **watchaware:** carry out season gate decisions in act mode ([#658](https://github.com/lusoris/SnatchArr/issues/658)) ([8f6356f](https://github.com/lusoris/SnatchArr/commit/8f6356f225e822ff2373f22e352370b258d8570a))
* **watchaware:** promote held seasons from watch data in observe mode ([#655](https://github.com/lusoris/SnatchArr/issues/655)) ([86dc337](https://github.com/lusoris/SnatchArr/commit/86dc3376de713ea0f488e4cabc322be2ae1e5e55))
* **watchaware:** record season-gate decisions in observe mode ([#652](https://github.com/lusoris/SnatchArr/issues/652)) ([6f67306](https://github.com/lusoris/SnatchArr/commit/6f67306b06a3777d1857dc96ac46ddbb645a3813))
* **watchaware:** show season-gate decisions and emit a gate event ([#653](https://github.com/lusoris/SnatchArr/issues/653)) ([aade628](https://github.com/lusoris/SnatchArr/commit/aade628e1885e95d1ecf2ddd6795c096925ee82a))
* **web:** add connections hub with per-service health ([#640](https://github.com/lusoris/SnatchArr/issues/640)) ([7bb5a52](https://github.com/lusoris/SnatchArr/commit/7bb5a529a01969073b53a7ad5a3ad5c96328b1c3))
* **web:** add system tabs and context help ([#649](https://github.com/lusoris/SnatchArr/issues/649)) ([c7e690b](https://github.com/lusoris/SnatchArr/commit/c7e690b14d8266c476c03d0be1d06360630d5154))
* **web:** gate navigation and routes on module switches ([#628](https://github.com/lusoris/SnatchArr/issues/628)) ([300d0df](https://github.com/lusoris/SnatchArr/commit/300d0dfb842b2de012135b0efb5af51f0503ca40))
* **web:** merge events and server log into one activity feed ([#637](https://github.com/lusoris/SnatchArr/issues/637)) ([119ab03](https://github.com/lusoris/SnatchArr/commit/119ab03dab14b730b5e41f880ce444727dbc56e5))
* **web:** move caps and priority presets to automation ([#639](https://github.com/lusoris/SnatchArr/issues/639)) ([171403e](https://github.com/lusoris/SnatchArr/commit/171403eff8ffb543b40b0f9dea0de3eb3825d3f0))
* **web:** redo the overview as a task-flow operations grid ([#629](https://github.com/lusoris/SnatchArr/issues/629)) ([3855df0](https://github.com/lusoris/SnatchArr/commit/3855df01f38df34571efda97f78fc33069788b94))
* **web:** widen instance pages and pin the instance header ([#638](https://github.com/lusoris/SnatchArr/issues/638)) ([bb0f51e](https://github.com/lusoris/SnatchArr/commit/bb0f51e06a88bcb5a5905544ccf72e24cefbcbd8))


### Bug Fixes

* **queue:** give force imports a 120-second timeout ([#650](https://github.com/lusoris/SnatchArr/issues/650)) ([8847da9](https://github.com/lusoris/SnatchArr/commit/8847da916ebcfed68c47a61751a5c394d640a166))
* **queue:** let import_blocked remove bad matches and unrecognized lines ([#651](https://github.com/lusoris/SnatchArr/issues/651)) ([b64bb9d](https://github.com/lusoris/SnatchArr/commit/b64bb9d0258a7e3b6007b64a4c4d0544fb67af65))
* **web:** drop duplicate message key and refuse duplicates in catalogs ([#635](https://github.com/lusoris/SnatchArr/issues/635)) ([fc34b10](https://github.com/lusoris/SnatchArr/commit/fc34b10714505f0d291d75a0451bbfb87e6ce374))
* **web:** move component classes into [@layer](https://github.com/layer) components ([#642](https://github.com/lusoris/SnatchArr/issues/642)) ([83b7412](https://github.com/lusoris/SnatchArr/commit/83b7412db0a19ca951109bf8c06ebe4c2267e292))
* **web:** show app names in priority preset labels and align rows ([#647](https://github.com/lusoris/SnatchArr/issues/647)) ([d31e3ce](https://github.com/lusoris/SnatchArr/commit/d31e3ce0fd5837cfbcb1a0d4e53915c3c1508f2d))
* **web:** stack tables into labelled cards on phones on every route ([#648](https://github.com/lusoris/SnatchArr/issues/648)) ([119842e](https://github.com/lusoris/SnatchArr/commit/119842e463c31e675c6b55235623a5bb4cb11bda))


### Documentation

* **adr:** record the plugin GUID and the plugin repository in ADR-0028 ([#656](https://github.com/lusoris/SnatchArr/issues/656)) ([2bbf1e0](https://github.com/lusoris/SnatchArr/commit/2bbf1e0d805047923da7bf7a4bbd1d77cc52dc60))
* **adr:** record the plugin release key as stored in ADR-0028 ([#657](https://github.com/lusoris/SnatchArr/issues/657)) ([0a8672c](https://github.com/lusoris/SnatchArr/commit/0a8672cb5d84e87f58adee289bfb26162f06ef75))

## [0.6.5](https://github.com/lusoris/SnatchArr/compare/v0.6.4...v0.6.5) (2026-10-25)


### Features

* **dlclients:** add an opt-in max_queued limit ([#630](https://github.com/lusoris/SnatchArr/issues/630)) ([fdae12c](https://github.com/lusoris/SnatchArr/commit/fdae12c877445e1a3ee8aba1b3b8906bf2445305)), closes [#627](https://github.com/lusoris/SnatchArr/issues/627)
* **llm:** hide and refuse MCP tools of switched-off modules ([#619](https://github.com/lusoris/SnatchArr/issues/619)) ([34ccd83](https://github.com/lusoris/SnatchArr/commit/34ccd83b2b862ac2afa52978eae3cba13485b643))
* **watchaware:** persist sources and run the sync loop behind the module switch ([#623](https://github.com/lusoris/SnatchArr/issues/623)) ([049c3fc](https://github.com/lusoris/SnatchArr/commit/049c3fcbf9cf2a252b07d7184e1288e7dd119493))
* **web:** add desktop width gate, status tokens and StatusIcon ([#621](https://github.com/lusoris/SnatchArr/issues/621)) ([583e63a](https://github.com/lusoris/SnatchArr/commit/583e63ae84bc5d9f48bd6d4ebe4380c154f272c1))
* **web:** move to the six-entry task-flow navigation ([#624](https://github.com/lusoris/SnatchArr/issues/624)) ([0a7384d](https://github.com/lusoris/SnatchArr/commit/0a7384ded92f01274f51726b1583cf8d336b324e))


### Bug Fixes

* **e2e:** retry image pulls in the dev compose smoke ([#625](https://github.com/lusoris/SnatchArr/issues/625)) ([d23f08d](https://github.com/lusoris/SnatchArr/commit/d23f08d1a8af57663a13e48eeb889a740e66c347))
* **licences:** make Go licence URLs independent of the network ([#626](https://github.com/lusoris/SnatchArr/issues/626)) ([85cb73a](https://github.com/lusoris/SnatchArr/commit/85cb73abe1b6ddc7b7831645918b00d269840467))
* **web:** wrap URLs in log and live rows at delimiters ([#622](https://github.com/lusoris/SnatchArr/issues/622)) ([83b4351](https://github.com/lusoris/SnatchArr/commit/83b435171488e93f3eb3b6e5c0a71274e0c8c59b))


### Documentation

* **adr:** accept ADR-0029 task-flow UI, one activity view and context help ([#618](https://github.com/lusoris/SnatchArr/issues/618)) ([9365abf](https://github.com/lusoris/SnatchArr/commit/9365abf9c233b0579a2a59cb46faf120454309b3))

## [0.6.4](https://github.com/lusoris/SnatchArr/compare/v0.6.3...v0.6.4) (2026-10-24)


### Features

* **llm:** add MCP resources and prompts ([#591](https://github.com/lusoris/SnatchArr/issues/591)) ([2bf2b51](https://github.com/lusoris/SnatchArr/commit/2bf2b51d73798bd346d71366d3da7824f0b3e9e8))
* **llm:** add snatcharr mcp stdio proxy ([#592](https://github.com/lusoris/SnatchArr/issues/592)) ([155795d](https://github.com/lusoris/SnatchArr/commit/155795d3354ea58aa1b6a6ab39e65a3593b3d966))
* **llm:** serve live queue contents in the queue resource ([#593](https://github.com/lusoris/SnatchArr/issues/593)) ([f13d219](https://github.com/lusoris/SnatchArr/commit/f13d2190cc04b8d12be25abdf856122aa3eb0426))
* **modules:** add module registry and modules table ([#604](https://github.com/lusoris/SnatchArr/issues/604)) ([5038596](https://github.com/lusoris/SnatchArr/commit/50385969fe4ef43e3eba84f11ef8c3d8248df319))
* **modules:** add module switch HTTP API ([#605](https://github.com/lusoris/SnatchArr/issues/605)) ([881c613](https://github.com/lusoris/SnatchArr/commit/881c613edcb330964e4e86f8640a43676f58e530))
* **modules:** gate module operations by x-module ([#606](https://github.com/lusoris/SnatchArr/issues/606)) ([4420a55](https://github.com/lusoris/SnatchArr/commit/4420a55325296cc309a42eac26673e60caf3a748))
* **watchaware:** add Jellyfin and Emby watch-state sources ([#602](https://github.com/lusoris/SnatchArr/issues/602)) ([fd0b978](https://github.com/lusoris/SnatchArr/commit/fd0b97814f0be6006c9aea3faab250d5fee726c1))


### Bug Fixes

* **dlclients:** take goenvoy rtorrent v2.1.1 for short multicall rows ([#587](https://github.com/lusoris/SnatchArr/issues/587)) ([ba5224a](https://github.com/lusoris/SnatchArr/commit/ba5224a8bd7172ae902a4af3053cd2e4e3a7b1b2))
* **queue:** apply stalled rule to torrents only ([#608](https://github.com/lusoris/SnatchArr/issues/608)) ([4e740c0](https://github.com/lusoris/SnatchArr/commit/4e740c0bd9a03f361976b6788557edaf08a0f8e9))
* **queue:** classify Lidarr import failures by reason lines ([#607](https://github.com/lusoris/SnatchArr/issues/607)) ([a3b9a42](https://github.com/lusoris/SnatchArr/commit/a3b9a42ccb4c0f748927e16beab37377cd236340))
* **web:** wrap instance URLs only at host, port and path boundaries ([#594](https://github.com/lusoris/SnatchArr/issues/594)) ([5d744e0](https://github.com/lusoris/SnatchArr/commit/5d744e0ccaf25ab6a1b4f7d91f2d940f5f5633e0))
* **workergrpc:** search Lidarr upgrades per album ([#609](https://github.com/lusoris/SnatchArr/issues/609)) ([c092827](https://github.com/lusoris/SnatchArr/commit/c092827d5d24dcaae4a3457490250b104836d88a))


### Documentation

* **adr:** close the goenvoy follow-ups of ADR-0024 ([#596](https://github.com/lusoris/SnatchArr/issues/596)) ([be467ea](https://github.com/lusoris/SnatchArr/commit/be467ea53da60a360ed9d93e252c77b6145d3389))
* **adr:** mark ADR-0018 follow-ups done ([#601](https://github.com/lusoris/SnatchArr/issues/601)) ([ed38ebc](https://github.com/lusoris/SnatchArr/commit/ed38ebc488196ffe4188e267908084f24d91413b))
* **adr:** propose ADR-0025 for optional module switches ([#595](https://github.com/lusoris/SnatchArr/issues/595)) ([6473838](https://github.com/lusoris/SnatchArr/commit/6473838c9a9d4f763a2fde2d257168f060f23c93)), closes [#563](https://github.com/lusoris/SnatchArr/issues/563)
* **adr:** propose ADR-0026 accounts, roles and Jellyfin sign-in ([#597](https://github.com/lusoris/SnatchArr/issues/597)) ([95de0fc](https://github.com/lusoris/SnatchArr/commit/95de0fc2760603a7aa369f05b394e6675286124b))
* **adr:** propose ADR-0027 adult module ([#598](https://github.com/lusoris/SnatchArr/issues/598)) ([dab4f07](https://github.com/lusoris/SnatchArr/commit/dab4f07c64062fd2561438f4bd73300eb4255f9a))
* **adr:** propose ADR-0028 Jellyfin companion plugin ([#599](https://github.com/lusoris/SnatchArr/issues/599)) ([62775dc](https://github.com/lusoris/SnatchArr/commit/62775dc87494232590ee4dd3ee93903cdc6a9474))
* **adr:** record answered questions in ADR-0026 to ADR-0028 ([#600](https://github.com/lusoris/SnatchArr/issues/600)) ([9243060](https://github.com/lusoris/SnatchArr/commit/9243060646a65cf78f576598d793fcfd05591805))
* **adr:** record operator answers in ADR-0024 to ADR-0028 ([#603](https://github.com/lusoris/SnatchArr/issues/603)) ([3009b43](https://github.com/lusoris/SnatchArr/commit/3009b43dbac70ad80c3226000f389c686722c3a7))
* **llm:** add client connection guides and llms.txt ([#590](https://github.com/lusoris/SnatchArr/issues/590)) ([ec1127a](https://github.com/lusoris/SnatchArr/commit/ec1127a46e12432b2b75f0499c835b5d63e108f7))
* **llm:** ship the snatcharr agent skills bundle ([#589](https://github.com/lusoris/SnatchArr/issues/589)) ([f65a409](https://github.com/lusoris/SnatchArr/commit/f65a4091feee20208b1225824d2deaf8591312b2))

## [0.6.3](https://github.com/lusoris/SnatchArr/compare/v0.6.2...v0.6.3) (2026-10-23)


### Bug Fixes

* **dlclients:** count rTorrent downloads in the hash queue as queued ([#580](https://github.com/lusoris/SnatchArr/issues/580)) ([2004549](https://github.com/lusoris/SnatchArr/commit/20045494ceff719230f33c7f3a651771bee6e143))
* **instances:** check enabled instances every 5 minutes on the leader ([#582](https://github.com/lusoris/SnatchArr/issues/582)) ([fa2438b](https://github.com/lusoris/SnatchArr/commit/fa2438b7e315056d947544f35b1a95bd022ca35c))
* **web:** keep a gap between label and value on the dashboard rows ([#583](https://github.com/lusoris/SnatchArr/issues/583)) ([abc373c](https://github.com/lusoris/SnatchArr/commit/abc373cb9f9563e9e831545f007972ff867eea96))


### Documentation

* **adr:** plan the season gate in three slices in ADR-0024 ([#579](https://github.com/lusoris/SnatchArr/issues/579)) ([9fb3cfb](https://github.com/lusoris/SnatchArr/commit/9fb3cfbc40bc2c18bd0154a0a75e69847cb6b5bd))

## [0.6.2](https://github.com/lusoris/SnatchArr/compare/v0.6.1...v0.6.2) (2026-10-23)


### Bug Fixes

* **web:** keep the dashboard instance cards inside a phone viewport ([#577](https://github.com/lusoris/SnatchArr/issues/577)) ([2597e23](https://github.com/lusoris/SnatchArr/commit/2597e230b7820d34f760678a0e188e30ff9c6129))


### Refactoring

* **web:** share the navigation footer between drawer and sidebar ([#578](https://github.com/lusoris/SnatchArr/issues/578)) ([3d8ed47](https://github.com/lusoris/SnatchArr/commit/3d8ed47d6c841e284044ac374f6781c18356518d))


### Documentation

* add a roadmap page for 0.7 to 0.9 ([#573](https://github.com/lusoris/SnatchArr/issues/573)) ([037c278](https://github.com/lusoris/SnatchArr/commit/037c27877264df923d1dfa5562574b05357c3dc4))

## [0.6.1](https://github.com/lusoris/SnatchArr/compare/v0.6.0...v0.6.1) (2026-10-23)


### Features

* **web:** show the version, add an About page and a Ko-fi link ([#554](https://github.com/lusoris/SnatchArr/issues/554)) ([79c210d](https://github.com/lusoris/SnatchArr/commit/79c210dd486df59fd2d581dad3627064358eec59))


### Bug Fixes

* **deps:** update golusoris ([#553](https://github.com/lusoris/SnatchArr/issues/553)) ([5a42756](https://github.com/lusoris/SnatchArr/commit/5a42756fde9c95a8ce642ae9d852e6f9cce845bf))
* **dlclients:** count waiting jobs as queued in every adapter ([#556](https://github.com/lusoris/SnatchArr/issues/556)) ([1620357](https://github.com/lusoris/SnatchArr/commit/1620357594247ccdb976c38df24b2aa9529a0102))
* **licences:** read the module cache from go env ([#555](https://github.com/lusoris/SnatchArr/issues/555)) ([27ab7dc](https://github.com/lusoris/SnatchArr/commit/27ab7dcd4176ee4bc211664c45eb891fa9e40739))


### Refactoring

* **web:** share admin form edit state through FormEditor ([#557](https://github.com/lusoris/SnatchArr/issues/557)) ([7702f7a](https://github.com/lusoris/SnatchArr/commit/7702f7af23fae5f9a9739685f9701247ada3e16f))
* **web:** share the admin-only link form slot across three pages ([#546](https://github.com/lusoris/SnatchArr/issues/546)) ([d22fcf1](https://github.com/lusoris/SnatchArr/commit/d22fcf1fe21816288467d6726999539641239fa9))
* **web:** share the new-password field and the account test steps ([#548](https://github.com/lusoris/SnatchArr/issues/548)) ([f36ecc9](https://github.com/lusoris/SnatchArr/commit/f36ecc9c795c5abeb519feef1825e45570cc4979))
* **web:** share the policy section prop types and kind check ([#547](https://github.com/lusoris/SnatchArr/issues/547)) ([5dbdf4b](https://github.com/lusoris/SnatchArr/commit/5dbdf4bfb3ada35da27a03ccc888f6e4371f83ba))
* **worker:** share the wanted-cycle and dispatched-id test helpers ([#545](https://github.com/lusoris/SnatchArr/issues/545)) ([b733fa8](https://github.com/lusoris/SnatchArr/commit/b733fa85c9d9891ae4f5525aad54e5f5b8cb08a2))

## [0.6.0](https://github.com/lusoris/SnatchArr/compare/v0.5.5...v0.6.0) (2026-10-23)


### ⚠ BREAKING CHANGES

* **api:** serve /metrics on a port of its own ([#538](https://github.com/lusoris/SnatchArr/issues/538))
* **sso:** require a shared secret from the trusted proxy ([#535](https://github.com/lusoris/SnatchArr/issues/535))

### Features

* **api:** serve /metrics on a port of its own ([#538](https://github.com/lusoris/SnatchArr/issues/538)) ([b5d505e](https://github.com/lusoris/SnatchArr/commit/b5d505e1835ab23e3ce693e11d979aa38437564e))


### Bug Fixes

* **deps:** take golusoris v0.20.0 and its exported ogenkit problem resolver ([#533](https://github.com/lusoris/SnatchArr/issues/533)) ([59df0a0](https://github.com/lusoris/SnatchArr/commit/59df0a083e65b21029b795eb2298bd6f83822dbd))
* **httpapi:** answer 422 instead of 400 for request bodies that break an OpenAPI schema bound ([#533](https://github.com/lusoris/SnatchArr/issues/533)) ([59df0a0](https://github.com/lusoris/SnatchArr/commit/59df0a083e65b21029b795eb2298bd6f83822dbd))
* **sso:** require a shared secret from the trusted proxy ([#535](https://github.com/lusoris/SnatchArr/issues/535)) ([d757d9b](https://github.com/lusoris/SnatchArr/commit/d757d9bb2705df03b8846d5c3e31ed8dd636da81))
* **web:** take dompurify 3.4.17 for two new advisories ([#531](https://github.com/lusoris/SnatchArr/issues/531)) ([012fd6b](https://github.com/lusoris/SnatchArr/commit/012fd6bbc8fe417b734e4fa32176e7aea348dd3d))
* **web:** wrap long /live frames and make layout tables role-aware ([#542](https://github.com/lusoris/SnatchArr/issues/542)) ([5708aa2](https://github.com/lusoris/SnatchArr/commit/5708aa2312f4974fdbd85b47a9d7d6db05f16696))


### Refactoring

* **ci:** share repeated workflow steps through composite actions ([#540](https://github.com/lusoris/SnatchArr/issues/540)) ([1b98535](https://github.com/lusoris/SnatchArr/commit/1b985359201d60e528b0b092974ee78d0230b05c))
* **e2e:** share fail, answers, free_port and add_image helpers ([#537](https://github.com/lusoris/SnatchArr/issues/537)) ([8214590](https://github.com/lusoris/SnatchArr/commit/8214590361c6f0fa0e61a7c31e6c937cc0609928))
* **web:** render configarr status through a shared query guard ([#544](https://github.com/lusoris/SnatchArr/issues/544)) ([ddf9ae4](https://github.com/lusoris/SnatchArr/commit/ddf9ae4acc5910e3b7596e1e4cf1076a2189cc30))
* **web:** share the admin create form of notification settings ([#534](https://github.com/lusoris/SnatchArr/issues/534)) ([f31fe0e](https://github.com/lusoris/SnatchArr/commit/f31fe0e62bde2768f9178541a0f1c512d529ee18))
* **web:** share the instance query, tab guard and admin save button ([#541](https://github.com/lusoris/SnatchArr/issues/541)) ([37f269f](https://github.com/lusoris/SnatchArr/commit/37f269ff6cc27cc4c65180de646d7e024ed65052))
* **web:** share the link card and mutation toasts of link pages ([#539](https://github.com/lusoris/SnatchArr/issues/539)) ([e547fe3](https://github.com/lusoris/SnatchArr/commit/e547fe31d514d89d3a8b9c619d35d01b8129b4ee))
* **web:** share the nav group list and locale select of the shell ([#536](https://github.com/lusoris/SnatchArr/issues/536)) ([e626b96](https://github.com/lusoris/SnatchArr/commit/e626b96f56947daafbe435cb6cac4215a4fba549))

## [0.5.5](https://github.com/lusoris/SnatchArr/compare/v0.5.4...v0.5.5) (2026-10-22)


### Features

* **auth:** revoke single sign-on sessions whose role no longer holds ([#509](https://github.com/lusoris/SnatchArr/issues/509)) ([5540732](https://github.com/lusoris/SnatchArr/commit/5540732423858c81a8103ab18a6e93fa2fadf08f))
* **llm:** add the core write tools with generated input schemas ([#521](https://github.com/lusoris/SnatchArr/issues/521)) ([9ee5543](https://github.com/lusoris/SnatchArr/commit/9ee5543eb1a1c5e641f3ffbd3d94d00456106a4f))
* **llm:** add the remaining write tools of the MCP catalog ([#523](https://github.com/lusoris/SnatchArr/issues/523)) ([aa0e458](https://github.com/lusoris/SnatchArr/commit/aa0e458eaf3f75c3d89e1b299f06dc6352a1df3d))
* **llm:** expose every read operation of the catalog as an MCP tool ([#519](https://github.com/lusoris/SnatchArr/issues/519)) ([a812de1](https://github.com/lusoris/SnatchArr/commit/a812de1f9fbd3760eefee27ddd4da5b3ecc32e82))
* **llm:** fence MCP write tools behind an admin opt-in and confirmation ([#520](https://github.com/lusoris/SnatchArr/issues/520)) ([80f8a6a](https://github.com/lusoris/SnatchArr/commit/80f8a6a21b4e3ff866d115d64dbad8b4f7c22b77))


### Bug Fixes

* **deps:** update golusoris ([#512](https://github.com/lusoris/SnatchArr/issues/512)) ([06874a9](https://github.com/lusoris/SnatchArr/commit/06874a95bf83be22707bb04c4c8ab4e6cb82631b))


### Refactoring

* **web:** share the connection status and SSE test stub ([#525](https://github.com/lusoris/SnatchArr/issues/525)) ([02a5eca](https://github.com/lusoris/SnatchArr/commit/02a5eca1e35d5560c55e96a8924eb72de27fa399))
* **web:** share the sign-in card between login and setup ([#527](https://github.com/lusoris/SnatchArr/issues/527)) ([fb14e51](https://github.com/lusoris/SnatchArr/commit/fb14e51a5e86d04d2d85032be90e74ddc878674f))
* **web:** use PageHeader on the dashboard, account and instances pages ([#524](https://github.com/lusoris/SnatchArr/issues/524)) ([96cd84e](https://github.com/lusoris/SnatchArr/commit/96cd84ec7a5312149da60a2387ceb492b1997905))


### Documentation

* **sso:** use the live group names in the Helm example ([#507](https://github.com/lusoris/SnatchArr/issues/507)) ([9ecf0ef](https://github.com/lusoris/SnatchArr/commit/9ecf0efbf67a8b8f9a9b41888f1e280a9ac7109d))

## [0.5.4](https://github.com/lusoris/SnatchArr/compare/v0.5.3...v0.5.4) (2026-10-21)


### Features

* **auth:** map single sign-on groups to admin and viewer roles ([#506](https://github.com/lusoris/SnatchArr/issues/506)) ([ac63990](https://github.com/lusoris/SnatchArr/commit/ac63990639ff45f31e5a5a3186250b9bfc6e366f))
* **priority:** rank instances for leasing and queue sweeps ([#504](https://github.com/lusoris/SnatchArr/issues/504)) ([6a28c54](https://github.com/lusoris/SnatchArr/commit/6a28c54363696b7f3e896cd43b65b20047ded950))
* **queue:** remove torrents stuck fetching magnet metadata ([#503](https://github.com/lusoris/SnatchArr/issues/503)) ([503d990](https://github.com/lusoris/SnatchArr/commit/503d99085b75907aab2003abdea3be7cfc042271))


### Bug Fixes

* **arrclient:** use an own transport instead of http.DefaultTransport ([#496](https://github.com/lusoris/SnatchArr/issues/496)) ([c82cae2](https://github.com/lusoris/SnatchArr/commit/c82cae2baf57fcbf7c8cecfb6066732e0edb2329))
* **deps:** update Go to 1.27.3 for GO-2026-6809 ([#494](https://github.com/lusoris/SnatchArr/issues/494)) ([9ad7a16](https://github.com/lusoris/SnatchArr/commit/9ad7a160fb91362df64b6f1467bd60a06fc9a012))
* **queue:** retry refused id-matched imports and keep their reasons ([#501](https://github.com/lusoris/SnatchArr/issues/501)) ([077f8e9](https://github.com/lusoris/SnatchArr/commit/077f8e9c10ae04520710f8915c90ab4e07b1ddf2))
* **web:** give every History row the header's column template ([#499](https://github.com/lusoris/SnatchArr/issues/499)) ([4534443](https://github.com/lusoris/SnatchArr/commit/4534443198754e34852947b7663361ebfecec141))


### Documentation

* **adr:** map single sign-on groups to roles in ADR-0023 ([#500](https://github.com/lusoris/SnatchArr/issues/500)) ([2dd9ed3](https://github.com/lusoris/SnatchArr/commit/2dd9ed392db99a3d94f95768cf3519e66c87ea79))

## [0.5.3](https://github.com/lusoris/SnatchArr/compare/v0.5.2...v0.5.3) (2026-10-20)


### Features

* **policy:** balanced snatch defaults and presets (ADR-0020) ([#485](https://github.com/lusoris/SnatchArr/issues/485)) ([c0a7df2](https://github.com/lusoris/SnatchArr/commit/c0a7df21b1d70d6c957ceea80880535a4bb942b0))
* **web:** regroup the snatch policy form by task with presets ([#486](https://github.com/lusoris/SnatchArr/issues/486)) ([86f4208](https://github.com/lusoris/SnatchArr/commit/86f4208af4d0f7aa96a49a0fe948c1f96abe2df3))


### Bug Fixes

* **deps:** update rustls-webpki and webpki-root-certs for two advisories ([#487](https://github.com/lusoris/SnatchArr/issues/487)) ([c5cd991](https://github.com/lusoris/SnatchArr/commit/c5cd991b930ed3b3661be3795cbad8dcd523e08e))
* **live:** keep the event stream open and fill the live page ([#488](https://github.com/lusoris/SnatchArr/issues/488)) ([650875b](https://github.com/lusoris/SnatchArr/commit/650875b2d4d7e83d0bf72be78c37cf3829283bcc))


### Documentation

* **adr:** ADR-0019 foreign-title search without proxy indexers ([#484](https://github.com/lusoris/SnatchArr/issues/484)) ([ade31e6](https://github.com/lusoris/SnatchArr/commit/ade31e612f805f1336371dfa5ae0a9e486418b64))
* **adr:** ADR-0021 LLM access module ([#489](https://github.com/lusoris/SnatchArr/issues/489)) ([c7ee0b3](https://github.com/lusoris/SnatchArr/commit/c7ee0b31558997e867bc11022a61bff8df111952))
* **adr:** link README by URL so mkdocs --strict resolves it ([#482](https://github.com/lusoris/SnatchArr/issues/482)) ([acad5fc](https://github.com/lusoris/SnatchArr/commit/acad5fc5d95180f3ab39be8c132dbcf2041b6ab5))

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
