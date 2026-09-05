# Changelog

## [1.21.0](https://github.com/torrin-app/torrin/compare/v1.20.0...v1.21.0) (2026-09-05)


### Features

* **add:** one endpoint that auto-detects magnet, infohash, link or nzb ([#116](https://github.com/torrin-app/torrin/issues/116)) ([052cd82](https://github.com/torrin-app/torrin/commit/052cd82ea6daf27215902fcdd070c389836b3181))
* **auth:** normalize signup emails and cap free signups per IP ([#113](https://github.com/torrin-app/torrin/issues/113)) ([ad37bca](https://github.com/torrin-app/torrin/commit/ad37bcad31a10135dc11b7daa33b0500361d7d21))
* **byos:** cold cache tier + persistent availability cache ([#123](https://github.com/torrin-app/torrin/issues/123)) ([da5d33b](https://github.com/torrin-app/torrin/commit/da5d33bedf6cd8777d115c6cd5b8a0c97b705801))
* **byos:** multi-provider pool, config export, cleanup, encrypted mirror ([#104](https://github.com/torrin-app/torrin/issues/104)) ([5033441](https://github.com/torrin-app/torrin/commit/5033441701d42392769404a424caf0173032c8c6))
* **cairn:** store archive NZBs in object storage ([#114](https://github.com/torrin-app/torrin/issues/114)) ([4e86a5b](https://github.com/torrin-app/torrin/commit/4e86a5b82f8105575c19aeca59348c9f8bcfe05d))
* cluster routing, debrid import fallback, download rate limits, and dedicated eviction ([#131](https://github.com/torrin-app/torrin/issues/131)) ([7307849](https://github.com/torrin-app/torrin/commit/730784938162c0eaa71c4df21bfe5308e1da4d76))
* debrid provider resilience (real error reasons + circuit breaker) ([#109](https://github.com/torrin-app/torrin/issues/109)) ([6dc4d38](https://github.com/torrin-app/torrin/commit/6dc4d38983c4f10e1d31fb40a831d0f6eccb8873))
* multi-node cache overflow, seeding, cross-seed and RSS ([#117](https://github.com/torrin-app/torrin/issues/117)) ([4071f93](https://github.com/torrin-app/torrin/commit/4071f9374acfee4f9da12ae6c8d86f4fb259a040))
* multiple API keys and node-scoped archiving ([#118](https://github.com/torrin-app/torrin/issues/118)) ([55ba76c](https://github.com/torrin-app/torrin/commit/55ba76c2b5dfe8ae59f7464911c3197eb0f44a19))
* Offcloud library import, admin key stats, and immediate torrent delete ([#122](https://github.com/torrin-app/torrin/issues/122)) ([6e03ec6](https://github.com/torrin-app/torrin/commit/6e03ec662c41c1707e7c39cadc46bc918790ee45))
* Offcloud provider and serving-cache refresh on publish ([#121](https://github.com/torrin-app/torrin/issues/121)) ([b9a5ec0](https://github.com/torrin-app/torrin/commit/b9a5ec0c3fabf48124a45f352b99ba3238e557ef))
* post new donations to a Discord webhook ([#107](https://github.com/torrin-app/torrin/issues/107)) ([5b284ea](https://github.com/torrin-app/torrin/commit/5b284eac961b492983db320611842be08d79dd15))
* prepaid wallet with credit top-up and plan purchases ([#124](https://github.com/torrin-app/torrin/issues/124)) ([870962b](https://github.com/torrin-app/torrin/commit/870962b9988da981cd3dfc02ac77f02a04c334b7))
* reliability, multi-node serving, and usenet improvements ([#127](https://github.com/torrin-app/torrin/issues/127)) ([36ae77a](https://github.com/torrin-app/torrin/commit/36ae77a1b47f9ca99b90b473661013b23db26838))
* stream cairn archives directly from usenet ([#128](https://github.com/torrin-app/torrin/issues/128)) ([b48a170](https://github.com/torrin-app/torrin/commit/b48a1708f6ac9ae84301f5c366a71bec490aaf8e))
* stremthru newz store endpoints ([#108](https://github.com/torrin-app/torrin/issues/108)) ([ad4a6a8](https://github.com/torrin-app/torrin/commit/ad4a6a8954944b976ea482667ec917777b6efa80))
* two-factor authentication and node-scoped storage export ([#119](https://github.com/torrin-app/torrin/issues/119)) ([b133748](https://github.com/torrin-app/torrin/commit/b133748f904607aa1af6ab6ff554fc7b9982cd15))
* usenet indexer egress, password login, and reliability fixes ([#139](https://github.com/torrin-app/torrin/issues/139)) ([deed168](https://github.com/torrin-app/torrin/commit/deed168b513582813c4968f27546361cb36d5919))
* **usenet:** multiple indexers per user with pagination ([#111](https://github.com/torrin-app/torrin/issues/111)) ([95b3143](https://github.com/torrin-app/torrin/commit/95b3143d93efe02500eebc61a142f1583530f081))
* **usenet:** verified, cached indexer search for the addon ([#103](https://github.com/torrin-app/torrin/issues/103)) ([909afb1](https://github.com/torrin-app/torrin/commit/909afb13fc331c867517479239e0c3732acf8f91))
* **webdav:** folder hierarchy, compliant PROPFIND, browser access ([#101](https://github.com/torrin-app/torrin/issues/101)) ([8169db6](https://github.com/torrin-app/torrin/commit/8169db64006b92c102b68495b1520d4aa382f37e))


### Bug Fixes

* **add:** give pasted release URLs a real title ([#136](https://github.com/torrin-app/torrin/issues/136)) ([7e64e16](https://github.com/torrin-app/torrin/commit/7e64e1625248f58ef757ce5aa0e0ed3dc31eca5b))
* **byos:** copy via operations/copyurl + disable the source stream write timeout ([#134](https://github.com/torrin-app/torrin/issues/134)) ([07faa61](https://github.com/torrin-app/torrin/commit/07faa61271cd5fdda567e63389cd54768a691e01))
* **byos:** reliable large-file copy to user storage via rclone async copy ([#133](https://github.com/torrin-app/torrin/issues/133)) ([aec6bb4](https://github.com/torrin-app/torrin/commit/aec6bb487ee049181ea1a4b08b63ea3d54f5e144))
* **byos:** serve warm-cached content from cache, not user storage ([#135](https://github.com/torrin-app/torrin/issues/135)) ([abd1298](https://github.com/torrin-app/torrin/commit/abd129849e3147defc2ac279fb3566de7d84cd6d))
* dedupe account jobs and scope pack playback ([#137](https://github.com/torrin-app/torrin/issues/137)) ([4c8c81e](https://github.com/torrin-app/torrin/commit/4c8c81e7ffac9104fec1edd34e75ee891709729b))
* filter Stremio streams by episode ([#130](https://github.com/torrin-app/torrin/issues/130)) ([aee2ccc](https://github.com/torrin-app/torrin/commit/aee2cccdb3956d69c1d8df7f285b0bc0358f4640))
* **hdencode-solver:** virtual display + JS click for the updated link protector ([#138](https://github.com/torrin-app/torrin/issues/138)) ([e7ec312](https://github.com/torrin-app/torrin/commit/e7ec312b236b8726c2812e608d3d438ca6aa0314))
* prefer warm cache before Cairn streaming ([#132](https://github.com/torrin-app/torrin/issues/132)) ([da741d3](https://github.com/torrin-app/torrin/commit/da741d3dd1b045c0afd481522450ca0e810ae92f))
* reject corrupt videos, correct cross-node cache status, gate day passes ([#125](https://github.com/torrin-app/torrin/issues/125)) ([5e23005](https://github.com/torrin-app/torrin/commit/5e230059c21383b71eafb86502f6d4784ab79854))
* reliability across usenet, archive restore, downloads, storage, telegram + service logging ([#110](https://github.com/torrin-app/torrin/issues/110)) ([dcfc01c](https://github.com/torrin-app/torrin/commit/dcfc01c2ef827b5858bf687903914ec98adddc75))
* reliability and error-surfacing improvements ([#112](https://github.com/torrin-app/torrin/issues/112)) ([0b3da83](https://github.com/torrin-app/torrin/commit/0b3da8337f0919d34bd69b6047f09e0485dfac95))
* **usenet:** explicit re-add clears the delete tombstone ([#115](https://github.com/torrin-app/torrin/issues/115)) ([dd031ff](https://github.com/torrin-app/torrin/commit/dd031fff106d5bcc037b0091c47c309d98518854))
* **usenet:** search the anime category and resolve show title from imdb ([#140](https://github.com/torrin-app/torrin/issues/140)) ([503946c](https://github.com/torrin-app/torrin/commit/503946c7cbab129f4ba5149e2cbe9852e3ea3eb4))
* **ytdlp:** strip yt-dlp's structural noise from the error reason ([#106](https://github.com/torrin-app/torrin/issues/106)) ([139b697](https://github.com/torrin-app/torrin/commit/139b697268a066fc8355daab71e7a8a89c9c4ad7))
* **ytdlp:** surface the real yt-dlp reason instead of a generic error ([#105](https://github.com/torrin-app/torrin/issues/105)) ([f06a878](https://github.com/torrin-app/torrin/commit/f06a87810330045611473bae98bc1f7edb76a023))

## [1.20.0](https://github.com/torrin-app/torrin/compare/v1.19.0...v1.20.0) (2026-08-01)


### Features

* **debrid:** fail over to the next provider on download failure ([#90](https://github.com/torrin-app/torrin/issues/90)) ([9e1d483](https://github.com/torrin-app/torrin/commit/9e1d4831598eddda002069dc1d2b12bb1188436a))
* **eviction:** evict small cold files before large ones when over cap ([#97](https://github.com/torrin-app/torrin/issues/97)) ([773fc7b](https://github.com/torrin-app/torrin/commit/773fc7bc2431823fcf427087715f448031104f2a))
* filesystem storage backend (replace Garage S3 for the cache) ([#84](https://github.com/torrin-app/torrin/issues/84)) ([de284c5](https://github.com/torrin-app/torrin/commit/de284c51abd92dd03ade3b923f7303d05d776d0b))
* reseller settlement, stremthru magnet URIs, ytdlp progress ([#100](https://github.com/torrin-app/torrin/issues/100)) ([bc05de3](https://github.com/torrin-app/torrin/commit/bc05de3ce892050e5d4a7a0c0caa452c173f4eee))


### Bug Fixes

* **byok:** gate bring-your-own usenet and library behind a paid plan ([#94](https://github.com/torrin-app/torrin/issues/94)) ([7d636d1](https://github.com/torrin-app/torrin/commit/7d636d1ed3703ed9d125626a8cb8f008c457839b))
* **byok:** require a paid plan for bring-your-own debrid keys ([#93](https://github.com/torrin-app/torrin/issues/93)) ([e0a6bdd](https://github.com/torrin-app/torrin/commit/e0a6bdd54327a7e35baff34d09f8834d199eb44c))
* **cairn:** apply STORAGE_KEY so encrypted manifests decrypt ([#88](https://github.com/torrin-app/torrin/issues/88)) ([a3a6bf9](https://github.com/torrin-app/torrin/commit/a3a6bf9717ac991fb70a7def4fc4ca27f1c72b6c))
* **cairn:** read file content by blob key, not legacy path ([#89](https://github.com/torrin-app/torrin/issues/89)) ([3630696](https://github.com/torrin-app/torrin/commit/363069662911e84fb35f9ebde0e9d1c20b5406c9))
* **deploy:** add store overlay; writers root, readers ro ([#92](https://github.com/torrin-app/torrin/issues/92)) ([d00916d](https://github.com/torrin-app/torrin/commit/d00916dbb44d4ec3d6fdee31a1a15c5bd23830b2))
* download, cache, and rss reliability improvements ([#86](https://github.com/torrin-app/torrin/issues/86)) ([0b2513b](https://github.com/torrin-app/torrin/commit/0b2513b33890dc169d644cc62c7bd414cc7bdf08))
* **ingest:** finalize stranded dedup followers from cache in reconcile ([#95](https://github.com/torrin-app/torrin/issues/95)) ([7d207e7](https://github.com/torrin-app/torrin/commit/7d207e7018c93d7f443ec877978cf3aac006fcc3))
* **safety:** only block real .onion addresses, not dotted titles ([#98](https://github.com/torrin-app/torrin/issues/98)) ([78cc680](https://github.com/torrin-app/torrin/commit/78cc680814435b8e4584f96c3e8ead8262b1e068))
* **store:** report real modtime from the S3 store so the cache tier stays valid ([#91](https://github.com/torrin-app/torrin/issues/91)) ([ece67fd](https://github.com/torrin-app/torrin/commit/ece67fd922fc872f08ec639921165bce143625d9))
* **stream:** use the real media filename for downloads ([#99](https://github.com/torrin-app/torrin/issues/99)) ([fee5a49](https://github.com/torrin-app/torrin/commit/fee5a49906da80178d7fd04174d0d9d449d29e4c))
* **stremthru:** include name on check items and magnet on add responses ([#96](https://github.com/torrin-app/torrin/issues/96)) ([096020e](https://github.com/torrin-app/torrin/commit/096020e02cc7e26654b1b4a971c656c10561d23d))

## [1.19.0](https://github.com/torrin-app/torrin/compare/v1.18.0...v1.19.0) (2026-07-26)


### Features

* content-addressed encrypted blob store + cross-content dedup ([#82](https://github.com/torrin-app/torrin/issues/82)) ([7bc6d40](https://github.com/torrin-app/torrin/commit/7bc6d402f1378ef0ec52912e2abb52fc11fadf1a))

## [1.18.0](https://github.com/torrin-app/torrin/compare/v1.17.1...v1.18.0) (2026-07-26)


### Features

* encrypted BYOS, streamed from the user's own storage ([#77](https://github.com/torrin-app/torrin/issues/77)) ([4450fe4](https://github.com/torrin-app/torrin/commit/4450fe4793ef4dbf1f4f6c8cbc014cc297dca220))
* geo-route native addon, share georoute package ([#81](https://github.com/torrin-app/torrin/issues/81)) ([fc2ce61](https://github.com/torrin-app/torrin/commit/fc2ce61939fd6d808e07cfbb3613b3771d20eaae))


### Bug Fixes

* route BYOS users' Stremio playback to their own storage ([#79](https://github.com/torrin-app/torrin/issues/79)) ([45e8b3f](https://github.com/torrin-app/torrin/commit/45e8b3fb0156d0489bc120b8d11fb4137f652201))
* stremio addon streams BYOS users from their own storage ([#80](https://github.com/torrin-app/torrin/issues/80)) ([1a17566](https://github.com/torrin-app/torrin/commit/1a175667ecf3067f42cd43353224fa248850694f))

## [1.17.1](https://github.com/torrin-app/torrin/compare/v1.17.0...v1.17.1) (2026-07-25)


### Bug Fixes

* credit referrals on all payment rails ([#75](https://github.com/torrin-app/torrin/issues/75)) ([6eb1e71](https://github.com/torrin-app/torrin/commit/6eb1e71af6eb44e54f9c685725cf6c8bfb055bde))

## [1.17.0](https://github.com/torrin-app/torrin/compare/v1.16.0...v1.17.0) (2026-07-24)


### Features

* referral partner tracking ([#73](https://github.com/torrin-app/torrin/issues/73)) ([85bc0a3](https://github.com/torrin-app/torrin/commit/85bc0a36e9ae7283c4be2f314c2312a98738a0a6))

## [1.16.0](https://github.com/torrin-app/torrin/compare/v1.15.0...v1.16.0) (2026-07-23)


### Features

* geo-route download links to nearest relay ([#70](https://github.com/torrin-app/torrin/issues/70)) ([f69bbce](https://github.com/torrin-app/torrin/commit/f69bbce3e92d1bd2a7e58c1169c5673d08041379))


### Bug Fixes

* single-request-reopen dns for ingest + cairn ([#72](https://github.com/torrin-app/torrin/issues/72)) ([4bac804](https://github.com/torrin-app/torrin/commit/4bac8048f3e005f38b75fbb80e17e779070ffc51))

## [1.15.0](https://github.com/torrin-app/torrin/compare/v1.14.0...v1.15.0) (2026-07-21)


### Features

* resumable, ranged zip downloads ([#68](https://github.com/torrin-app/torrin/issues/68)) ([97f9ba1](https://github.com/torrin-app/torrin/commit/97f9ba1653a26dd0a5665b8ac69e6b3718a48b95))

## [1.14.0](https://github.com/torrin-app/torrin/compare/v1.13.0...v1.14.0) (2026-07-19)


### Features

* cache-origin fallback, RSS feed edit, debrid usage ([#66](https://github.com/torrin-app/torrin/issues/66)) ([c62745a](https://github.com/torrin-app/torrin/commit/c62745a88e0bd0e1df08faa32a611ea9fe0ceda9))

## [1.13.0](https://github.com/torrin-app/torrin/compare/v1.12.3...v1.13.0) (2026-07-17)


### Features

* **stream:** optional rclone read-through cache (RCLONE_CACHE_URL) ([#65](https://github.com/torrin-app/torrin/issues/65)) ([450cad7](https://github.com/torrin-app/torrin/commit/450cad7cb7db4ed723fe8997c4fd8348097b7ac2))


### Bug Fixes

* **stream:** disable write timeout for streaming, use 256KB buffered copy ([#63](https://github.com/torrin-app/torrin/issues/63)) ([29f5d16](https://github.com/torrin-app/torrin/commit/29f5d16227147e3ae5e5dc26b9410a70e76e4f12))

## [1.12.3](https://github.com/torrin-app/torrin/compare/v1.12.2...v1.12.3) (2026-07-16)


### Bug Fixes

* **usenet:** mark job processing during post-download (par2/unrar) ([#61](https://github.com/torrin-app/torrin/issues/61)) ([ce744f6](https://github.com/torrin-app/torrin/commit/ce744f60b02f635e6b4b5d63a276339b4f6f14e2))

## [1.12.2](https://github.com/torrin-app/torrin/compare/v1.12.1...v1.12.2) (2026-07-16)


### Bug Fixes

* **mediainfo:** ignore image streams, width-aware resolution ([#59](https://github.com/torrin-app/torrin/issues/59)) ([5276a7a](https://github.com/torrin-app/torrin/commit/5276a7adfd8cfc686ec5f898dcfab6297ff79acf))

## [1.12.1](https://github.com/torrin-app/torrin/compare/v1.12.0...v1.12.1) (2026-07-16)


### Bug Fixes

* **usenet:** validate yEnc pcrc32 on decode ([#57](https://github.com/torrin-app/torrin/issues/57)) ([00c5166](https://github.com/torrin-app/torrin/commit/00c5166819b0dad4f70d90951cb5bb407f582d83))

## [1.12.0](https://github.com/torrin-app/torrin/compare/v1.11.0...v1.12.0) (2026-07-15)


### Features

* media info (ffprobe) at ingest, exposed to addon ([#56](https://github.com/torrin-app/torrin/issues/56)) ([3d069c4](https://github.com/torrin-app/torrin/commit/3d069c499ff546917ac0dc4e81070943ea881d72))


### Performance Improvements

* parallel Cairn posting ([#54](https://github.com/torrin-app/torrin/issues/54)) ([d6c6cd3](https://github.com/torrin-app/torrin/commit/d6c6cd3a723a55197d7ea1339a4d1f2b22a4385b))

## [1.11.0](https://github.com/torrin-app/torrin/compare/v1.10.1...v1.11.0) (2026-07-15)


### Features

* Cairn usenet-backed permanent archive ([#52](https://github.com/torrin-app/torrin/issues/52)) ([5c1dba9](https://github.com/torrin-app/torrin/commit/5c1dba9d8847c8ab706deafa659a5361aa10cfd1))

## [1.10.1](https://github.com/torrin-app/torrin/compare/v1.10.0...v1.10.1) (2026-07-14)


### Bug Fixes

* enforce concurrency on library import + queue promotion ([#50](https://github.com/torrin-app/torrin/issues/50)) ([78164ce](https://github.com/torrin-app/torrin/commit/78164ce545d7c5004a909d66c8a1c8df58f3b422))

## [1.10.0](https://github.com/torrin-app/torrin/compare/v1.9.0...v1.10.0) (2026-07-14)


### Features

* admin endpoints ([#48](https://github.com/torrin-app/torrin/issues/48)) ([ff94552](https://github.com/torrin-app/torrin/commit/ff945524296d7c2f69e2745952880f6b362e1c3e))

## [1.9.0](https://github.com/torrin-app/torrin/compare/v1.8.0...v1.9.0) (2026-07-13)


### Features

* Bachs card payments ([#46](https://github.com/torrin-app/torrin/issues/46)) ([a366c51](https://github.com/torrin-app/torrin/commit/a366c519a70a9b6b0c1314f74956eb89ef821d43))

## [1.8.0](https://github.com/torrin-app/torrin/compare/v1.7.0...v1.8.0) (2026-07-13)


### Features

* add AllDebrid as a user debrid provider ([#44](https://github.com/torrin-app/torrin/issues/44)) ([2746447](https://github.com/torrin-app/torrin/commit/2746447bebdccabc52c72e826b8cb67d16cc23b0))

## [1.7.0](https://github.com/torrin-app/torrin/compare/v1.6.1...v1.7.0) (2026-07-13)


### Features

* handle zip, filejoin, and ts-join usenet releases ([#42](https://github.com/torrin-app/torrin/issues/42)) ([206d83f](https://github.com/torrin-app/torrin/commit/206d83f58ab52717501628d0ab9200ab9d795087))

## [1.6.1](https://github.com/torrin-app/torrin/compare/v1.6.0...v1.6.1) (2026-07-13)


### Bug Fixes

* extract 7z usenet releases in postproc ([#40](https://github.com/torrin-app/torrin/issues/40)) ([809d0d6](https://github.com/torrin-app/torrin/commit/809d0d6bcd5e995e0df422f41d2bcf17bac1fbb0))

## [1.6.0](https://github.com/torrin-app/torrin/compare/v1.5.4...v1.6.0) (2026-07-12)


### Features

* self-hosted crypto payments via Bitcart ([#38](https://github.com/torrin-app/torrin/issues/38)) ([095ce77](https://github.com/torrin-app/torrin/commit/095ce7785b3282df4323db5f42ec8d9a20c6d180))

## [1.5.4](https://github.com/torrin-app/torrin/compare/v1.5.3...v1.5.4) (2026-07-12)


### Bug Fixes

* usenet test checks saved credentials ([#36](https://github.com/torrin-app/torrin/issues/36)) ([3620fc5](https://github.com/torrin-app/torrin/commit/3620fc5c7c39df3abf667032a45c7cf3734766ef))

## [1.5.3](https://github.com/torrin-app/torrin/compare/v1.5.2...v1.5.3) (2026-07-12)


### Bug Fixes

* global CORS middleware on the api ([#34](https://github.com/torrin-app/torrin/issues/34)) ([4a3f1b1](https://github.com/torrin-app/torrin/commit/4a3f1b1393afe3aaccf4a53b6939d692ae488ce8))

## [1.5.2](https://github.com/torrin-app/torrin/compare/v1.5.1...v1.5.2) (2026-07-11)


### Bug Fixes

* CORS preflight on REST API ([#32](https://github.com/torrin-app/torrin/issues/32)) ([80bc76d](https://github.com/torrin-app/torrin/commit/80bc76dba25ee28b991351749972e16db5927560))

## [1.5.1](https://github.com/torrin-app/torrin/compare/v1.5.0...v1.5.1) (2026-07-11)


### Bug Fixes

* ytdlp fragmented download progress, speed, plan-size, and H.264 playback ([#30](https://github.com/torrin-app/torrin/issues/30)) ([13b4ca1](https://github.com/torrin-app/torrin/commit/13b4ca1007d0fd6880a9ba65727317137eba288e))

## [1.5.0](https://github.com/torrin-app/torrin/compare/v1.4.0...v1.5.0) (2026-07-11)


### Features

* hdencode reveal via headless solver behind the VPN ([#28](https://github.com/torrin-app/torrin/issues/28)) ([1ad514f](https://github.com/torrin-app/torrin/commit/1ad514f1976fdc221b0cc55d35b6bc3cf4ed0862))

## [1.4.0](https://github.com/torrin-app/torrin/compare/v1.3.1...v1.4.0) (2026-07-11)


### Features

* yt-dlp web-download source ([#26](https://github.com/torrin-app/torrin/issues/26)) ([7f4d13a](https://github.com/torrin-app/torrin/commit/7f4d13a50c57f033d9b2066e814a6b6cd0027241))

## [1.3.1](https://github.com/torrin-app/torrin/compare/v1.3.0...v1.3.1) (2026-07-07)


### Bug Fixes

* stremthru /magnets returns basenames so pack playback resolves ([#24](https://github.com/torrin-app/torrin/issues/24)) ([1701c17](https://github.com/torrin-app/torrin/commit/1701c17f1857e38ea06ec18b65b11f6fc1329776))

## [1.3.0](https://github.com/torrin-app/torrin/compare/v1.2.0...v1.3.0) (2026-07-07)


### Features

* addon searches library by title, not just imdb ([#22](https://github.com/torrin-app/torrin/issues/22)) ([bc6e1f0](https://github.com/torrin-app/torrin/commit/bc6e1f0d6e1ee3f1bab8f7a6d2887169bceae534))

## [1.2.0](https://github.com/torrin-app/torrin/compare/v1.1.1...v1.2.0) (2026-07-06)


### Features

* hosters availability endpoint via alldebrid ([#20](https://github.com/torrin-app/torrin/issues/20)) ([f725705](https://github.com/torrin-app/torrin/commit/f725705d5228409d6a8b346d2840ac4bebb6cc59))

## [1.1.1](https://github.com/torrin-app/torrin/compare/v1.1.0...v1.1.1) (2026-07-06)


### Bug Fixes

* paginate downloads list with keyset cursor ([#18](https://github.com/torrin-app/torrin/issues/18)) ([9f55261](https://github.com/torrin-app/torrin/commit/9f55261013642e445d770611e77050cc7e4cf0d4))

## [1.1.0](https://github.com/torrin-app/torrin/compare/v1.0.4...v1.1.0) (2026-07-05)


### Features

* usenet fallback for dead file-hosts + par2-robust downloader ([#16](https://github.com/torrin-app/torrin/issues/16)) ([73b7af4](https://github.com/torrin-app/torrin/commit/73b7af4eb55c398607ce2140fc4112b922987968))

## [1.0.4](https://github.com/torrin-app/torrin/compare/v1.0.3...v1.0.4) (2026-07-04)


### Bug Fixes

* **redeem:** allow expired free-trial users to redeem codes ([#14](https://github.com/torrin-app/torrin/issues/14)) ([6d8d94d](https://github.com/torrin-app/torrin/commit/6d8d94dbc7335f918bf018b3246dd1576cc970d5))

## [1.0.3](https://github.com/torrin-app/torrin/compare/v1.0.2...v1.0.3) (2026-07-02)


### Bug Fixes

* **reseller:** serialize redeemed flag in codes API ([#12](https://github.com/torrin-app/torrin/issues/12)) ([a8720bc](https://github.com/torrin-app/torrin/commit/a8720bc7bcbe26654984ffc2cba8f56a90c86851))

## [1.0.2](https://github.com/torrin-app/torrin/compare/v1.0.1...v1.0.2) (2026-07-02)


### Bug Fixes

* **status:** add processing status for usenet post-processing ([#10](https://github.com/torrin-app/torrin/issues/10)) ([8682e88](https://github.com/torrin-app/torrin/commit/8682e88c6ea62a0e7fdc0415309970913b1c3900))

## [1.0.1](https://github.com/torrin-app/torrin/compare/v1.0.0...v1.0.1) (2026-07-02)


### Bug Fixes

* **stremthru:** include file path in cached magnet check results ([#8](https://github.com/torrin-app/torrin/issues/8)) ([d5f313e](https://github.com/torrin-app/torrin/commit/d5f313ed40bf8313c0eda5e7e447625e3d23f0e3))

## 1.0.0 (2026-07-02)


### Bug Fixes

* resolve cache misses with own/system debrid keys only ([#6](https://github.com/torrin-app/torrin/issues/6)) ([5a3ca77](https://github.com/torrin-app/torrin/commit/5a3ca77bb1070dbf46e9133a7ef602597f08d608))
