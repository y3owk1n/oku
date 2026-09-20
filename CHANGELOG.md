# Changelog

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
