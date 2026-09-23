# Changelog

## [0.7.0](https://github.com/y3owk1n/oku/compare/v0.6.1...v0.7.0) (2026-09-23)


### Features

* **infer:** expose every program named after the package, and keep --bin on update ([#220](https://github.com/y3owk1n/oku/issues/220)) ([ce1e28e](https://github.com/y3owk1n/oku/commit/ce1e28e6e7bce17a04c19c3fa8a26f7fa73514ab))
* **lock:** let packages fit some platforms without blocking ([#215](https://github.com/y3owk1n/oku/issues/215)) ([edd7e9f](https://github.com/y3owk1n/oku/commit/edd7e9f3b718173e592f5fbc253ad7d503bda3dc))
* **manifest:** let a service and a build step take an array of when tables ([#217](https://github.com/y3owk1n/oku/issues/217)) ([6522094](https://github.com/y3owk1n/oku/commit/65220945b742272827fc95e91fdd02d3b3da45a7))
* **store:** keep build tools off a program's PATH and put its runtime there ([#218](https://github.com/y3owk1n/oku/issues/218)) ([a3e4ddb](https://github.com/y3owk1n/oku/commit/a3e4ddbb0a78d182d957edc41f5ed19546f679d8))


### Bug Fixes

* **infer:** skip checksum files that disagree with GitHub and read AppImage as Linux ([#216](https://github.com/y3owk1n/oku/issues/216)) ([ef76941](https://github.com/y3owk1n/oku/commit/ef769411b110b334c74411374a9c83cda8b7dddd))
* **manifest:** drop signature_url, which lint accepted and nothing read ([#221](https://github.com/y3owk1n/oku/issues/221)) ([b0e700e](https://github.com/y3owk1n/oku/commit/b0e700ec4848633756f2fca886d1ff2b57079516))
* **store:** run pwsh steps in Windows PowerShell when PowerShell 7 is missing ([#212](https://github.com/y3owk1n/oku/issues/212)) ([53c6531](https://github.com/y3owk1n/oku/commit/53c65312ddf8cf1386c850480d9a0687868ef066))


### Documentation

* drop unnecessary text from guides ([#224](https://github.com/y3owk1n/oku/issues/224)) ([056a42f](https://github.com/y3owk1n/oku/commit/056a42f6dece4b63c7378d82efa78255f1e42691))
* **examples:** add manifests for the node, python, go and rust runtimes ([#219](https://github.com/y3owk1n/oku/issues/219)) ([f608ff4](https://github.com/y3owk1n/oku/commit/f608ff41796aa5c1f9f466160bbd75acd6a4b729))
* rewrite the docs into a tutorial, guides, concepts and reference ([#223](https://github.com/y3owk1n/oku/issues/223)) ([3b585f1](https://github.com/y3owk1n/oku/commit/3b585f1e722d64d5c82cc799ce8fb8dbdb79a07a))

## [0.6.1](https://github.com/y3owk1n/oku/compare/v0.6.0...v0.6.1) (2026-09-23)


### Bug Fixes

* **action:** install the release of the tag the action is used by ([#208](https://github.com/y3owk1n/oku/issues/208)) ([e24e647](https://github.com/y3owk1n/oku/commit/e24e647822ed1a5a1d7bea86f12e6d98356ffc78))

## [0.6.0](https://github.com/y3owk1n/oku/compare/v0.5.0...v0.6.0) (2026-09-23)


### Features

* add a GitHub Action that syncs an oku list and puts its tools on PATH ([#187](https://github.com/y3owk1n/oku/issues/187)) ([b1bd5dc](https://github.com/y3owk1n/oku/commit/b1bd5dc579280ef4a6ebe2863401574c6ec8ede8))
* **add:** infer manifests from installers and macOS app bundles ([#161](https://github.com/y3owk1n/oku/issues/161)) ([3b35fff](https://github.com/y3owk1n/oku/commit/3b35fffc1972590b2ae4d9edbae2ba76189dc538))
* build cargo: refs on Windows ([#189](https://github.com/y3owk1n/oku/issues/189)) ([e470550](https://github.com/y3owk1n/oku/commit/e470550ec272faae016aba3ecd3912b38dafda78))
* build Go programs with go: refs, the way go install does ([#184](https://github.com/y3owk1n/oku/issues/184)) ([8e2a7df](https://github.com/y3owk1n/oku/commit/8e2a7df7667bef0f8b5e13cb7bf9a21fad8135f0))
* build go: refs on Windows ([#188](https://github.com/y3owk1n/oku/issues/188)) ([5808372](https://github.com/y3owk1n/oku/commit/580837276ae2f259dcd381f5b7e2646d264e96e2))
* build Rust crates from crates.io with cargo: refs ([#185](https://github.com/y3owk1n/oku/issues/185)) ([e5a2e14](https://github.com/y3owk1n/oku/commit/e5a2e140bc034927d318930a022dabbe1e822bca))
* **cli:** list packages with a newer version in oku outdated ([#186](https://github.com/y3owk1n/oku/issues/186)) ([37876fd](https://github.com/y3owk1n/oku/commit/37876fd1cbcf526b323411450fd2e4faa8cb4b7b))
* **cli:** report files and settings in sync, generations, rollback and list ([#164](https://github.com/y3owk1n/oku/issues/164)) ([b5bdf07](https://github.com/y3owk1n/oku/commit/b5bdf0736d618c91888195b16e1eebf90287c8a1))
* **cli:** run a command with the directory's programs in oku exec ([#199](https://github.com/y3owk1n/oku/issues/199)) ([5ca202d](https://github.com/y3owk1n/oku/commit/5ca202dff7328791f936b9ff1be792467e89abe1))
* **cli:** show the latest release in oku outdated ([#205](https://github.com/y3owk1n/oku/issues/205)) ([0b58070](https://github.com/y3owk1n/oku/commit/0b5807094c066c482286da8cd3e4c7bd84bf4641))
* **cli:** take a version constraint for a runtime ([#193](https://github.com/y3owk1n/oku/issues/193)) ([020effa](https://github.com/y3owk1n/oku/commit/020effa9883885c7084d2a66a89192f7a8741bdb))
* **cli:** take a version range or a prefix for a package ([#197](https://github.com/y3owk1n/oku/issues/197)) ([9e2f4e9](https://github.com/y3owk1n/oku/commit/9e2f4e9d2b2bf9d779702efaec5cdb2f343ad745))
* **completions:** generate completions by running the package ([#155](https://github.com/y3owk1n/oku/issues/155)) ([1ae3874](https://github.com/y3owk1n/oku/commit/1ae387420d0a233ad9c708e6bc88433fbe22b79a))
* **forge:** use the gh CLI's login when GITHUB_TOKEN is not set ([#181](https://github.com/y3owk1n/oku/issues/181)) ([1559317](https://github.com/y3owk1n/oku/commit/1559317dd7643c172332d8aa72264a8d6eea78c3))
* **hook:** load package and oku completions from the shell hook ([#158](https://github.com/y3owk1n/oku/issues/158)) ([0fe3013](https://github.com/y3owk1n/oku/commit/0fe3013e5dda75abbd4b7c245516b80d51c06825))
* install npm packages with dependencies on Windows ([#191](https://github.com/y3owk1n/oku/issues/191)) ([b445f03](https://github.com/y3owk1n/oku/commit/b445f03546b41c07a965c8be879e9486b4c4e18d))
* install pypi: packages on Windows ([#190](https://github.com/y3owk1n/oku/issues/190)) ([abac714](https://github.com/y3owk1n/oku/commit/abac714de4d036eea871fb4d37aa4b429df56219))
* install Python tools from PyPI with pypi: refs ([#183](https://github.com/y3owk1n/oku/issues/183)) ([c2bad74](https://github.com/y3owk1n/oku/commit/c2bad745410ac902c55b8d1cd2e9cc89dd3d7ed2))
* **install:** print the version, detect the hook line and list the next steps ([#159](https://github.com/y3owk1n/oku/issues/159)) ([d809d04](https://github.com/y3owk1n/oku/commit/d809d0438354fd02e3c6cfb61dcd8eb601c48e68))
* **lists:** name the node of npm packages in the list ([#182](https://github.com/y3owk1n/oku/issues/182)) ([fd3d2ec](https://github.com/y3owk1n/oku/commit/fd3d2ec9a6bf430e36be8af3613ce79797b4b399))
* **lists:** place the files and secrets of a list from a repo ([#179](https://github.com/y3owk1n/oku/issues/179)) ([ba3106c](https://github.com/y3owk1n/oku/commit/ba3106cc3d4d7f8e1b1da0582a81509c3e424f24))
* **lists:** resolve a relative path in a remote list inside the same repo ([#178](https://github.com/y3owk1n/oku/issues/178)) ([7d72bf8](https://github.com/y3owk1n/oku/commit/7d72bf82b0cf1a829466d73fc59d8bd6af27287c))
* **lock:** without [lock] platforms every command works for the host alone ([#162](https://github.com/y3owk1n/oku/issues/162)) ([d8c8095](https://github.com/y3owk1n/oku/commit/d8c8095cc4e8addbeafa9734914cb25a58fe97b8))
* **resolve:** check a release file against the digest GitHub reports ([#202](https://github.com/y3owk1n/oku/issues/202)) ([826be9a](https://github.com/y3owk1n/oku/commit/826be9adfdea09d919290cdddea383a145fed97a))
* **self:** keep a nightly build, add --release and --to, link the release notes ([#160](https://github.com/y3owk1n/oku/issues/160)) ([14f8a3f](https://github.com/y3owk1n/oku/commit/14f8a3f7b994a18b91857ee3e52cfa0d7afbd2a5))
* **store:** check a fetch step against a published checksum file ([#204](https://github.com/y3owk1n/oku/issues/204)) ([780eb07](https://github.com/y3owk1n/oku/commit/780eb07a7f26bc3c28d549016d94dc09bface7e2))
* **ui:** a check on every finished line and a done line to close ([#167](https://github.com/y3owk1n/oku/issues/167)) ([e86e5d9](https://github.com/y3owk1n/oku/commit/e86e5d9f457edceae1098761926f5c849f9f6f39))
* **ui:** fit tables to the terminal, print sync rows as they finish, collapse the approval prompt ([#165](https://github.com/y3owk1n/oku/issues/165)) ([73d182d](https://github.com/y3owk1n/oku/commit/73d182d80bd56dfbbad625a41224f64fdc0785fd))


### Bug Fixes

* **add:** limit a one-OS inferred package with when, and say what to type on failure ([#166](https://github.com/y3owk1n/oku/issues/166)) ([1def6a7](https://github.com/y3owk1n/oku/commit/1def6a7456a919f25d52ed87a8f5db4f1b8f00a1))
* **add:** list every asset that fits the host as an alternative ([#163](https://github.com/y3owk1n/oku/issues/163)) ([923fd4c](https://github.com/y3owk1n/oku/commit/923fd4c8d64f8eaf7790021762bf3cadd8fea851))
* check archive links on the unpacked tree and self update's release ([#175](https://github.com/y3owk1n/oku/issues/175)) ([0c5be05](https://github.com/y3owk1n/oku/commit/0c5be055bdaf6de927225011c1040992f995317c))
* **cli:** check a build source named by {{tag}} against GitHub's digest ([#203](https://github.com/y3owk1n/oku/issues/203)) ([1328caf](https://github.com/y3owk1n/oku/commit/1328cafe56baaee665d139111d815c3a7c7870ff))
* **cli:** clearer messages, commands in colour, oku add takes several refs ([#170](https://github.com/y3owk1n/oku/issues/170)) ([dbbc8e1](https://github.com/y3owk1n/oku/commit/dbbc8e1107da5a07e9b81a179a4f18b87621735d))
* **cli:** name a project's runtime relative to the project in its lock ([#192](https://github.com/y3owk1n/oku/issues/192)) ([06eee91](https://github.com/y3owk1n/oku/commit/06eee91284f22c0c5a9945a380ee6e2ab1b21d89))
* **cli:** read a bare version in a dep or a runtime as a prefix ([#201](https://github.com/y3owk1n/oku/issues/201)) ([a8021e3](https://github.com/y3owk1n/oku/commit/a8021e3a9ef01e7878952f712bd9f17b3051084f))
* **forge:** ask GitHub whether an answer changed, and read 100 releases a page ([#177](https://github.com/y3owk1n/oku/issues/177)) ([9f90021](https://github.com/y3owk1n/oku/commit/9f900213287cda3355511ca0256d36c214b40da3))
* **gc:** keep the old build that a killed rebuild left ([#174](https://github.com/y3owk1n/oku/issues/174)) ([7f1c7ed](https://github.com/y3owk1n/oku/commit/7f1c7eda91b6e6aaf09118ff135ca1f11468f442))
* **generations:** compare each generation with the one it replaced ([#171](https://github.com/y3owk1n/oku/issues/171)) ([e76228b](https://github.com/y3owk1n/oku/commit/e76228b7e704512f2035d7092b06c5bb366bcb1d))
* let one oku process change the machine at a time ([#173](https://github.com/y3owk1n/oku/issues/173)) ([b9a5118](https://github.com/y3owk1n/oku/commit/b9a5118e97626c9629c2f6255f81b55c4a6a66f5))
* **manifest:** ignore line endings in a manifest's digest ([#195](https://github.com/y3owk1n/oku/issues/195)) ([853ee39](https://github.com/y3owk1n/oku/commit/853ee39b08f72d72eed5314196617498c5a8cd73))
* **resolve:** read a tag with or without a v as a version ([#198](https://github.com/y3owk1n/oku/issues/198)) ([0d1a5df](https://github.com/y3owk1n/oku/commit/0d1a5df8717336d9b30d43ecd48694997a6e1299))
* **sandbox:** make the filesystem read-only and hide the session on Linux ([#176](https://github.com/y3owk1n/oku/issues/176)) ([3fde7c8](https://github.com/y3owk1n/oku/commit/3fde7c8c228ac83632e3bd3edd62df36310566a4))
* **shellhook:** move oku's directories to the front of PATH ([#200](https://github.com/y3owk1n/oku/issues/200)) ([d7b3808](https://github.com/y3owk1n/oku/commit/d7b3808ec8c96b4deeaeaad1d7d4612c0930021b))
* **status:** hold the other packages' output while oku asks a question ([#168](https://github.com/y3owk1n/oku/issues/168)) ([31c6083](https://github.com/y3owk1n/oku/commit/31c6083d255af9ccc498f16b989de3e85ebe09a6))
* **store:** keep a download that another package of the sync finished ([#196](https://github.com/y3owk1n/oku/issues/196)) ([f6b7433](https://github.com/y3owk1n/oku/commit/f6b7433d690fb5978f2a8e92baa77b6970ec92ad))
* **sync:** name every drifted package in one update command ([#172](https://github.com/y3owk1n/oku/issues/172)) ([8b72b32](https://github.com/y3owk1n/oku/commit/8b72b32692b9a91d5ae5be620d035527d643447b))
* **ui:** fit tables, wraps and help to the terminal, one wait row per package ([#169](https://github.com/y3owk1n/oku/issues/169)) ([1c00dcd](https://github.com/y3owk1n/oku/commit/1c00dcdb3e68d05550dd600305ddaf2fdf104a15))


### Documentation

* bring the README up to date, with real oku add examples ([#207](https://github.com/y3owk1n/oku/issues/207)) ([115886d](https://github.com/y3owk1n/oku/commit/115886d8166aed6cee4a066f9c6ea0a95a13ccb2))
* correct the journeys and the Windows skip of the CLI tests ([#180](https://github.com/y3owk1n/oku/issues/180)) ([c293821](https://github.com/y3owk1n/oku/commit/c293821fa8bf69c2356b08f776ad59724a060bdf))
* **prd:** record D72, completions generated from the download ([#157](https://github.com/y3owk1n/oku/issues/157)) ([41b8bf3](https://github.com/y3owk1n/oku/commit/41b8bf3c9e72dfac5d6588245dcb4ae97007b930))

## [0.5.0](https://github.com/y3owk1n/oku/compare/v0.4.0...v0.5.0) (2026-09-22)


### Features

* **build:** warn when a built file loads a store package outside runtime.deps ([#147](https://github.com/y3owk1n/oku/issues/147)) ([a961bb1](https://github.com/y3owk1n/oku/commit/a961bb1420ff4213d89b0d80e79e4f3e91670844))
* **cli:** colour, tables and clearer messages on a terminal ([#143](https://github.com/y3owk1n/oku/issues/143)) ([4e12f4e](https://github.com/y3owk1n/oku/commit/4e12f4eeac69f368e153d07fcfea716acc375834))
* **cli:** show every installing package, and say more after a change ([#144](https://github.com/y3owk1n/oku/issues/144)) ([b6161fd](https://github.com/y3owk1n/oku/commit/b6161fd465b9935075830fde2aed4d5dfcc8782e))
* **files:** override [vars] per file entry with a vars table ([#141](https://github.com/y3owk1n/oku/issues/141)) ([5eb3da6](https://github.com/y3owk1n/oku/commit/5eb3da60fb7753c648994ac7e3da20818cc64fc2))
* **lock:** pin a build for another platform with all that a build writes ([#136](https://github.com/y3owk1n/oku/issues/136)) ([2ee32de](https://github.com/y3owk1n/oku/commit/2ee32dec29f4e17deb237b121a95a748a3692b67))
* **lock:** pin a package that the host does not install ([#131](https://github.com/y3owk1n/oku/issues/131)) ([63dd455](https://github.com/y3owk1n/oku/commit/63dd45567e01c1aae94a3ef73eec4fb427507ded))
* **lock:** pin other platforms when oku writes the lock ([#128](https://github.com/y3owk1n/oku/issues/128)) ([4fe89ac](https://github.com/y3owk1n/oku/commit/4fe89ac216c71870e009ccb30fa7a55d7e849c78))
* **lock:** pin the npm packages of another platform from this one ([#138](https://github.com/y3owk1n/oku/issues/138)) ([43bed76](https://github.com/y3owk1n/oku/commit/43bed76de9282c1257b7e2e7908252f8c656eacf))
* **manifest:** let a download that is the program itself have a wrapper ([#135](https://github.com/y3owk1n/oku/issues/135)) ([2d9256c](https://github.com/y3owk1n/oku/commit/2d9256c4c635b70ffa3a0b044b0dd5b096c90027))
* **manifest:** match ** in font and man patterns ([#146](https://github.com/y3owk1n/oku/issues/146)) ([db7d6e0](https://github.com/y3owk1n/oku/commit/db7d6e07a1b9c494e0e8bf2dfc448bb61845a4db))
* **npm:** run the install scripts a manifest names, and lint every artifact ([#152](https://github.com/y3owk1n/oku/issues/152)) ([c178c2d](https://github.com/y3owk1n/oku/commit/c178c2dd3139ad19c297b10fcc89f6fc99182dbf))
* **service:** limit a service to some platforms with when ([#133](https://github.com/y3owk1n/oku/issues/133)) ([0a9a8dc](https://github.com/y3owk1n/oku/commit/0a9a8dc898443fa6d15b709ea88da8c7541d1ab1))
* **sync:** build a package again with --rebuild ([#140](https://github.com/y3owk1n/oku/issues/140)) ([abbd2b2](https://github.com/y3owk1n/oku/commit/abbd2b260073da95a2b88409f732d1fe88245c77))
* **sync:** fail with --locked when oku.lock would change ([#130](https://github.com/y3owk1n/oku/issues/130)) ([d61f2ee](https://github.com/y3owk1n/oku/commit/d61f2eea213b94db56ec4b33c4794fbf5eba6556))


### Bug Fixes

* **build:** link man/, isolate needs tools, add bin path, sbin and xcrun cache ([#151](https://github.com/y3owk1n/oku/issues/151)) ([61c1868](https://github.com/y3owk1n/oku/commit/61c186859e6b3593716998dc0a1008a9ed38d3f6))
* **cli:** let which find a global program inside a project ([#153](https://github.com/y3owk1n/oku/issues/153)) ([3efc702](https://github.com/y3owk1n/oku/commit/3efc702f0c700dee63b89acbf8ee61c1b91c2d7a))
* **files:** keep entries of one target that differ by when across lists ([#142](https://github.com/y3owk1n/oku/issues/142)) ([659e104](https://github.com/y3owk1n/oku/commit/659e104b48e08904afbf5606086621f042eb1631))
* **files:** private directories for private files, and a mode change for a secret ([#150](https://github.com/y3owk1n/oku/issues/150)) ([449e62e](https://github.com/y3owk1n/oku/commit/449e62e6dd4e9ce5dd477f093a5dd968bcf0ceab))
* **infer:** pick the asset and checksums that fit, and read more releases ([#145](https://github.com/y3owk1n/oku/issues/145)) ([e4e8a0d](https://github.com/y3owk1n/oku/commit/e4e8a0d81523df26934d2bacedf304d2e3554899))
* **lock:** complete a build entry that lacks a pin ([#139](https://github.com/y3owk1n/oku/issues/139)) ([cc4891a](https://github.com/y3owk1n/oku/commit/cc4891a9e37b7ca379eb011d6f202abd1b9dbb2d))
* **lock:** keep the pins of a build that the store already holds ([#137](https://github.com/y3owk1n/oku/issues/137)) ([2aa3dbe](https://github.com/y3owk1n/oku/commit/2aa3dbe8bf6de335d080585b74f61fd84efa2b3b))
* **npm:** keep the path of this machine out of the lock of an npm package ([#132](https://github.com/y3owk1n/oku/issues/132)) ([827ae87](https://github.com/y3owk1n/oku/commit/827ae871466af23653072da3fdb2496031cc5f5a))
* **service:** report a service that exits right after start ([#148](https://github.com/y3owk1n/oku/issues/148)) ([1eb25b6](https://github.com/y3owk1n/oku/commit/1eb25b69ffdf19bb68f091805dece27ed78de8d4))
* **service:** skip services on a Linux machine that systemd does not run ([#134](https://github.com/y3owk1n/oku/issues/134)) ([934f07a](https://github.com/y3owk1n/oku/commit/934f07a95681999770519d299abbc83983e56f56))

## [0.4.0](https://github.com/y3owk1n/oku/compare/v0.3.0...v0.4.0) (2026-09-21)


### Features

* **build:** give pkg-config the system libraries of macOS ([#117](https://github.com/y3owk1n/oku/issues/117)) ([75e69ba](https://github.com/y3owk1n/oku/commit/75e69ba72409da37e9ba131830a0510115b92fc2))
* **build:** install an npm package with its dependencies, dated ([#95](https://github.com/y3owk1n/oku/issues/95)) ([13cb343](https://github.com/y3owk1n/oku/commit/13cb3438d0f95f17cd2d7bd0ef66fcb1bc09546f))
* **build:** pin a source archive on first download, or read sha256_url ([#116](https://github.com/y3owk1n/oku/issues/116)) ([90074bb](https://github.com/y3owk1n/oku/commit/90074bb97e53165081906aa1776d877c5f775a1c))
* **cli:** say what oku waits for, with bytes and time on a terminal ([#120](https://github.com/y3owk1n/oku/issues/120)) ([961f0ed](https://github.com/y3owk1n/oku/commit/961f0ed699d8070ed013e2ff21771b57cd95f484))
* **files:** place files in the home directory from the list ([#98](https://github.com/y3owk1n/oku/issues/98)) ([efdd5fd](https://github.com/y3owk1n/oku/commit/efdd5fd6491cc1c7cb87a95e478a536a698756a2))
* **files:** place files on Windows with junctions and copies ([#99](https://github.com/y3owk1n/oku/issues/99)) ([fb85a94](https://github.com/y3owk1n/oku/commit/fb85a94565afc7de9d4294206de80050ec38299a))
* **files:** render files from templates and the list's variables ([#100](https://github.com/y3owk1n/oku/issues/100)) ([666a7e0](https://github.com/y3owk1n/oku/commit/666a7e00811c692a45f7f18dca096a0066a0b363))
* **forge:** install from Codeberg and any Gitea or Forgejo server ([#82](https://github.com/y3owk1n/oku/issues/82)) ([301afb8](https://github.com/y3owk1n/oku/commit/301afb825411515c5e70a7d74637723991e49f26))
* **forge:** install from gitlab.com and any GitLab server ([#83](https://github.com/y3owk1n/oku/issues/83)) ([59d7bc1](https://github.com/y3owk1n/oku/commit/59d7bc1b005a8fcae24786bb436cfbc2e50996a9))
* **forge:** read GitHub through one forge interface, and accept github:host/owner/repo ([#81](https://github.com/y3owk1n/oku/issues/81)) ([6e0e37b](https://github.com/y3owk1n/oku/commit/6e0e37b512b8a50f722e9f0b7131d5bd750d2131))
* **infer:** install a command-line tool from the npm registry with npm:name ([#93](https://github.com/y3owk1n/oku/issues/93)) ([e269946](https://github.com/y3owk1n/oku/commit/e2699460a640dc02fda8cd1a921aefef7852ece8))
* **infer:** install from a URL that is the download itself ([#84](https://github.com/y3owk1n/oku/issues/84)) ([7fb5b3f](https://github.com/y3owk1n/oku/commit/7fb5b3f23ddcf4714aa6d17355e8deecfe398140))
* **infer:** read more release layouts, and add --asset and --bin to oku add ([#79](https://github.com/y3owk1n/oku/issues/79)) ([7ddb68a](https://github.com/y3owk1n/oku/commit/7ddb68acdc5071aece86ae027c1a1fab04b834fa))
* **manifest:** add "oku manifest hash" to print a download's checksums ([#92](https://github.com/y3owk1n/oku/issues/92)) ([ebb12e9](https://github.com/y3owk1n/oku/commit/ebb12e976502063279ed962ed2ada6bc80fb42de))
* **manifest:** bump a manifest whose releases are not on github.com ([#88](https://github.com/y3owk1n/oku/issues/88)) ([b1bb927](https://github.com/y3owk1n/oku/commit/b1bb927d97af383303b8d3429d55b2c25515efaf))
* **manifest:** follow the newest commit of a branch with version.from = "git-branch" ([#125](https://github.com/y3owk1n/oku/issues/125)) ([2b4eacb](https://github.com/y3owk1n/oku/commit/2b4eacb0aaf390db7fb7c5d59f3846cf2d99d7b9))
* **manifest:** let a bin entry be a program that oku writes ([#90](https://github.com/y3owk1n/oku/issues/90)) ([93d3dcb](https://github.com/y3owk1n/oku/commit/93d3dcbc52cd2f6d82d331ff6e9bd5a86ae084dd))
* **manifest:** let a font entry be a pattern ([#119](https://github.com/y3owk1n/oku/issues/119)) ([d29d072](https://github.com/y3owk1n/oku/commit/d29d07227081ef80e4cd51e01ee062b7d08d646c))
* **manifest:** let a package hold only files, with data = true ([#108](https://github.com/y3owk1n/oku/issues/108)) ([e1ae0f5](https://github.com/y3owk1n/oku/commit/e1ae0f593c6617d33c9f967bd3c55910a046bb3a))
* **manifest:** let a prebuilt download fill lib, include and share ([#115](https://github.com/y3owk1n/oku/issues/115)) ([5959390](https://github.com/y3owk1n/oku/commit/5959390a7a3880ff2851aa9eaa9098787f96a5df))
* **secrets:** place secrets from sops and age files ([#106](https://github.com/y3owk1n/oku/issues/106)) ([2c009b1](https://github.com/y3owk1n/oku/commit/2c009b1286eaad715a033b23382f172ce4ae3a93))
* **service:** put the profile on a service's PATH, and expand locations ([#113](https://github.com/y3owk1n/oku/issues/113)) ([e8330cc](https://github.com/y3owk1n/oku/commit/e8330cc3c52d7d93996eac35216f701537cfeb7a))
* **settings:** restart the Dock when one of its settings changed ([#102](https://github.com/y3owk1n/oku/issues/102)) ([9d5879e](https://github.com/y3owk1n/oku/commit/9d5879e7f70fbe86202b09a272849335090f0a09))
* **settings:** set dconf keys on Linux with [dconf] ([#104](https://github.com/y3owk1n/oku/issues/104)) ([25af82d](https://github.com/y3owk1n/oku/commit/25af82dcaf1008cf0a901a64ef1368ef799d9273))
* **settings:** set macOS preferences from the list with [defaults] ([#101](https://github.com/y3owk1n/oku/issues/101)) ([516a5ed](https://github.com/y3owk1n/oku/commit/516a5ed3e4261837f1513a8b57475aa04c44d300))
* **settings:** set macOS preferences of this one Mac ([#110](https://github.com/y3owk1n/oku/issues/110)) ([2df27bf](https://github.com/y3owk1n/oku/commit/2df27bf11e0d2672e603917e9127c3475808a327))
* **settings:** set registry values on Windows with [registry] ([#103](https://github.com/y3owk1n/oku/issues/103)) ([43d7557](https://github.com/y3owk1n/oku/commit/43d7557d4f0fde1253f2223a8267cfe00fefc7da))
* **sync:** add --dry-run to sync and update ([#109](https://github.com/y3owk1n/oku/issues/109)) ([cb279c1](https://github.com/y3owk1n/oku/commit/cb279c1d4dfae03a363cc2f72a88b47380fa43ee))
* **sync:** reuse downloads after a stopped run, share dep lookups, quiet the inferred manifest ([#122](https://github.com/y3owk1n/oku/issues/122)) ([943038c](https://github.com/y3owk1n/oku/commit/943038ca07213835aea741a0833b6c4434ff235e))
* **sync:** undo a change that fails partway, and check before the first step ([#97](https://github.com/y3owk1n/oku/issues/97)) ([4ece0bb](https://github.com/y3owk1n/oku/commit/4ece0bb6d2e415b7c1b982fc0731ce79a5f36c08))
* **version:** follow the versions of an npm package, and check its sha512 ([#91](https://github.com/y3owk1n/oku/issues/91)) ([e4ba9ba](https://github.com/y3owk1n/oku/commit/e4ba9ba2f704440680534eb5445bda863888321d))


### Bug Fixes

* **env:** add {{pkg}} so an artifact's [env] can name its own files ([#94](https://github.com/y3owk1n/oku/issues/94)) ([e550a99](https://github.com/y3owk1n/oku/commit/e550a997df5d3aed425c282250530ae389b26d9a))
* **forge:** list the releases of a repo whose releases have many assets ([#85](https://github.com/y3owk1n/oku/issues/85)) ([5ce2009](https://github.com/y3owk1n/oku/commit/5ce2009575ec443b437897aab6bcd8f068519ad6))
* **infer:** pick the command line build and the checksums of its own OS ([#118](https://github.com/y3owk1n/oku/issues/118)) ([7c2158b](https://github.com/y3owk1n/oku/commit/7c2158b0e731436fb8dfa258951a1b079a9e9584))
* **npm:** read a relative runtimes.node from the config directory ([#114](https://github.com/y3owk1n/oku/issues/114)) ([b4abf9a](https://github.com/y3owk1n/oku/commit/b4abf9a15aee9265d3a1a66cc0d830c41e869409))
* **resolve:** never pick a prerelease such as 1.27rc1 as the newest version ([#123](https://github.com/y3owk1n/oku/issues/123)) ([5c25ca9](https://github.com/y3owk1n/oku/commit/5c25ca915ae0fe3f3057bce7900b0d0868357ba0))
* **self:** download the nightly build again when its url served an older one ([#126](https://github.com/y3owk1n/oku/issues/126)) ([30bce21](https://github.com/y3owk1n/oku/commit/30bce2149ce9b2580f21aeb0861dcc19dc4a58bc))
* **service:** wait for the old program to exit before loading a service again ([#124](https://github.com/y3owk1n/oku/issues/124)) ([c96c5ed](https://github.com/y3owk1n/oku/commit/c96c5ede08684fc5bc7002f5491e8326007314b3))
* **store:** keep the file times of an archive when unpacking it ([#111](https://github.com/y3owk1n/oku/issues/111)) ([8d0902a](https://github.com/y3owk1n/oku/commit/8d0902a6871ae090ed241f5799867011c254017d))
* **store:** send the host's token with the downloads of a private repo ([#87](https://github.com/y3owk1n/oku/issues/87)) ([c6eb038](https://github.com/y3owk1n/oku/commit/c6eb038f39c8cb7fceb59ba25c3f4b93f08c008b))
* **sync:** read an included list on this machine as it is ([#107](https://github.com/y3owk1n/oku/issues/107)) ([bf06a7e](https://github.com/y3owk1n/oku/commit/bf06a7e539c3c9ae8db6034083401e2b4648b27f))
* **version:** compare a number in a version suffix as a number ([#112](https://github.com/y3owk1n/oku/issues/112)) ([e384a9a](https://github.com/y3owk1n/oku/commit/e384a9a4046e9807779777339a8c5dedf1509975))


### Performance Improvements

* **sync:** install packages in parallel and skip checks the lock already made ([#127](https://github.com/y3owk1n/oku/issues/127)) ([b469611](https://github.com/y3owk1n/oku/commit/b46961181bf745bce7b25871527f0011dc768030))


### Documentation

* **prd:** add behaviours B112 to B121, each with a test named after it ([#89](https://github.com/y3owk1n/oku/issues/89)) ([8573bb3](https://github.com/y3owk1n/oku/commit/8573bb3ad90083188918ffac70cf29baa6352858))
* **prd:** let the list set up home files and per-user settings ([#96](https://github.com/y3owk1n/oku/issues/96)) ([4e30336](https://github.com/y3owk1n/oku/commit/4e303369b50231a5af1a82f04f542b100753266b))
* **prd:** plan secrets from sops and age files as build step 16 ([#105](https://github.com/y3owk1n/oku/issues/105)) ([c1aa83a](https://github.com/y3owk1n/oku/commit/c1aa83acc440a85f9b371db240ca68a23d784db5))
* **readme:** cover home files, templates, secrets, settings and dry runs ([#121](https://github.com/y3owk1n/oku/issues/121)) ([e95c7ec](https://github.com/y3owk1n/oku/commit/e95c7ec92c83064e1ee8dbef62aee17939813032))

## [0.3.0](https://github.com/y3owk1n/oku/compare/v0.2.2...v0.3.0) (2026-09-20)


### Features

* **self:** support nightly builds for oku itself via install script or self update ([#76](https://github.com/y3owk1n/oku/issues/76)) ([eb98eac](https://github.com/y3owk1n/oku/commit/eb98eac1020c7b5032308a5e7861118ab34d70f4))
* **version:** follow a moving tag such as nightly ([#74](https://github.com/y3owk1n/oku/issues/74)) ([093395c](https://github.com/y3owk1n/oku/commit/093395cdec37b10476e77041bd10807ad5bc48f0))


### Bug Fixes

* **self:** upload the nightly files before the tag moves ([#78](https://github.com/y3owk1n/oku/issues/78)) ([ac04e91](https://github.com/y3owk1n/oku/commit/ac04e91698c6fe0128620533d7f32cbf04d7f1b8))
* **version:** date a moving tag's version from its commit ([#77](https://github.com/y3owk1n/oku/issues/77)) ([bed5770](https://github.com/y3owk1n/oku/commit/bed5770fb9d1361ea1c97ffc74ff8a88b54dbe0b))

## [0.2.2](https://github.com/y3owk1n/oku/compare/v0.2.1...v0.2.2) (2026-09-20)


### Bug Fixes

* **hook:** stop asking for a sync when the lock holds relative refs ([#72](https://github.com/y3owk1n/oku/issues/72)) ([074b88a](https://github.com/y3owk1n/oku/commit/074b88ae28026210d6e130f8fe1b2f56960b9b61))

## [0.2.1](https://github.com/y3owk1n/oku/compare/v0.2.0...v0.2.1) (2026-09-20)


### Bug Fixes

* **refs:** store a file inside the project as a relative ref ([#70](https://github.com/y3owk1n/oku/issues/70)) ([28a3d14](https://github.com/y3owk1n/oku/commit/28a3d149b8fb838e933b9adfa0ce0d7905b5123c))

## [0.2.0](https://github.com/y3owk1n/oku/compare/v0.1.0...v0.2.0) (2026-09-20)


### Features

* **hook:** make one hook line the whole shell setup ([#65](https://github.com/y3owk1n/oku/issues/65)) ([d43d490](https://github.com/y3owk1n/oku/commit/d43d490c83bb72c1c4646ebf0ab35952142b9a52))


### Bug Fixes

* **sandbox:** let builds read the store on newer macOS ([#67](https://github.com/y3owk1n/oku/issues/67)) ([f33fa16](https://github.com/y3owk1n/oku/commit/f33fa163512b7b4cca196dff4ed5455347aface8))

## [0.1.0](https://github.com/y3owk1n/oku/compare/v0.0.0...v0.1.0) (2026-09-20)


### Features

* **build:** add vendor steps with a pinned download ([#24](https://github.com/y3owk1n/oku/issues/24)) ([a2c441b](https://github.com/y3owk1n/oku/commit/a2c441bb80cb33bb3ed514e432aa66e83a7e9332))
* **build:** apply patches with a patch step ([#55](https://github.com/y3owk1n/oku/issues/55)) ([f481fed](https://github.com/y3owk1n/oku/commit/f481fed6583473c5d88f62a538236c13314fb320))
* **build:** build packages from source, with approval ([#20](https://github.com/y3owk1n/oku/issues/20)) ([359b024](https://github.com/y3owk1n/oku/commit/359b0247793c4af66a877e6eea202f168ea12f52))
* **cache:** share built packages through signed caches ([#37](https://github.com/y3owk1n/oku/issues/37)) ([9c2a2d5](https://github.com/y3owk1n/oku/commit/9c2a2d53237112fca73dbfe8d05f1e1f6a61841d))
* **cli:** add gc ([#14](https://github.com/y3owk1n/oku/issues/14)) ([214a3f2](https://github.com/y3owk1n/oku/commit/214a3f2ace75790ed7f037dfe03baf3c7e08a45b))
* **cli:** add generations and rollback ([#13](https://github.com/y3owk1n/oku/issues/13)) ([ebc3196](https://github.com/y3owk1n/oku/commit/ebc3196059ce1c268ebdd5d99e5538a6ce9af112))
* **cli:** add manifest test and info, fix leaked build dirs ([#25](https://github.com/y3owk1n/oku/issues/25)) ([d4b6262](https://github.com/y3owk1n/oku/commit/d4b62621e1a2b0e9bcc308509e19e0b9fed3fdf4))
* **cli:** add module skeleton and platform detection ([#1](https://github.com/y3owk1n/oku/issues/1)) ([240b8bd](https://github.com/y3owk1n/oku/commit/240b8bda1d55eaf6e41925d7ce8c9f4bbe7fc24f))
* **cli:** add per-project lists ([#27](https://github.com/y3owk1n/oku/issues/27)) ([b3a9bb5](https://github.com/y3owk1n/oku/commit/b3a9bb54d33848f84540be57388a1848b9e6996c))
* **cli:** add sync and update, and start the user docs ([#8](https://github.com/y3owk1n/oku/issues/8)) ([952da0e](https://github.com/y3owk1n/oku/commit/952da0e314453f611e33b288be627bc5baf45b0b))
* **cli:** add, remove, list and self uninstall ([#4](https://github.com/y3owk1n/oku/issues/4)) ([7c99877](https://github.com/y3owk1n/oku/commit/7c99877b39ca1318f8346e2dbe831485f8b8aaa0))
* **cli:** print data as JSON with --json ([#54](https://github.com/y3owk1n/oku/issues/54)) ([125815a](https://github.com/y3owk1n/oku/commit/125815a0aa0251cc13420b8fdba4dbe9f75b37d4))
* **cli:** set up a machine from a published list ([#10](https://github.com/y3owk1n/oku/issues/10)) ([eeb75ab](https://github.com/y3owk1n/oku/commit/eeb75ab14a382d42205d1bafce1dc418306ac39e))
* **deps:** install dependencies and link builds against them ([#21](https://github.com/y3owk1n/oku/issues/21)) ([dbef7b1](https://github.com/y3owk1n/oku/commit/dbef7b1fc7a0afd119dcce41d39b477ecdb2fa01))
* **doctor:** check the setup and say what to fix ([#53](https://github.com/y3owk1n/oku/issues/53)) ([d898309](https://github.com/y3owk1n/oku/commit/d8983095662a0fe27722b9697d20223f58a541f4))
* **expose:** install apps and fonts, tracked in a ledger ([#30](https://github.com/y3owk1n/oku/issues/30)) ([f034153](https://github.com/y3owk1n/oku/commit/f034153d724deb88ad356dea8e2a7556b4cfda70))
* **hook:** activate an allowed project from the shell ([#28](https://github.com/y3owk1n/oku/issues/28)) ([671bb74](https://github.com/y3owk1n/oku/commit/671bb7473901acf116028f6d121394a67e8cdb7d))
* **hook:** apply a project's environment in PowerShell ([#45](https://github.com/y3owk1n/oku/issues/45)) ([a53e215](https://github.com/y3owk1n/oku/commit/a53e215cbf4c717403d5a0bea1550193fb99c4f5))
* **infer:** install from a GitHub repo that has no manifest ([#16](https://github.com/y3owk1n/oku/issues/16)) ([5f12418](https://github.com/y3owk1n/oku/commit/5f1241860719374e2555dd24a247166885e54e43))
* **list:** add include and when to oku.toml ([#9](https://github.com/y3owk1n/oku/issues/9)) ([8ed9a7f](https://github.com/y3owk1n/oku/commit/8ed9a7fe8cb9ee52563295013568786f43d45936))
* **lock:** write oku.toml and oku.lock on add and remove ([#7](https://github.com/y3owk1n/oku/issues/7)) ([0b2e131](https://github.com/y3owk1n/oku/commit/0b2e13162e8079cd41d2482de24743525129c8e2))
* **manifest:** add lint and bump ([#18](https://github.com/y3owk1n/oku/issues/18)) ([68c4b36](https://github.com/y3owk1n/oku/commit/68c4b3696d85f113084163b07ee19ec9d6d3662e))
* **manifest:** parse and validate package manifests ([#2](https://github.com/y3owk1n/oku/issues/2)) ([483b357](https://github.com/y3owk1n/oku/commit/483b35772a711ab55d46b007e40710f421ebaa4a))
* **ref:** install from https, github and git refs ([#6](https://github.com/y3owk1n/oku/issues/6)) ([6ec9c23](https://github.com/y3owk1n/oku/commit/6ec9c230f648030b8908b52848137bb81c9f21c2))
* **release:** build the release key into oku ([#63](https://github.com/y3owk1n/oku/issues/63)) ([1c7e7de](https://github.com/y3owk1n/oku/commit/1c7e7de934fba1bdb0fcd414e4e2b9a5e8ccaef1))
* **release:** self update, install scripts and signed releases ([#59](https://github.com/y3owk1n/oku/issues/59)) ([0d4f09c](https://github.com/y3owk1n/oku/commit/0d4f09c191e466a3c3153b1db3aae4207a141e60))
* **resolve:** discover versions from releases and tags ([#12](https://github.com/y3owk1n/oku/issues/12)) ([d728520](https://github.com/y3owk1n/oku/commit/d7285205e473bd9eb27acb033874033ae126b75d))
* **sandbox:** run build commands without network or home ([#23](https://github.com/y3owk1n/oku/issues/23)) ([e8fdb2a](https://github.com/y3owk1n/oku/commit/e8fdb2aec99dc9152b9765b4e483c49234e72a18))
* **service:** run package services with launchd and systemd ([#33](https://github.com/y3owk1n/oku/issues/33)) ([aaa3627](https://github.com/y3owk1n/oku/commit/aaa3627b49341c241706ceb399da00387c89cb2f))
* **setup:** create a shared store root with oku setup --system ([#34](https://github.com/y3owk1n/oku/issues/34)) ([e903ca4](https://github.com/y3owk1n/oku/commit/e903ca4bbeb5006f888b4e051be691d5b6f90c89))
* **shell:** open a shell with packages on PATH and install nothing ([#40](https://github.com/y3owk1n/oku/issues/40)) ([3f18109](https://github.com/y3owk1n/oku/commit/3f18109fd5df0f179f0e606ad73c267d7c20ea90))
* **source:** add sources, alias refs and search ([#17](https://github.com/y3owk1n/oku/issues/17)) ([a82be53](https://github.com/y3owk1n/oku/commit/a82be536c8e9d4ccf3119d386e66db246ca679bc))
* **store:** realize artifacts into the store ([#3](https://github.com/y3owk1n/oku/issues/3)) ([002828c](https://github.com/y3owk1n/oku/commit/002828c2c739c00b9559002ab74a93124dc7b8c6))
* **store:** unpack 7z downloads ([#57](https://github.com/y3owk1n/oku/issues/57)) ([7f5b889](https://github.com/y3owk1n/oku/commit/7f5b889a80e5f53bff37d97c555d147958567c88))
* **store:** unpack msi downloads on Windows ([#48](https://github.com/y3owk1n/oku/issues/48)) ([972e2b3](https://github.com/y3owk1n/oku/commit/972e2b3cebcee9c47a6d4882952e5ae425fa6f31))
* **store:** unpack xz, zst, deb, rpm, dmg and pkg ([#31](https://github.com/y3owk1n/oku/issues/31)) ([d508a46](https://github.com/y3owk1n/oku/commit/d508a462afe7f4bbebf65e002d73bf64f85c191b))
* **system:** install apps, fonts and services for the whole machine ([#35](https://github.com/y3owk1n/oku/issues/35)) ([00619a9](https://github.com/y3owk1n/oku/commit/00619a9f151791809bd457f4913029e3418eaf52))
* **trust:** verify artifacts against a manifest's signing key ([#38](https://github.com/y3owk1n/oku/issues/38)) ([06ca0e6](https://github.com/y3owk1n/oku/commit/06ca0e67f64b164195db6f941d62fc903b2511a7))
* **windows:** build packages from source ([#47](https://github.com/y3owk1n/oku/issues/47)) ([7ee5d97](https://github.com/y3owk1n/oku/commit/7ee5d972adcd1d4c63f4b7ddf2671906d2cee4ef))
* **windows:** delete the running oku.exe on uninstall ([#46](https://github.com/y3owk1n/oku/issues/46)) ([21d1f66](https://github.com/y3owk1n/oku/commit/21d1f665e38c25c533b0482e8864b1242e4182f6))
* **windows:** expose apps and fonts for the current user ([#49](https://github.com/y3owk1n/oku/issues/49)) ([87e903a](https://github.com/y3owk1n/oku/commit/87e903a47212ac518aca84f5bd11743aa7706fee))
* **windows:** run profiles through shims and a junction ([#44](https://github.com/y3owk1n/oku/issues/44)) ([40a4105](https://github.com/y3owk1n/oku/commit/40a4105d589a958a9a01ed5afd7a571e0212a395))
* **windows:** run services as scheduled tasks ([#50](https://github.com/y3owk1n/oku/issues/50)) ([b51419e](https://github.com/y3owk1n/oku/commit/b51419e0c2e5395947c99f795acebaadf427b827))
* **windows:** system scope for apps, fonts and services ([#58](https://github.com/y3owk1n/oku/issues/58)) ([cf86517](https://github.com/y3owk1n/oku/commit/cf865172b7fc0fe332b9ff10b0b891892b5a3f24))


### Bug Fixes

* **infer:** match release assets named x86_64 ([#42](https://github.com/y3owk1n/oku/issues/42)) ([ef31385](https://github.com/y3owk1n/oku/commit/ef31385fc342b4b0f5765b24a8e567fed20477ac))
* **sandbox:** detect hosts that allow a user namespace but no mounts ([#43](https://github.com/y3owk1n/oku/issues/43)) ([822e785](https://github.com/y3owk1n/oku/commit/822e785b778e0848d0f81220f45b36498aaae864))


### Documentation

* **prd:** record the design that build step 10 settled ([#39](https://github.com/y3owk1n/oku/issues/39)) ([fda9548](https://github.com/y3owk1n/oku/commit/fda95489781de5fd89f1177e3e3c02ec7f5765cc))
* **prd:** record the design that build step 2 settled ([#11](https://github.com/y3owk1n/oku/issues/11)) ([5c05abe](https://github.com/y3owk1n/oku/commit/5c05abe6b995dfdefefa2c464cec4b200fc35636))
* **prd:** record the design that build step 3 settled ([#15](https://github.com/y3owk1n/oku/issues/15)) ([7a3c825](https://github.com/y3owk1n/oku/commit/7a3c82522dab36be3cf3e8dbd22bc9b5ebfd161c))
* **prd:** record the design that build step 4 settled ([#19](https://github.com/y3owk1n/oku/issues/19)) ([4a0f691](https://github.com/y3owk1n/oku/commit/4a0f691811f8e2e567bfe21f4f0957c275b30d57))
* **prd:** record the design that build step 5 settled ([#22](https://github.com/y3owk1n/oku/issues/22)) ([6b3fd4a](https://github.com/y3owk1n/oku/commit/6b3fd4af3e81674dd3a55f8e5c2c9bccecd53ed8))
* **prd:** record the design that build step 6 settled ([#26](https://github.com/y3owk1n/oku/issues/26)) ([9a5ef55](https://github.com/y3owk1n/oku/commit/9a5ef5511ea6269806418a31e1803e33f9359caa))
* **prd:** record the design that build step 7 settled ([#29](https://github.com/y3owk1n/oku/issues/29)) ([eb74183](https://github.com/y3owk1n/oku/commit/eb7418379966e96209a87d94fde5cbb286454939))
* **prd:** record the design that build step 8 settled ([#36](https://github.com/y3owk1n/oku/issues/36)) ([344ba77](https://github.com/y3owk1n/oku/commit/344ba772b10131df2aa759ccbc540d570fc7ee84))
* **prd:** record the design that build step 9 settled ([#51](https://github.com/y3owk1n/oku/issues/51)) ([aa319ad](https://github.com/y3owk1n/oku/commit/aa319adc23d3afa0a9cd0e516c38180f1a9deb47))
* **prd:** rewrap a paragraph of D50 to 80 columns ([#52](https://github.com/y3owk1n/oku/issues/52)) ([ff640c6](https://github.com/y3owk1n/oku/commit/ff640c6b96e8970492467eb95f8c3df5942d9165))
* record what build step 1 delivers ([#5](https://github.com/y3owk1n/oku/issues/5)) ([dee1d2e](https://github.com/y3owk1n/oku/commit/dee1d2e8a810c2da4598a5a054714467bc54d23d))
* write a front page README and bring the docs up to date ([#64](https://github.com/y3owk1n/oku/issues/64)) ([1c87144](https://github.com/y3owk1n/oku/commit/1c87144dffe4d4f9b312104b019e4b4a89f01dbf))
