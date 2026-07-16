# Changelog

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
