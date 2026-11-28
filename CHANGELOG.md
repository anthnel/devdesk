# Changelog

## [2.3.0](https://github.com/anthnel/devdesk/compare/v2.2.0...v2.3.0) (2026-11-28)


### Features

* **about:** open the online help with W ([#511](https://github.com/anthnel/devdesk/issues/511)) ([d0ae0d0](https://github.com/anthnel/devdesk/commit/d0ae0d0b5f33b5e57cf631e57b204fa0450bd1d9))
* **config:** add sensitive environment variable names with a configurable list (DEVDESK-810) ([#519](https://github.com/anthnel/devdesk/issues/519)) ([ea01857](https://github.com/anthnel/devdesk/commit/ea0185745e9bd71fcaaa80973b40d46a0b1fa0f7))
* **containers:** confirm sensitive bind mounts and the engine socket (OC-20, DEVDESK-818) ([#543](https://github.com/anthnel/devdesk/issues/543)) ([cb80930](https://github.com/anthnel/devdesk/commit/cb80930bb5b544d184409cb1d52e847160b9c2ed))
* **containers:** confirm the host network and refuse flag-like launch values (OC-21, DEVDESK-819) ([#544](https://github.com/anthnel/devdesk/issues/544)) ([1236d85](https://github.com/anthnel/devdesk/commit/1236d850a0df4e7e5b2d22f359ef893cf3d34d9d))
* **containers:** destroy exactly what the confirmation showed, never forced (OC-22, DEVDESK-820) ([#545](https://github.com/anthnel/devdesk/issues/545)) ([dc0b4c5](https://github.com/anthnel/devdesk/commit/dc0b4c5024b9bf40e1f71961d1960c966fd61ca3))
* **containers:** mask the environment values of an inspect by default (DEVDESK-811) ([#541](https://github.com/anthnel/devdesk/issues/541)) ([78185da](https://github.com/anthnel/devdesk/commit/78185daaa86137fe32c257fc1cffb65a589769bb))
* **containers:** say in the views whether the engine is missing, stopped or refusing access (OC-24, DEVDESK-822) ([#548](https://github.com/anthnel/devdesk/issues/548)) ([f1e659b](https://github.com/anthnel/devdesk/commit/f1e659b717ab5abd2f78c6aa2f7ff5836b4fb9af))
* **docker:** say whether the engine is missing, stopped or refusing access (OC-24, DEVDESK-822) ([#547](https://github.com/anthnel/devdesk/issues/547)) ([a2bb0fd](https://github.com/anthnel/devdesk/commit/a2bb0fdfd6fb4fee6e01a98d263ae32068e79a9d))
* **oci:** publish container ports on 127.0.0.1 by default (DEVDESK-817) ([#518](https://github.com/anthnel/devdesk/issues/518)) ([1a31857](https://github.com/anthnel/devdesk/commit/1a3185759a9a33c82ede9d5f2a0993353138289d))
* **tools:** Incus engine test VMs on jakku (DEVDESK-795) ([#516](https://github.com/anthnel/devdesk/issues/516)) ([15b7a68](https://github.com/anthnel/devdesk/commit/15b7a686f639f096cdf729cd72c163f84daec1bb))


### Bug Fixes

* **containers:** detect the shell off the Update loop and keep inspect JSON clean (DEVDESK-800) ([#514](https://github.com/anthnel/devdesk/issues/514)) ([9836076](https://github.com/anthnel/devdesk/commit/98360763528c9050839bf6654e30ca1978175f63))
* **containers:** keep only variable names in the launch cache (DEVDESK-815) ([#540](https://github.com/anthnel/devdesk/issues/540)) ([3ba7f64](https://github.com/anthnel/devdesk/commit/3ba7f64b3c2d2317c1c75081252d553d047d34bd))
* **containers:** pass launch environment values to the child, never in argv (DEVDESK-812) ([#534](https://github.com/anthnel/devdesk/issues/534)) ([65f378f](https://github.com/anthnel/devdesk/commit/65f378f3fa8321e3032c601dc2329ef0553848e8))
* **containers:** refuse reserved environment names in the launch form (DEVDESK-813) ([#535](https://github.com/anthnel/devdesk/issues/535)) ([c84e70c](https://github.com/anthnel/devdesk/commit/c84e70c168c2798f34c7a0f265c0b3689f6cb1c1))
* **containers:** run the log pager without a shell (DEVDESK-801) ([#520](https://github.com/anthnel/devdesk/issues/520)) ([f20522d](https://github.com/anthnel/devdesk/commit/f20522db8b8b792e13ca259fe6d677df7d1d0fe0))
* **containers:** show the launch command and copy it without environment values (DEVDESK-814) ([#537](https://github.com/anthnel/devdesk/issues/537)) ([ef8a313](https://github.com/anthnel/devdesk/commit/ef8a313e1d99beb980bde909f6ea0294e39da7e9))
* **docker:** validate identifiers before every destructive verb and add the G3 guard (DEVDESK-803) ([#525](https://github.com/anthnel/devdesk/issues/525)) ([b44aa4e](https://github.com/anthnel/devdesk/commit/b44aa4efc095b84234c8aa6412005c46e41fa6e3))
* **docker:** validate the container ID before opening a shell (OC-18, DEVDESK-816) ([#542](https://github.com/anthnel/devdesk/issues/542)) ([5ebba56](https://github.com/anthnel/devdesk/commit/5ebba560939ef254fda6b6bd7202e87a1238ca0b))
* **fields:** keep the palette and FilterBar search fields inside their box ([#510](https://github.com/anthnel/devdesk/issues/510)) ([9d18f86](https://github.com/anthnel/devdesk/commit/9d18f86d10a77018e3e811a701c3b6d506a662af))
* **scan:** keep the forge token out of the plumber argv (DEVDESK-945) ([#536](https://github.com/anthnel/devdesk/issues/536)) ([ee2fb09](https://github.com/anthnel/devdesk/commit/ee2fb0971926c1e58dda0354755b96b34d2bdcb5))

## [2.2.0](https://github.com/anthnel/devdesk/compare/v2.1.0...v2.2.0) (2026-11-25)


### Features

* **command:** show one alias per entry in the ctrl+p palette (DEVDESK-747) ([a0344e5](https://github.com/anthnel/devdesk/commit/a0344e589949b8a667b4e573d09757ff8daefd70))
* **config:** write the Docker Hub registry in a new context (DEVDESK-663) ([f1685ef](https://github.com/anthnel/devdesk/commit/f1685ef32a249899006506efdeeb1496ea59a0b3))
* **fuzzy:** switch the g prompt between fuzzy and exact matching (DEVDESK-678) ([#464](https://github.com/anthnel/devdesk/issues/464)) ([6a50fc2](https://github.com/anthnel/devdesk/commit/6a50fc2df84faf2a1d43fabb4228f16c63ba0505))
* **oci:** explain why a Docker Hub search or tag listing failed (DEVDESK-667) ([#461](https://github.com/anthnel/devdesk/issues/461)) ([938fb30](https://github.com/anthnel/devdesk/commit/938fb3034ffa14caa882bfce1c72a5ed88aa035b))
* **ports:** refuse to stop DevDesk, its parent and init, and stop the confirmed row (DEVDESK-708, DEVDESK-710, DEVDESK-714) ([#471](https://github.com/anthnel/devdesk/issues/471)) ([0317788](https://github.com/anthnel/devdesk/commit/0317788d2a7aa88bac8e2783a41b81c132e4e619))
* **ports:** stop a process in two steps, SIGTERM first then a confirmed SIGKILL (DEVDESK-711) ([#474](https://github.com/anthnel/devdesk/issues/474)) ([769cf10](https://github.com/anthnel/devdesk/commit/769cf1006027157522287f3966a5f54f23c74087))
* **ports:** verify the process identity at every signal (DEVDESK-709) ([ec45718](https://github.com/anthnel/devdesk/commit/ec4571840b0fdeb2cf5ad3f3e0b137615df757f0))
* **remediation:** offer to set seccomp RuntimeDefault on a pod that has none (DEVDESK-688) ([#469](https://github.com/anthnel/devdesk/issues/469)) ([6d38c21](https://github.com/anthnel/devdesk/commit/6d38c218510cbf5cb61ee8fcfc9ca87a00455aba))
* **remediation:** offer to turn hostNetwork, hostPID and hostIPC off (DEVDESK-687) ([#467](https://github.com/anthnel/devdesk/issues/467)) ([01a31c6](https://github.com/anthnel/devdesk/commit/01a31c6a5bc451eff2dcf52593d1410cc0211f3f))
* **remediation:** say what is missing when a Pod Security finding has no safe fix (DEVDESK-689) ([#468](https://github.com/anthnel/devdesk/issues/468)) ([b3a0b67](https://github.com/anthnel/devdesk/commit/b3a0b677e34cf0fe0d04480f3d937c8fa3d4ffb2))
* **scan:** label the Pod Security profile of each finding and filter by it (DEVDESK-686) ([#466](https://github.com/anthnel/devdesk/issues/466)) ([c4b07de](https://github.com/anthnel/devdesk/commit/c4b07de1de753e1ac40634f7d3a491ad288b62df))
* **security:** before/after Change preview in the finding detail (DEVDESK-753) ([#485](https://github.com/anthnel/devdesk/issues/485)) ([9e2086e](https://github.com/anthnel/devdesk/commit/9e2086e8315afdf458500b9e513cae166197709d))
* **security:** group the CVE to-do list by fix with g (DEVDESK-754) ([#487](https://github.com/anthnel/devdesk/issues/487)) ([534b373](https://github.com/anthnel/devdesk/commit/534b37360195849d704d29ca4e1b17b1ad671f11))
* **security:** group the Misconfigurations list by fix with g (DEVDESK-754) ([#488](https://github.com/anthnel/devdesk/issues/488)) ([42ed0b8](https://github.com/anthnel/devdesk/commit/42ed0b8b28073abfcf4bd927d16d6b8054d159d5))
* **security:** readable finding detail (DEVDESK-750) ([#482](https://github.com/anthnel/devdesk/issues/482)) ([07a1e84](https://github.com/anthnel/devdesk/commit/07a1e84e7b09cb72ad1594e22d5de48aadf5c2c5))
* **security:** structured Fix block and detail actions (enter, ctrl+o, Y, X) (DEVDESK-751) ([#483](https://github.com/anthnel/devdesk/issues/483)) ([ce9555a](https://github.com/anthnel/devdesk/commit/ce9555ae6284dcc63eb968f25b55649284205965))
* **security:** structured Steps and local Repro for findings DevDesk writes (DEVDESK-752) ([#484](https://github.com/anthnel/devdesk/issues/484)) ([0800ae5](https://github.com/anthnel/devdesk/commit/0800ae5b65786048ceb7acfe019b2e4c56ba9186))
* **setup:** write the Docker Hub registry from dk setup (DEVDESK-664) ([#460](https://github.com/anthnel/devdesk/issues/460)) ([2aa2c9b](https://github.com/anthnel/devdesk/commit/2aa2c9b1c516b5f0f11c7531589f218f299b894d))


### Bug Fixes

* **credentials:** bound every secret-store call so a locked keyring cannot hang startup (DEVDESK-745) ([#480](https://github.com/anthnel/devdesk/issues/480)) ([32290ce](https://github.com/anthnel/devdesk/commit/32290ce532c0e0af95f84f98e8276036d1b4840e))
* **dashboard:** never cut a box on a short terminal and drop the dashboard title line (DEVDESK-743) ([#479](https://github.com/anthnel/devdesk/issues/479)) ([fa5091c](https://github.com/anthnel/devdesk/commit/fa5091c031e93fad23f4e320d850b67bb87a3c0c))
* **netdiag:** let the Diagnostics table fill the viewport height (DEVDESK-744) ([#478](https://github.com/anthnel/devdesk/issues/478)) ([e3c95e7](https://github.com/anthnel/devdesk/commit/e3c95e7fb0cf181d2fc5d34b14bb1a52fc2d52e1))
* **scan:** build a Flux overlay again without the load restrictor when kustomize refused it, bounded to the repository (DEVDESK-783) ([#506](https://github.com/anthnel/devdesk/issues/506)) ([cddc97f](https://github.com/anthnel/devdesk/commit/cddc97f0c884799dd46deffd693df9bbbefb3e60))
* **scan:** cut the network of the kustomize and helm renderers so an untrusted repo triggers no connection (DEVDESK-767) ([#496](https://github.com/anthnel/devdesk/issues/496)) ([dd6c667](https://github.com/anthnel/devdesk/commit/dd6c66726ec49f38fe0cad55f104587bccb07a9c))
* **scan:** mask user argument values in the Running log, quote control characters in paths and write ./-x in the Steps (DEVDESK-768) ([#498](https://github.com/anthnel/devdesk/issues/498)) ([9793cec](https://github.com/anthnel/devdesk/commit/9793cec007e41accc5cf3238f752b643afd809ea))
* **scan:** pass chart and overlay folders as paths so a folder name is never an option or a remote base (DEVDESK-763) ([#491](https://github.com/anthnel/devdesk/issues/491)) ([514f666](https://github.com/anthnel/devdesk/commit/514f6665589d0da8f0d8d48999bb0483cd5b35a4))
* **scan:** quote repo paths, names and errors in the remaining scan logs and guard them (DEVDESK-775) ([#500](https://github.com/anthnel/devdesk/issues/500)) ([9092898](https://github.com/anthnel/devdesk/commit/90928985a31fea2ed42dd282d0b9847e8f44158b))
* **scan:** refuse any non-local reference in a native kustomize overlay before it runs (DEVDESK-771) ([#503](https://github.com/anthnel/devdesk/issues/503)) ([7045a24](https://github.com/anthnel/devdesk/commit/7045a24f4d7f4be7e85acdc01fedcfb53de4a186))
* **scan:** render kustomize and helm overlays from the git root and explain what kustomize refuses (DEVDESK-742) ([#477](https://github.com/anthnel/devdesk/issues/477)) ([bb73c3a](https://github.com/anthnel/devdesk/commit/bb73c3ac9e0b6838699302ef41d1a888854e1b22))
* **scan:** single-quote folder names in the finding Steps for a POSIX shell and fall back to text for unquotable names (DEVDESK-772) ([#501](https://github.com/anthnel/devdesk/issues/501)) ([9888f77](https://github.com/anthnel/devdesk/commit/9888f7752101b2d6c6b26fb51a591b4a37761979))
* **security:** align the facts under Where in the finding detail (DEVDESK-762) ([#486](https://github.com/anthnel/devdesk/issues/486)) ([a8b0f82](https://github.com/anthnel/devdesk/commit/a8b0f827212ba0346d8442a73206af3c38f63491))
* **security:** make the tab bar fit in 80 columns (DEVDESK-765) ([#492](https://github.com/anthnel/devdesk/issues/492)) ([d211ba5](https://github.com/anthnel/devdesk/commit/d211ba5d64a6cc327b692930264be727929c8431))
* **security:** mark invisible characters in grouped file names so two groups never look alike (DEVDESK-766) ([#490](https://github.com/anthnel/devdesk/issues/490)) ([4fd3d1e](https://github.com/anthnel/devdesk/commit/4fd3d1eb9fc510e09ca7ecc6f0d444a2c278052f))
* **theme:** raise every text colour to 4.5:1 on the 13 themes (DEVDESK-764) ([#493](https://github.com/anthnel/devdesk/issues/493)) ([c959acb](https://github.com/anthnel/devdesk/commit/c959acb22794c68a815b48723aa43b654f582cae))
* **theme:** reach 4.5:1 for the remaining text colours and 3:1 for the frame border on the 13 themes (DEVDESK-770, part A) ([#495](https://github.com/anthnel/devdesk/issues/495)) ([317805f](https://github.com/anthnel/devdesk/commit/317805ff42cb444803fb9ed385d15a9545da079f))
* **theme:** reach 4.5:1 on the focused and unfocused Yes/success buttons, the INFO and UNKNOWN badges and the error row on the 13 themes (DEVDESK-774) ([#502](https://github.com/anthnel/devdesk/issues/502)) ([3e9fbc7](https://github.com/anthnel/devdesk/commit/3e9fbc7bad0a04c4848645210d916495c3aae88f))
* **theme:** reach 4.5:1 on the HIGH and LOW severity badges and tie the gauge rail to the frame border (DEVDESK-770, part B) ([#497](https://github.com/anthnel/devdesk/issues/497)) ([30c3d13](https://github.com/anthnel/devdesk/commit/30c3d13e79fb42fc21ef130a6cf96885bba77542))
* **ui:** make the variable-length breadcrumb trails fit the terminal width (DEVDESK-769) ([#494](https://github.com/anthnel/devdesk/issues/494)) ([3a9ff19](https://github.com/anthnel/devdesk/commit/3a9ff19b928e0153218b4b97058cfbad42e09e4c))


### Performance

* **app:** pad the frame in one pass and record the View() cost tracks (DEVDESK-281) ([#476](https://github.com/anthnel/devdesk/issues/476)) ([7cadafd](https://github.com/anthnel/devdesk/commit/7cadafd35c159aeb3f17b67cf34483f3fd626089))

## [2.1.0](https://github.com/anthnel/devdesk/compare/v2.0.0...v2.1.0) (2026-11-21)


### Features

* add app.theme auto with embedded dark and light themes (DEVDESK-258) ([#367](https://github.com/anthnel/devdesk/issues/367)) ([f26012d](https://github.com/anthnel/devdesk/commit/f26012deee0da9774633375b72ba76ce0c5f9c3e))
* add mise run ci chaining the six CI checks (DEVDESK-342) ([#373](https://github.com/anthnel/devdesk/issues/373)) ([d5e7e85](https://github.com/anthnel/devdesk/commit/d5e7e85d641e2b24badf31e7d49796f3aa59cd49))
* add Run Diagnostics and Trace Path buttons to the Diagnostics form (DEVDESK-470) ([147593c](https://github.com/anthnel/devdesk/commit/147593c001b1f7eef842661c9f2996fb5e0761c9))
* add the native path trace engine internal/nettrace (DEVDESK-327) ([#400](https://github.com/anthnel/devdesk/issues/400)) ([9d9a868](https://github.com/anthnel/devdesk/commit/9d9a86886f6aeb35692d4386ae1b79a0eae2886d))
* add the Nexus bench NX-01 (DEVDESK-504) ([#420](https://github.com/anthnel/devdesk/issues/420)) ([75ec254](https://github.com/anthnel/devdesk/commit/75ec254e00ea306c04c533c52d80884c7a2c0920))
* add the Path tab with streaming hops and an H shortcut from Diagnostics (DEVDESK-328) ([#401](https://github.com/anthnel/devdesk/issues/401)) ([e096ef7](https://github.com/anthnel/devdesk/commit/e096ef7c6b098a6ca1d5e371926fb4c4ca1fb4a0))
* choose a Docker Engine container as the Path trace source with ctrl+o (DEVDESK-330) ([#403](https://github.com/anthnel/devdesk/issues/403)) ([1816b14](https://github.com/anthnel/devdesk/commit/1816b14696170b28a8cd6ce461d8dc2d341fff6e))
* classify each registry's image search answer into three verdicts (DEVDESK-303) ([#391](https://github.com/anthnel/devdesk/issues/391)) ([abe1f67](https://github.com/anthnel/devdesk/commit/abe1f6763991c64d93d054d5fcb0f2f6f6e8f68a))
* come back to image search level 1 with the left arrow key or esc (DEVDESK-307) ([#390](https://github.com/anthnel/devdesk/issues/390)) ([bd074e6](https://github.com/anthnel/devdesk/commit/bd074e6d6f91b11c7d2a47f22a37dd14b773317e))
* default image search order, key-tracked cursor and selected-row footer (DEVDESK-302) ([#388](https://github.com/anthnel/devdesk/issues/388)) ([c23b07a](https://github.com/anthnel/devdesk/commit/c23b07a84d74805222f994f2e729d93972333ea9))
* filter the level 1 image table by registry with the r key (DEVDESK-310) ([#396](https://github.com/anthnel/devdesk/issues/396)) ([9f593fb](https://github.com/anthnel/devdesk/commit/9f593fbf94e414bf5321d2583414bb63be1d8668))
* fold the Path tab into the Diagnostics trace level, four tabs (DEVDESK-471) ([e08e67c](https://github.com/anthnel/devdesk/commit/e08e67cef1bd40b3ee0febece873acc6d5459f17))
* grey out the Path tab with a named reason on unsupported platforms (DEVDESK-334) ([3168006](https://github.com/anthnel/devdesk/commit/31680064787cb408900ead2fdf3f64739d39d9ba))
* image search level 1 in the OCI registry browser, inactive until a searcher is wired (DEVDESK-299) ([#384](https://github.com/anthnel/devdesk/issues/384)) ([a65b54d](https://github.com/anthnel/devdesk/commit/a65b54d5e998ae81975ffbee203b58dc1a08fc4b))
* image search level 1 on Docker Hub (DEVDESK-301) ([#387](https://github.com/anthnel/devdesk/issues/387)) ([d6b7529](https://github.com/anthnel/devdesk/commit/d6b7529050d8e5c6508778b7b061c268dff1461f))
* keep each registry's outcome visible in the level 1 header (DEVDESK-305) ([#393](https://github.com/anthnel/devdesk/issues/393)) ([93396b7](https://github.com/anthnel/devdesk/commit/93396b763974e70ab505b756c5f93713706d6f7c))
* label WSL2 source and Windows host in the Path column (DEVDESK-333) ([#406](https://github.com/anthnel/devdesk/issues/406)) ([b522394](https://github.com/anthnel/devdesk/commit/b522394d9ff3f5004c470f294df9784d44f5bd37))
* migrate to Charm v2 (DEVDESK-201, DEVDESK-208) ([c0f9ee1](https://github.com/anthnel/devdesk/commit/c0f9ee1baab6072ff9e16a78fbba65d0902d98e7))
* **netcheck:** say a proxy is configured when the direct DNS lookup fails (DEVDESK-596) ([#430](https://github.com/anthnel/devdesk/issues/430)) ([20dece3](https://github.com/anthnel/devdesk/commit/20dece320ac68b46051e8f1e104affdd2ff8dc8c))
* **netcheck:** tell which proxy applies to a Diagnostics target (DEVDESK-582) ([#428](https://github.com/anthnel/devdesk/issues/428)) ([fe9ebf8](https://github.com/anthnel/devdesk/commit/fe9ebf8001433ea3355701372fc08c135e79b773))
* **nexusbench:** cut and restore the proxy upstream and reset the bench without recreating the instance (DEVDESK-534) ([#423](https://github.com/anthnel/devdesk/issues/423)) ([5ba3d5e](https://github.com/anthnel/devdesk/commit/5ba3d5e54d888eba30fbe498aa85a1e6a662ba4b))
* **nexusbench:** seed the Nexus bench with known images and deploy the proxy upstream (DEVDESK-505) ([#421](https://github.com/anthnel/devdesk/issues/421)) ([ace0fcd](https://github.com/anthnel/devdesk/commit/ace0fcdfe52c065362c6695e72965418ead23c26))
* open an image's tags with the right arrow key from image search level 1 (DEVDESK-304) ([#389](https://github.com/anthnel/devdesk/issues/389)) ([af28bf3](https://github.com/anthnel/devdesk/commit/af28bf3341773369cacac3212cc4605d0e08a569))
* open the tags of the typed name explicitly with a second Open Tags button in the registry browser (DEVDESK-300) ([#385](https://github.com/anthnel/devdesk/issues/385)) ([5ff4e6e](https://github.com/anthnel/devdesk/commit/5ff4e6e0b95f42ef20764ce3021b78a838d3d874))
* pause view reads while the terminal is unfocused, add app.when_unfocused (DEVDESK-210) ([#364](https://github.com/anthnel/devdesk/issues/364)) ([1df1d7b](https://github.com/anthnel/devdesk/commit/1df1d7b9a64b9c470ddf59ad178365027c154aa7))
* read a Nexus group member's catalog under its path (DEVDESK-579) ([#434](https://github.com/anthnel/devdesk/issues/434)) ([7cb91e8](https://github.com/anthnel/devdesk/commit/7cb91e8d35d052253b105e7d797c30c431be9980))
* read the local route once for Diagnostics and the path trace (DEVDESK-473) ([#417](https://github.com/anthnel/devdesk/issues/417)) ([e939a06](https://github.com/anthnel/devdesk/commit/e939a066bf118d45fdfcda7508c396a22791266b))
* record multi-exchange Bearer and redirect flows, manifest HEADs and bench states (DEVDESK-580) ([#436](https://github.com/anthnel/devdesk/issues/436)) ([53c839b](https://github.com/anthnel/devdesk/commit/53c839bb10491d97a95a25cfbd0c2e3a0619d8eb))
* replay recorded Nexus answers as regression tests (DEVDESK-511) ([#426](https://github.com/anthnel/devdesk/issues/426)) ([9f878a2](https://github.com/anthnel/devdesk/commit/9f878a2ad954099f5dcd7ba63ff3b82bc3d109f1))
* route the terminal focus level to the view on screen (DEVDESK-210) ([#363](https://github.com/anthnel/devdesk/issues/363)) ([eb13ee9](https://github.com/anthnel/devdesk/commit/eb13ee9e33f328cfc07abe34b3c50d8a2f3a529f))
* say no route or undetermined on the Diagnostics route stage (DEVDESK-474) ([899df36](https://github.com/anthnel/devdesk/commit/899df36399c93c9f101569303f68069290462291))
* say when a Nexus listing is limited to cached content (DEVDESK-308) ([#394](https://github.com/anthnel/devdesk/issues/394)) ([f74f306](https://github.com/anthnel/devdesk/commit/f74f306dd99428d4e6ee2df7aa722d002b894261))
* **scan:** forward the proxy to container scanners and document what DevDesk reads (DEVDESK-583) ([#429](https://github.com/anthnel/devdesk/issues/429)) ([7de12bb](https://github.com/anthnel/devdesk/commit/7de12bbb23c60d956ea084acded6ad8815556ff5))
* search a private registry through its catalog (DEVDESK-306) ([#392](https://github.com/anthnel/devdesk/issues/392)) ([75be683](https://github.com/anthnel/devdesk/commit/75be68354f508694a57e5d135d8bf976044ba87c))
* set the terminal background from the theme (DEVDESK-209) ([#356](https://github.com/anthnel/devdesk/issues/356)) ([5813532](https://github.com/anthnel/devdesk/commit/5813532127fdc1d30a33d3754a8ae9c7e8ca1caf))
* set the terminal window title to "dk · view · context" and restore it on exit (DEVDESK-257) ([#366](https://github.com/anthnel/devdesk/issues/366)) ([4042d2b](https://github.com/anthnel/devdesk/commit/4042d2b8c3279ea2fe340ea479c936c4a15f8761))
* shared activity dot for :containers, :agent-usage, :sec, :net, forge auth and dk setup (DEVDESK-371, DEVDESK-372, DEVDESK-373, DEVDESK-375, DEVDESK-376) ([#376](https://github.com/anthnel/devdesk/issues/376)) ([cbeda2e](https://github.com/anthnel/devdesk/commit/cbeda2ef6c30af5f739869abc7d9908eb69b32d9))
* shared activity dot for the viewer and :oci loads (DEVDESK-370, DEVDESK-374) ([#375](https://github.com/anthnel/devdesk/issues/375)) ([e4cc688](https://github.com/anthnel/devdesk/commit/e4cc688eaba43bc1ab1920fd90888111f65e192a))
* show an engine row in the Path column when the container's bridge is not visible on this machine (DEVDESK-332) ([#405](https://github.com/anthnel/devdesk/issues/405)) ([c2b4e96](https://github.com/anthnel/devdesk/commit/c2b4e961e818916c53d5f2c0ca3df29c40480cdc))
* show how far the call went when the path trace fails (DEVDESK-335) ([#409](https://github.com/anthnel/devdesk/issues/409)) ([d7c4267](https://github.com/anthnel/devdesk/commit/d7c4267d4917f1df35dcbf5590232119a302f6b2))
* show job progress in the terminal tab via View.ProgressBar (DEVDESK-211) ([#365](https://github.com/anthnel/devdesk/issues/365)) ([a596538](https://github.com/anthnel/devdesk/commit/a596538ebd1336f8b41a4dc3b5fdf124cd67d6ee))
* show the container's bridge in the Path column only when this machine has it (DEVDESK-331) ([#404](https://github.com/anthnel/devdesk/issues/404)) ([fb4e5f9](https://github.com/anthnel/devdesk/commit/fb4e5f93a44d56452fa8111801e38da9ad0d2a45))
* show the shared activity dot in :status and add the loading test bench (DEVDESK-369) ([#372](https://github.com/anthnel/devdesk/issues/372)) ([9c6c782](https://github.com/anthnel/devdesk/commit/9c6c782582f4c232e91a3bd4515126009fbb50c1))
* show the shared activity dot in the footer instead of the braille spinner (DEVDESK-341) ([#370](https://github.com/anthnel/devdesk/issues/370)) ([5f76cc1](https://github.com/anthnel/devdesk/commit/5f76cc162fe83642a169c2cff876ca039e0162f7))
* show this machine, its interface and gateway before the hops in the Path tab (DEVDESK-329) ([#402](https://github.com/anthnel/devdesk/issues/402)) ([65b4729](https://github.com/anthnel/devdesk/commit/65b4729623cdaa5bddb36aa4dce08ce510292dcc))
* sort the level 1 image table with the dot key (DEVDESK-309) ([#395](https://github.com/anthnel/devdesk/issues/395)) ([5db95ce](https://github.com/anthnel/devdesk/commit/5db95ce6daca9f20bca5338d3e1c325814e3a317))
* suspend the animation while the terminal is blurred (DEVDESK-210) ([#362](https://github.com/anthnel/devdesk/issues/362)) ([2cf7ea2](https://github.com/anthnel/devdesk/commit/2cf7ea2c8b721d6b0d0107af80fc6216f420c594))
* trace the path on native Windows with ICMP echoes (DEVDESK-486) ([388fc8c](https://github.com/anthnel/devdesk/commit/388fc8cfb7880e3d65eb20146568bc6a1c2a24d1))
* typed registry errors, context-aware tag lookups and generation-stamped tag results (DEVDESK-298) ([#383](https://github.com/anthnel/devdesk/issues/383)) ([e7a8a22](https://github.com/anthnel/devdesk/commit/e7a8a22ce9750535603fe08b53ba4b4f00a15a23))
* underline invalid form fields with a curly red line and a footer message (DEVDESK-292) ([#368](https://github.com/anthnel/devdesk/issues/368)) ([56166bf](https://github.com/anthnel/devdesk/commit/56166bf87b30d7459a515b1a4ec8dfd6626d1972))
* walk a host's catalog only once per search and identity (DEVDESK-311) ([#397](https://github.com/anthnel/devdesk/issues/397)) ([5285034](https://github.com/anthnel/devdesk/commit/5285034310645e5cad91d7f64b37ed4d75ec32a5))
* write a commented agent_usage.prices example into new context files (DEVDESK-314) ([#398](https://github.com/anthnel/devdesk/issues/398)) ([f5c0bc5](https://github.com/anthnel/devdesk/commit/f5c0bc5e3b02a384be141b32690de2e70862ffa1))


### Bug Fixes

* align the mise ci guard with D149 (DEVDESK-502) ([#418](https://github.com/anthnel/devdesk/issues/418)) ([ed14483](https://github.com/anthnel/devdesk/commit/ed1448325f3e717b70b93046e35f27b393844f9b))
* calibrate the hub body cap test to kernel TCP buffers (DEVDESK-411) ([#415](https://github.com/anthnel/devdesk/issues/415)) ([68f635d](https://github.com/anthnel/devdesk/commit/68f635d52e97d29fb18c36f71826a9183afd48f0))
* **deps:** bump golang.org/x/net to v0.62.0 (DEVDESK-278) ([#357](https://github.com/anthnel/devdesk/issues/357)) ([463d53a](https://github.com/anthnel/devdesk/commit/463d53aeca15919f44ea200725752abf2ad56e39))
* extend the registry origin and size hardening to the other HTTP clients (DEVDESK-412) ([#419](https://github.com/anthnel/devdesk/issues/419)) ([dae964b](https://github.com/anthnel/devdesk/commit/dae964b752081c4b139f213ae303f170a337ea77))
* join the bottom bar to the viewport frame (DEVDESK-457) ([d0b76ce](https://github.com/anthnel/devdesk/commit/d0b76ce76584bc1b0888d3386584ca8525ecb4b0))
* keep registry credentials on the registry's origin and bound the token response (DEVDESK-410) ([#399](https://github.com/anthnel/devdesk/issues/399)) ([f2617de](https://github.com/anthnel/devdesk/commit/f2617de9b327f48814821a943ebdd9181cc5a8c8))
* let the space key reach text fields in configuration and the OCI launch form (DEVDESK-266) ([#359](https://github.com/anthnel/devdesk/issues/359)) ([5393279](https://github.com/anthnel/devdesk/commit/5393279f0222f48c117bec9d9194e71e4d7040d1))
* measure datatable cells by grapheme cluster so VS16 emoji match x/ansi (DEVDESK-280) ([#371](https://github.com/anthnel/devdesk/issues/371)) ([2f383de](https://github.com/anthnel/devdesk/commit/2f383dec091ece5755c16ae6057f4f8018245253))
* **nexus:** act on the NX-04/NX-05 measurements (DEVDESK-629) ([#440](https://github.com/anthnel/devdesk/issues/440)) ([a253e8c](https://github.com/anthnel/devdesk/commit/a253e8c3cae2a5fbf9f98044d21ea92117eaf375))
* recognise Docker Hub through isDockerHub when loading tag dates (DEVDESK-297) ([#386](https://github.com/anthnel/devdesk/issues/386)) ([4beb086](https://github.com/anthnel/devdesk/commit/4beb0865fd9b0c22f4a0d7326142eb434daa7055))
* restart the cursor blink when the Diagnostics form regains focus (DEVDESK-485) ([2fdcc25](https://github.com/anthnel/devdesk/commit/2fdcc2537af4d103616a0faab37485681c47def2))
* **status:** route ClearFooterMsg and PostFooterMsg to the footer (DEVDESK-589) ([#431](https://github.com/anthnel/devdesk/issues/431)) ([e732f73](https://github.com/anthnel/devdesk/commit/e732f73c58c193cc057dd3196fe10ed09bef0dc4))
* **status:** route the SSL monitor through the proxy and guard against proxy-less HTTP clients (DEVDESK-581) ([#427](https://github.com/anthnel/devdesk/issues/427)) ([124a11b](https://github.com/anthnel/devdesk/commit/124a11b7bd8dbc4602a570da5f32939553a1029e))
* tell a refused Nexus group member list from an empty group (DEVDESK-510) ([#425](https://github.com/anthnel/devdesk/issues/425)) ([2faf472](https://github.com/anthnel/devdesk/commit/2faf472f9dadf5224dfbaa94c53026be3f9f3a97))
* treat a Nexus 404 "Repository not found" on the group endpoint as a members refusal (DEVDESK-578) ([#433](https://github.com/anthnel/devdesk/issues/433)) ([b7cd143](https://github.com/anthnel/devdesk/commit/b7cd143b91da42265967e5147d3eaee0942a3abf))
* type the Nexus management API errors and refuse unreadable replies (DEVDESK-509) ([#424](https://github.com/anthnel/devdesk/issues/424)) ([dce7480](https://github.com/anthnel/devdesk/commit/dce7480516821a99bb0e0a635077a9ca27ebb137))
* work around the bubbletea renderer ticker race after a fast exec (DEVDESK-290) ([#361](https://github.com/anthnel/devdesk/issues/361)) ([cd83400](https://github.com/anthnel/devdesk/commit/cd83400815e95e13027b2a9348c6286282fec9d6))


### Refactoring

* centralize the space key in keymap and guard legacy key fields (DEVDESK-200) ([#352](https://github.com/anthnel/devdesk/issues/352)) ([63ebd72](https://github.com/anthnel/devdesk/commit/63ebd72b3d0f834b5dfd584e055bbbe8e35a7ef6))
* remove the footer spinner API and guard against its return (DEVDESK-377) ([#378](https://github.com/anthnel/devdesk/issues/378)) ([37ab5a8](https://github.com/anthnel/devdesk/commit/37ab5a82a81ab5e3f758c35b1be16f0bacef729c))

## [2.0.0](https://github.com/anthnel/devdesk/compare/v1.7.0...v2.0.0) (2026-11-09)


### ⚠ BREAKING CHANGES

* `:ctx <unknown>` no longer creates a context; use

### Features

* add an ordered command registry as the single source (DEVDESK-89 step 1) ([#311](https://github.com/anthnel/devdesk/issues/311)) ([e854788](https://github.com/anthnel/devdesk/commit/e85478860de313de4d2bf6fae246f2b5081488e7))
* add the activity indicator component (DEVDESK-137) ([#327](https://github.com/anthnel/devdesk/issues/327)) ([ed40063](https://github.com/anthnel/devdesk/commit/ed40063394529ae08d4eddd4418668c0287f81fc))
* add the app.motion setting to stop the activity blink (DEVDESK-141) ([#331](https://github.com/anthnel/devdesk/issues/331)) ([f7f416d](https://github.com/anthnel/devdesk/commit/f7f416d51e54745994f3ecf13d1c53102b21d0f1))
* add the command suggestion engine over the registry (DEVDESK-89 step 2) ([#312](https://github.com/anthnel/devdesk/issues/312)) ([fa0494a](https://github.com/anthnel/devdesk/commit/fa0494a4f1aafccc17b891d8444068a7286f4df4))
* add the per-row footer detail and phrase builder (DEVDESK-140) ([#330](https://github.com/anthnel/devdesk/issues/330)) ([b3d32b2](https://github.com/anthnel/devdesk/commit/b3d32b2e4561f5eccada190b15adba47355f8ea7))
* break down a session's tokens by category, model and agent (DEVDESK-98) ([#322](https://github.com/anthnel/devdesk/issues/322)) ([2e65d4f](https://github.com/anthnel/devdesk/commit/2e65d4f00ba16051152304f4ff51d6c2be0ea138))
* compute WCAG contrast and fix three themes below 4.5:1 (DEVDESK-89 step 4) ([#314](https://github.com/anthnel/devdesk/issues/314)) ([13da708](https://github.com/anthnel/devdesk/commit/13da708008f89aab8fdec0b83a0a7ad30c2e595b))
* follow an active Claude Code session live in the :agent-usage view (DEVDESK-45) ([#306](https://github.com/anthnel/devdesk/issues/306)) ([cceb893](https://github.com/anthnel/devdesk/commit/cceb893a5d0d8d6574e4771d3ed0aaf48822b7b7))
* list Claude Code sessions in the :agent-usage view (DEVDESK-41) ([#302](https://github.com/anthnel/devdesk/issues/302)) ([a4be18d](https://github.com/anthnel/devdesk/commit/a4be18daf8c46c9b3429cfa5fbd66218faff1138))
* open views and commands from a modal command palette (DEVDESK-109) ([#319](https://github.com/anthnel/devdesk/issues/319)) ([d7f8f92](https://github.com/anthnel/devdesk/commit/d7f8f928e8a5820a712358e3f7cc7e22a74cfc88))
* parse a session's token usage per agent (DEVDESK-40) ([#301](https://github.com/anthnel/devdesk/issues/301)) ([7d6b9a2](https://github.com/anthnel/devdesk/commit/7d6b9a25410529bfd88f30b0ad50f65e23e7eb1d))
* put the :sec inventory on the activity column and drop the failure copy (DEVDESK-145) ([#335](https://github.com/anthnel/devdesk/issues/335)) ([f475a3b](https://github.com/anthnel/devdesk/commit/f475a3b8a03f74c3a3674eb746d47e00094ae097))
* put the agent-usage agents table on the activity column and colour the cost (DEVDESK-151) ([#343](https://github.com/anthnel/devdesk/issues/343)) ([86a0563](https://github.com/anthnel/devdesk/commit/86a056346ed955a168bf6472ad6ae51771341dfa))
* put the containers table on the activity column (DEVDESK-152) ([#344](https://github.com/anthnel/devdesk/issues/344)) ([84af59b](https://github.com/anthnel/devdesk/commit/84af59bfc9353584ed80473af813f56e63585002))
* put the explorer clone list on the activity column and redact clone causes (DEVDESK-148) ([#340](https://github.com/anthnel/devdesk/issues/340)) ([c2b443a](https://github.com/anthnel/devdesk/commit/c2b443aee331d73b2a16fd31e7fb1b2b2a6ce0f1))
* put the explorer tree on the activity column and drop the node spinner (DEVDESK-147) ([#339](https://github.com/anthnel/devdesk/issues/339)) ([aa350be](https://github.com/anthnel/devdesk/commit/aa350be453f145ef74216e85f98e0feaa4da3516))
* put the netdiag ports table on the activity column (DEVDESK-150) ([#342](https://github.com/anthnel/devdesk/issues/342)) ([043d034](https://github.com/anthnel/devdesk/commit/043d034e60d698805437b9a99f43b10d557b9b95))
* put the OCI images table on the activity column and drop the failure copy (DEVDESK-146) ([#336](https://github.com/anthnel/devdesk/issues/336)) ([d4d7551](https://github.com/anthnel/devdesk/commit/d4d755181c92540ffa7b4e14430dbb5cc8cd3a61))
* put the OCI networks, volumes and registries tables on the activity column (DEVDESK-153) ([#345](https://github.com/anthnel/devdesk/issues/345)) ([577f89f](https://github.com/anthnel/devdesk/commit/577f89fc9889638cd703ca6aaccc7165026a2bce))
* put the registry browser on the activity column and close the activity guard (DEVDESK-218, DEVDESK-143) ([#346](https://github.com/anthnel/devdesk/issues/346)) ([34abd58](https://github.com/anthnel/devdesk/commit/34abd58d212a44b360a6f45e8ed39685e8c6260b))
* put the templates catalog on the activity column (DEVDESK-149) ([#341](https://github.com/anthnel/devdesk/issues/341)) ([e037b2f](https://github.com/anthnel/devdesk/commit/e037b2f3ef0f7ecaaf9f9a58c156faffbf3dc2d6))
* read Claude Code transcripts incrementally and detect settled sessions (DEVDESK-44) ([#305](https://github.com/anthnel/devdesk/issues/305)) ([1049311](https://github.com/anthnel/devdesk/commit/10493113289b50129e4ee17d5e50e03f3dae497d))
* remember the last failure per target in the jobs registry (DEVDESK-138) ([#328](https://github.com/anthnel/devdesk/issues/328)) ([925f045](https://github.com/anthnel/devdesk/commit/925f0453123a280d6419893cb1f9754f49b486d7))
* render the command suggestion list as pure lines (DEVDESK-89 step 3) ([#313](https://github.com/anthnel/devdesk/issues/313)) ([31fa481](https://github.com/anthnel/devdesk/commit/31fa481f6c95d37b158026196a9f6e726724ea04))
* share one animation tick across blinking rows (DEVDESK-139) ([#329](https://github.com/anthnel/devdesk/issues/329)) ([4b505ee](https://github.com/anthnel/devdesk/commit/4b505ee4ff192476db649a91521749dd72f8e764))
* show :jobs state in the activity column and add the activity-column guard (DEVDESK-142) ([#332](https://github.com/anthnel/devdesk/issues/332)) ([b2c3224](https://github.com/anthnel/devdesk/commit/b2c32243342a645be4c4cf15f2236169ea891324))
* show a suggestion list under the command line (DEVDESK-89 step 5) ([#316](https://github.com/anthnel/devdesk/issues/316)) ([f653cbc](https://github.com/anthnel/devdesk/commit/f653cbc395729e962c11a58a7bbbe01a7482c0bf))
* show an indicative cost per agent in the :agent-usage view (DEVDESK-47) ([#308](https://github.com/anthnel/devdesk/issues/308)) ([39204bf](https://github.com/anthnel/devdesk/commit/39204bfd3d3c8f0c48152f5b873f63dda136b6df))
* show each agent's state, tool calls and duration in the :agent-usage view (DEVDESK-46) ([#307](https://github.com/anthnel/devdesk/issues/307)) ([0026b6b](https://github.com/anthnel/devdesk/commit/0026b6b6d097a8479ba5b3892a0fb04fa89860f0))
* show each agent's token mix as a coloured bar (DEVDESK-98 part 1) ([#321](https://github.com/anthnel/devdesk/issues/321)) ([0df012e](https://github.com/anthnel/devdesk/commit/0df012ea36c0958bde9553ecccba9c1866611af3))
* show per-row activity and failure dots in ws (DEVDESK-144) ([#334](https://github.com/anthnel/devdesk/issues/334)) ([a59eef3](https://github.com/anthnel/devdesk/commit/a59eef3bfafd4ace27d190002b89e310aa012e0b))
* show the agent tree of a session in the :agent-usage view (DEVDESK-42) ([#303](https://github.com/anthnel/devdesk/issues/303)) ([df35c82](https://github.com/anthnel/devdesk/commit/df35c82d85afddd60dd58450b899556920610887))
* show the agent-usage session breadcrumb on the top frame border (DEVDESK-134) ([#323](https://github.com/anthnel/devdesk/issues/323)) ([99bafee](https://github.com/anthnel/devdesk/commit/99bafee3d8e9524964c83f72930e68785c0c441a))
* sort agents by tokens and show their share of the session (DEVDESK-43) ([#304](https://github.com/anthnel/devdesk/issues/304)) ([8550c5c](https://github.com/anthnel/devdesk/commit/8550c5c540124d45dc3925bbf8278022a8b0d3bd))
* switch contexts quickly and safely with :ctx completion, ctx - and ctx new (DEVDESK-88) ([#318](https://github.com/anthnel/devdesk/issues/318)) ([7c8fe3f](https://github.com/anthnel/devdesk/commit/7c8fe3fcf67ac6fc10785bd5cf4ac409b9b415ac))


### Bug Fixes

* **containers:** keep known metrics when a fresh list arrives (D84) ([#289](https://github.com/anthnel/devdesk/issues/289)) ([2026927](https://github.com/anthnel/devdesk/commit/202692766be09370b1e2118813ea4ccbcc5273b7))
* **containers:** re-arm the refresh tick so the gauges move on their own (D83) ([#287](https://github.com/anthnel/devdesk/issues/287)) ([b4aa887](https://github.com/anthnel/devdesk/commit/b4aa887d0d7cf6cacf7605835ba8c1a3a78af321))
* keep the header within a narrow terminal (DEVDESK-89 step 6) ([#315](https://github.com/anthnel/devdesk/issues/315)) ([3013f87](https://github.com/anthnel/devdesk/commit/3013f87d439c32c4ca502dc87b6f6a4501c0869e))
* keep the layout whole on very short terminals (DEVDESK-89 step 7) ([#317](https://github.com/anthnel/devdesk/issues/317)) ([cdeb5a4](https://github.com/anthnel/devdesk/commit/cdeb5a43ce81a4171e5743d19685cd5ebef46d21))
* start ws and files deletes as immediate runs (DEVDESK-227) ([#348](https://github.com/anthnel/devdesk/issues/348)) ([69e8f4d](https://github.com/anthnel/devdesk/commit/69e8f4dadc4f28ab21cc685c98e2697ed5fb7452))
* stop sync deleting local-only tags when fetch.pruneTags is set (DEVDESK-135) ([#324](https://github.com/anthnel/devdesk/issues/324)) ([ad01ceb](https://github.com/anthnel/devdesk/commit/ad01cebc04a2c3da254d744ec51a6b95ef9ab762))
* stop views listing ctrl+p now that the header always shows the hint (DEVDESK-124) ([#320](https://github.com/anthnel/devdesk/issues/320)) ([f55e923](https://github.com/anthnel/devdesk/commit/f55e9230928263fbf95c3b1e7a7d1c3a85d1999d))
* **workspaces:** open the remote through explorer.exe under WSL ([#291](https://github.com/anthnel/devdesk/issues/291)) ([e7d2377](https://github.com/anthnel/devdesk/commit/e7d2377481af60031b7122eb07cd064e82267c42))
* **workspaces:** refresh the Last Tag column after a sync (D82) ([#285](https://github.com/anthnel/devdesk/issues/285)) ([2c674fc](https://github.com/anthnel/devdesk/commit/2c674fcddbb34b2081ffa89ea454ae7990bfe75e))

## [1.7.0](https://github.com/anthnel/devdesk/compare/v1.6.0...v1.7.0) (2026-10-08)


### Features

* **files:** add a file browser view and a path picker for forms (§3.95) ([#277](https://github.com/anthnel/devdesk/issues/277)) ([2bda519](https://github.com/anthnel/devdesk/commit/2bda519b88c7d44a57b53dfe59bbd199e335a24d))
* **theme:** add app.gauge_glyph to opt into a solid block gauge fill (§3.96) ([#278](https://github.com/anthnel/devdesk/issues/278)) ([9ab452a](https://github.com/anthnel/devdesk/commit/9ab452a2c909d8956701a30e2ae696380cb95802))
* **workspaces:** show every tag on the nearest tagged commit (§3.99) ([#283](https://github.com/anthnel/devdesk/issues/283)) ([62faea0](https://github.com/anthnel/devdesk/commit/62faea0474643b935ae77c8e49c327d6643da3f1))


### Bug Fixes

* **clipboard:** set the WSL clipboard through PowerShell, not clip.exe (D81) ([#282](https://github.com/anthnel/devdesk/issues/282)) ([a2fcae5](https://github.com/anthnel/devdesk/commit/a2fcae514aad5c25c491bf93c3707bfdf9d10e10))
* **clipboard:** write UTF-16 to clip.exe under WSL so accents survive (D81) ([#281](https://github.com/anthnel/devdesk/issues/281)) ([28e3364](https://github.com/anthnel/devdesk/commit/28e33640010e9df1b86f7c2baf51a1fb20c540fd))
* **git:** force the C locale, and stop sync pruning local tags (D80) ([#279](https://github.com/anthnel/devdesk/issues/279)) ([ddf0e42](https://github.com/anthnel/devdesk/commit/ddf0e425a29a4b709d1ee8cf9d21dae4fee89cfc))
* **git:** show ssh's reason when a clone cannot reach the remote (D78) ([#275](https://github.com/anthnel/devdesk/issues/275)) ([d432a60](https://github.com/anthnel/devdesk/commit/d432a60b50699e6641dd175820722ecea27fa687))

## [1.6.0](https://github.com/anthnel/devdesk/compare/v1.5.0...v1.6.0) (2026-09-26)


### Features

* **config:** Y copies the Claude Code MCP connect command (§3.77) ([#260](https://github.com/anthnel/devdesk/issues/260)) ([a43b640](https://github.com/anthnel/devdesk/commit/a43b640b47f883dd500d66e65eddcb1751060cb5))
* **containers:** J jumps to the container's image in the OCI view (§3.89) ([#263](https://github.com/anthnel/devdesk/issues/263)) ([add6c85](https://github.com/anthnel/devdesk/commit/add6c85b572bc47bd20c44a05b36bf1e9a594e68))
* **explorer:** index the forge per context (§3.93) ([#269](https://github.com/anthnel/devdesk/issues/269)) ([2587ea1](https://github.com/anthnel/devdesk/commit/2587ea128f08920f1632e014159ed4d8aa88b0ab))
* **images:** detect and act on image updates (floating base re-scan, Update column, G to update) ([#262](https://github.com/anthnel/devdesk/issues/262)) ([38c816f](https://github.com/anthnel/devdesk/commit/38c816fff1cd903ff43fa223bfeecf3bc1a47cfe))
* **images:** the Update column shows an icon for every state but a newer patch ([#264](https://github.com/anthnel/devdesk/issues/264)) ([31864a4](https://github.com/anthnel/devdesk/commit/31864a4ad44b13377f1c06886f72ed37e2566ac9))
* **registries:** say when a registry password sits unencrypted in the auth file (§3.68) ([#265](https://github.com/anthnel/devdesk/issues/265)) ([efc8071](https://github.com/anthnel/devdesk/commit/efc807120ea76bbf3dcfdd374062d023e85a9a6b))
* **scan:** a CFG column for misconfigurations, in ws, :sec and images ([#259](https://github.com/anthnel/devdesk/issues/259)) ([3a3069a](https://github.com/anthnel/devdesk/commit/3a3069afa2e281865c58b3e404dc39cc6f8baf6e))
* **scan:** detect and remediate Kubernetes manifest misconfigurations ([#257](https://github.com/anthnel/devdesk/issues/257)) ([80ab2fc](https://github.com/anthnel/devdesk/commit/80ab2fc09de2ec5ec54a434c1326d4812485d880))
* **scan:** report what a COPY of the whole build context takes into the image (§3.81) ([#261](https://github.com/anthnel/devdesk/issues/261)) ([a311584](https://github.com/anthnel/devdesk/commit/a311584d838788171a20d24a30493bb6ce1b5c85))
* **scan:** scanner categories, per-tool settings, and a Tools tab ([#258](https://github.com/anthnel/devdesk/issues/258)) ([74228ce](https://github.com/anthnel/devdesk/commit/74228ceefdaaa76d7cfdf3c572d9d66a0ea8b01d))
* **trust:** verify image signatures before pulling (§3.82) ([#266](https://github.com/anthnel/devdesk/issues/266)) ([d2ea013](https://github.com/anthnel/devdesk/commit/d2ea013db9f48fe440633c802df7cba6e3a0de73))
* **workspaces:** Git Status cell follows starship's markers (§3.94) ([#271](https://github.com/anthnel/devdesk/issues/271)) ([38da42b](https://github.com/anthnel/devdesk/commit/38da42baa4f4f3e9666d5cfc843510b67971f781))


### Bug Fixes

* **app:** reuse a recent forge index instead of walking again (D77) ([#274](https://github.com/anthnel/devdesk/issues/274)) ([efe098b](https://github.com/anthnel/devdesk/commit/efe098bb522535c5b12f4ce981428facbaf5cb39))
* **app:** switch context before drawing the interface (D76) ([#273](https://github.com/anthnel/devdesk/issues/273)) ([3e32a48](https://github.com/anthnel/devdesk/commit/3e32a48efe9ade2b030e8b3efb6020eee9f77e01))
* **containers:** size the Ports column to its content, not the surplus (D74) ([#272](https://github.com/anthnel/devdesk/issues/272)) ([e9d2418](https://github.com/anthnel/devdesk/commit/e9d241896037334279994fcfe2b99e91f7b6f46b))
* **scan:** misconfiguration AVDID fallback, extend the remediation catalog ([#254](https://github.com/anthnel/devdesk/issues/254)) ([eed82c3](https://github.com/anthnel/devdesk/commit/eed82c3e72954e98d7757121237653338e25d2dc))
* **workspaces:** sync follows tags the remote rewrote (D74) ([#270](https://github.com/anthnel/devdesk/issues/270)) ([8184fcd](https://github.com/anthnel/devdesk/commit/8184fcdfec77deb3c8af34383f6da858450df6a8))

## [1.5.0](https://github.com/anthnel/devdesk/compare/v1.4.0...v1.5.0) (2026-09-21)


### Features

* **app:** shift+tab moves left in the command-line view picker ([#246](https://github.com/anthnel/devdesk/issues/246)) ([4421c5a](https://github.com/anthnel/devdesk/commit/4421c5ae245f4dca60dfb433cc1bccb22e0c02e3))
* **app:** Tab in the command line opens a circular view picker ([#242](https://github.com/anthnel/devdesk/issues/242)) ([33dc534](https://github.com/anthnel/devdesk/commit/33dc534cba8261d83f7e8be5fa989456920a3a90))
* **forward:** named *.localhost routes on one shared proxy ([#236](https://github.com/anthnel/devdesk/issues/236)) ([62a5c30](https://github.com/anthnel/devdesk/commit/62a5c3016caeb99df02c73e7e8555485f58c7054))
* **forward:** persist forwards and reopen them at launch ([#235](https://github.com/anthnel/devdesk/issues/235)) ([79eabec](https://github.com/anthnel/devdesk/commit/79eabecf36c608282c7e6bce3372920a4b8de193))
* **mcp:** expose a misconfiguration's fix data — §3.78 phase A ([#249](https://github.com/anthnel/devdesk/issues/249)) ([6c12e0c](https://github.com/anthnel/devdesk/commit/6c12e0c77d1c116799bf0bf973717f282ed5aed7))
* **remediation:** a finite catalog of misconfiguration fixes, starting with the root user ([#250](https://github.com/anthnel/devdesk/issues/250)) ([775bc23](https://github.com/anthnel/devdesk/commit/775bc23b54677abc95987ca4294bbce8140f1e89))
* **security:** apply a built-in misconfiguration fix from the Misconfigurations tab ([#251](https://github.com/anthnel/devdesk/issues/251)) ([f35b105](https://github.com/anthnel/devdesk/commit/f35b105d62e8ed96c166ff68e69a16be3c4ce4f0))
* **security:** base image remediation — Remediation tab, tag candidates, Dockerfile rewrite ([#248](https://github.com/anthnel/devdesk/issues/248)) ([315fe8b](https://github.com/anthnel/devdesk/commit/315fe8bb5bdba79bc98a73ccde5059d3fdb5579f))
* **ui:** align the title rule and command line, add a blank line under it ([#241](https://github.com/anthnel/devdesk/issues/241)) ([58b00e8](https://github.com/anthnel/devdesk/commit/58b00e86da1adf0876ee908bdbbc37bbcf4800c2))


### Bug Fixes

* **app:** naming the security view lands on its inventory, not on a list's scan ([#247](https://github.com/anthnel/devdesk/issues/247)) ([10cb3b1](https://github.com/anthnel/devdesk/commit/10cb3b1dc6ac02dba72f4f6c0ff85cf3f0871b42))
* **dashboard:** keep line-ending status icons fully coloured ([#243](https://github.com/anthnel/devdesk/issues/243)) ([72e8368](https://github.com/anthnel/devdesk/commit/72e8368c9a0c46660750fcee779a8d5a7717b81d))
* **forward:** review findings on named routes, and focus the form on Type ([#237](https://github.com/anthnel/devdesk/issues/237)) ([4ff3aba](https://github.com/anthnel/devdesk/commit/4ff3aba818c50c20baa1865d7e7d68ddff3e70af))
* **status:** read status.timeout as seconds, not nanoseconds ([#239](https://github.com/anthnel/devdesk/issues/239)) ([e064575](https://github.com/anthnel/devdesk/commit/e0645754579de1ba722d5aedfc031f73b51b88e4))


### Refactoring

* **forward:** drop the Source column and the label behind it ([#231](https://github.com/anthnel/devdesk/issues/231)) ([8c6fda7](https://github.com/anthnel/devdesk/commit/8c6fda7f534d0d62336c1d55a318f9f839b12c93))

## [1.4.0](https://github.com/anthnel/devdesk/compare/v1.3.0...v1.4.0) (2026-09-19)


### Features

* **forge:** carry bytes and the execute bit in an initial commit ([#216](https://github.com/anthnel/devdesk/issues/216)) ([ae1017e](https://github.com/anthnel/devdesk/commit/ae1017ef4135a4ddd4a7ebef2671c279e77b791a))
* **jobsview:** color a done run/item green like status's IconOK ([#213](https://github.com/anthnel/devdesk/issues/213)) ([f552258](https://github.com/anthnel/devdesk/commit/f55225889fa58124d37ed2d837e5309009380638))
* **template:** add the repository template catalog and its fetchers ([#217](https://github.com/anthnel/devdesk/issues/217)) ([fe48c5b](https://github.com/anthnel/devdesk/commit/fe48c5be7b535908f91f53a2c9050903c4293239))
* **templates:** add the :templates view over the catalog ([#219](https://github.com/anthnel/devdesk/issues/219)) ([5823b4d](https://github.com/anthnel/devdesk/commit/5823b4d77b5aa3fbfc1db37d17a7c62c93e8730d))
* **templates:** cache what a template's source returns and sync it with F ([#225](https://github.com/anthnel/devdesk/issues/225)) ([3daadb4](https://github.com/anthnel/devdesk/commit/3daadb410b6442737bcb1c73fd3d8f1549f06fde))
* **templates:** choose a template when creating a repository ([#220](https://github.com/anthnel/devdesk/issues/220)) ([05db616](https://github.com/anthnel/devdesk/commit/05db61691df9bc3c461f4b5ac24dc514a0c6f676))
* **templates:** scan a template before it is copied into every repository ([#221](https://github.com/anthnel/devdesk/issues/221)) ([75c7740](https://github.com/anthnel/devdesk/commit/75c7740b5f53243b868c2dab40ce7394ab5d2be9))
* **templates:** show how old a template's cached copy is ([#226](https://github.com/anthnel/devdesk/issues/226)) ([0c7fa72](https://github.com/anthnel/devdesk/commit/0c7fa720338e5e89e805d79a5ac51295fba9964e))
* **ws:** add Last Tag column beside Git Status ([#214](https://github.com/anthnel/devdesk/issues/214)) ([f42e8b1](https://github.com/anthnel/devdesk/commit/f42e8b1f2d7d7d7966d48a7f9a16e49b5a76879a))


### Bug Fixes

* **app:** route a template sync's messages to the jobs registry ([#227](https://github.com/anthnel/devdesk/issues/227)) ([2472922](https://github.com/anthnel/devdesk/commit/2472922d3fa67f3d082113239ba25721a1f2eda0))
* explorer footer, not body, reports a missing session (D70) ([#210](https://github.com/anthnel/devdesk/issues/210)) ([e8567ea](https://github.com/anthnel/devdesk/commit/e8567ea623cb4fe797337d803064ca042d99ebbe))
* limit new project/group visibility to the parent's (D71, GitLab only) ([#212](https://github.com/anthnel/devdesk/issues/212)) ([ee09b41](https://github.com/anthnel/devdesk/commit/ee09b41a4d7bf574aaec58ee9566cfaad7da7fa9))
* **scan:** give gitleaks a real report file on native Windows ([#223](https://github.com/anthnel/devdesk/issues/223)) ([46a99f6](https://github.com/anthnel/devdesk/commit/46a99f60a27f75654b70de7ab29209c8827a7a2c))
* **template:** bound fetching while reading, tolerate bad catalog entries, keep secrets off http ([#222](https://github.com/anthnel/devdesk/issues/222)) ([7a818f2](https://github.com/anthnel/devdesk/commit/7a818f26fa4bb0c86bf2c1442ce2dc248b3eb73f))
* **ui:** space out fields in the template creation/edit form ([#224](https://github.com/anthnel/devdesk/issues/224)) ([19c2923](https://github.com/anthnel/devdesk/commit/19c2923516b3f4578e8259a8e2c05a248b907704))

## [1.3.0](https://github.com/anthnel/devdesk/compare/v1.2.0...v1.3.0) (2026-09-14)


### Features

* add `dk setup` wizard to guide new context creation ([#205](https://github.com/anthnel/devdesk/issues/205)) ([18bce2f](https://github.com/anthnel/devdesk/commit/18bce2f6f681289ec1d85ee23b73afdcc6e4152e))
* support Podman as an alternative container engine ([ca6fecc](https://github.com/anthnel/devdesk/commit/ca6fecc0f6ba6bed6e99bc548d220453a81023af))
* view a monitor's SSL certificate chain in the document viewer ([85fb52a](https://github.com/anthnel/devdesk/commit/85fb52ab22e5a187416814380bd5781611927eeb))
* **workspaces:** add fuzzy-find navigation (g) ([#207](https://github.com/anthnel/devdesk/issues/207)) ([d1229f5](https://github.com/anthnel/devdesk/commit/d1229f58b4ba56d31c43d2361dafb46e7a6114a0))


### Bug Fixes

* correct podman --format templates for Info, NetworkLS, SystemDF ([#202](https://github.com/anthnel/devdesk/issues/202)) ([88afb2f](https://github.com/anthnel/devdesk/commit/88afb2f0f09958d338623dfd24e62f488d2e9069))
* drop icons from the Health box's stacked count nodes ([#206](https://github.com/anthnel/devdesk/issues/206)) ([8ca1b61](https://github.com/anthnel/devdesk/commit/8ca1b614290d764f4d133d9dc61e137bb1e78a45))
* qualify Docker Hub pull references with docker.io under podman ([#203](https://github.com/anthnel/devdesk/issues/203)) ([6b5f1a9](https://github.com/anthnel/devdesk/commit/6b5f1a97de091bd3797a94aa808f7f253331453b))
* replace truncated "gl" placeholder in the registry alias field ([#204](https://github.com/anthnel/devdesk/issues/204)) ([3debe52](https://github.com/anthnel/devdesk/commit/3debe52b950f6cef551b733ee293ac96d21df264))

## [1.2.0](https://github.com/anthnel/devdesk/compare/v1.1.1...v1.2.0) (2026-09-11)


### Features

* :sec et oci lisent le registre (§3.58, poste 4) ([#153](https://github.com/anthnel/devdesk/issues/153)) ([434dd61](https://github.com/anthnel/devdesk/commit/434dd619d85ae441c351e81ad3b3a6d8bc4d4a26))
* :sec nomme une image par son alias de registry (§3.36) ([#93](https://github.com/anthnel/devdesk/issues/93)) ([b593c8a](https://github.com/anthnel/devdesk/commit/b593c8aad3dfaf9eb892dd5c4861e73fcad58871))
* `ws` affiche une icône par ligne, la colonne Type disparaît (§3.46) ([#120](https://github.com/anthnel/devdesk/issues/120)) ([a7468c3](https://github.com/anthnel/devdesk/commit/a7468c390756ca6f0068dd3d96d29ac82743fdf8))
* `ws` montre les fichiers cachés, si on le lui demande (§3.31) ([#78](https://github.com/anthnel/devdesk/issues/78)) ([95c8597](https://github.com/anthnel/devdesk/commit/95c85972baca8b77f71ed8656ed18d7faf0e421c))
* a configuration view (phase 1) ([#43](https://github.com/anthnel/devdesk/issues/43)) ([8174907](https://github.com/anthnel/devdesk/commit/8174907155552838672bca8e88c8bc01db46693f))
* a row says what it is, and what is happening to it (§3.22) ([#65](https://github.com/anthnel/devdesk/issues/65)) ([65bdaaf](https://github.com/anthnel/devdesk/commit/65bdaaf4ce5b0c4470f2e7f36fbf63a97d54e0b9))
* a viewer for documents — text, JSON, XML and logs (§3.25) ([#67](https://github.com/anthnel/devdesk/issues/67)) ([90f178e](https://github.com/anthnel/devdesk/commit/90f178e55757e72960cd83a59ec2b282f2bd9f4d))
* add internal/ui/datatable, the shared table mechanism (§2 step 1) ([#34](https://github.com/anthnel/devdesk/issues/34)) ([71217aa](https://github.com/anthnel/devdesk/commit/71217aa24669c033dfdbee6ce18219038fccaed3))
* ajouter les thèmes Windows Terminal Dark+, Nord, Solarized et GitHub ([#169](https://github.com/anthnel/devdesk/issues/169)) ([1a4e511](https://github.com/anthnel/devdesk/commit/1a4e5117c935ed89b78834d77238837567f15c78))
* cache discovered group members and refresh them on demand (§3.8 step 3) ([#32](https://github.com/anthnel/devdesk/issues/32)) ([654186b](https://github.com/anthnel/devdesk/commit/654186bda0f43bd83cf04d2f5c59ce7943a75f5a))
* colour in table cells — datatable renders its own rows ([#54](https://github.com/anthnel/devdesk/issues/54)) ([02cf0e7](https://github.com/anthnel/devdesk/commit/02cf0e72feda821d93418067e5b7f913a935a935))
* créer et supprimer sont du travail, pas une attente (§3.59) ([#159](https://github.com/anthnel/devdesk/issues/159)) ([c8ac122](https://github.com/anthnel/devdesk/commit/c8ac122d3c4e2992527e791fe33588fc0037d934))
* ct filtre par état et raccourcit sa colonne Ports (§3.27) ([#97](https://github.com/anthnel/devdesk/issues/97)) ([7aa33c4](https://github.com/anthnel/devdesk/commit/7aa33c4e733d0e159a464f77e72d1ac5525c6d04))
* dashboard Network box shows a cumulative total and RX/TX errors ([#191](https://github.com/anthnel/devdesk/issues/191)) ([3591952](https://github.com/anthnel/devdesk/commit/3591952a901de9bb4473f7398c2a402be3221648))
* datatable — chaque colonne déclare sa nature, et les largeurs suivent (§3.45, D61) ([#127](https://github.com/anthnel/devdesk/issues/127)) ([64569bf](https://github.com/anthnel/devdesk/commit/64569bfc7fd682a8acc4f7c3d56d1749046be692))
* forge.Vocabulary, et aucune vue n'écrit le nom d'une forge (§3.6 étape 5) ([#104](https://github.com/anthnel/devdesk/issues/104)) ([22a5e45](https://github.com/anthnel/devdesk/commit/22a5e4589dd0b42f6037c14041a11af0f261e6e1))
* give a registry a slug, a kind and a provider (§3.8 step 1) ([#29](https://github.com/anthnel/devdesk/issues/29)) ([4c9bbc1](https://github.com/anthnel/devdesk/commit/4c9bbc1fcccc40e12ed740ddba03aa3b5bf16ec0))
* groundwork for the explorer clone rework (§3.16) — pagination, lighter discovery, selection model ([#49](https://github.com/anthnel/devdesk/issues/49)) ([7fab711](https://github.com/anthnel/devdesk/commit/7fab711126911f75f466823174d4204bc77889cd))
* internal/forge, ce que DevDesk demande à une plateforme (§3.6 étape 1) ([#100](https://github.com/anthnel/devdesk/issues/100)) ([3c0c8af](https://github.com/anthnel/devdesk/commit/3c0c8afd3ce82cb71b1f2883a8a3b24a3d6f9655))
* internal/forge/github, le second backend (§3.6 étape 7) ([#106](https://github.com/anthnel/devdesk/issues/106)) ([5e500d6](https://github.com/anthnel/devdesk/commit/5e500d68492d54367ceae79a4788bd72d174ca59))
* internal/forge/gitlab, le backend GitLab derrière l'interface (§3.6 étape 2) ([#101](https://github.com/anthnel/devdesk/issues/101)) ([d481d3f](https://github.com/anthnel/devdesk/commit/d481d3f88f8aa4a00134d1408405b6796f76f763))
* internal/netcheck répond à des questions plutôt qu'à des outils (PR 1/4) ([#87](https://github.com/anthnel/devdesk/issues/87)) ([096b575](https://github.com/anthnel/devdesk/commit/096b575146a59bab2e3c7fe8f318a5b47213eac4))
* K arrête un run ou une de ses cibles (§3.58, poste 8) ([#157](https://github.com/anthnel/devdesk/issues/157)) ([46b012c](https://github.com/anthnel/devdesk/commit/46b012c1d67c9e0b2e5c763fbefdee9634bd6a5d))
* l'explorer passe aux icônes, et le thème apprend à les colorer (§3.56) ([#147](https://github.com/anthnel/devdesk/issues/147)) ([f636bd9](https://github.com/anthnel/devdesk/commit/f636bd9b6a2e4d849b5a9db7a4d4c783a96c2421))
* l'onglet de configuration suit la forge du contexte (§3.6 étape 6) ([#105](https://github.com/anthnel/devdesk/issues/105)) ([7514903](https://github.com/anthnel/devdesk/commit/751490317767ddd8fa7147610ba244cd4b2dc73d))
* l'onglet Topology devient Interfaces, et la route passe dans Diagnostics (D57, D58, §3.44) ([#117](https://github.com/anthnel/devdesk/issues/117)) ([6c2e7b4](https://github.com/anthnel/devdesk/commit/6c2e7b431de42e9b9a30b391583577e58c8e743c))
* la colonne CI et le cinquième onglet (§3.42, PR 3/3) ([#134](https://github.com/anthnel/devdesk/issues/134)) ([49a224d](https://github.com/anthnel/devdesk/commit/49a224d54b8312f3875f67007df67915fd425e82))
* la colonne CI se colore, et migre vers :sec (§3.52) ([#140](https://github.com/anthnel/devdesk/issues/140)) ([5a96cdd](https://github.com/anthnel/devdesk/commit/5a96cdddc234825dfe2f80680dd2b4be185a3d2f))
* la section `gitlab:` devient `forge:` avec un `type:` (§3.6 étape 4) ([#103](https://github.com/anthnel/devdesk/issues/103)) ([5f55bb1](https://github.com/anthnel/devdesk/commit/5f55bb150770425afac3f6874661647d0d4c6c7a))
* la trace de route est supprimée, `network.tool_image` avec (D57, §3.47) ([#118](https://github.com/anthnel/devdesk/issues/118)) ([5d0502d](https://github.com/anthnel/devdesk/commit/5d0502d7627596c430892e41119afb8dcd0182f3))
* la vue :jobs, et le dashboard compte ce qui tourne (§3.58, poste 6) ([#155](https://github.com/anthnel/devdesk/issues/155)) ([f88f79a](https://github.com/anthnel/devdesk/commit/f88f79adce62fcb53411a4dbbdff68713d3f3350))
* la vue config nomme son contexte, et l'onglet docker devient network (D48, §3.34) ([#91](https://github.com/anthnel/devdesk/issues/91)) ([998d298](https://github.com/anthnel/devdesk/commit/998d298b49937bf39329c9f370c57cec46c84d69))
* le binaire connaît sa version, publiée par release-please + goreleaser (§3.62) ([#175](https://github.com/anthnel/devdesk/issues/175)) ([8387ed0](https://github.com/anthnel/devdesk/commit/8387ed03a7cf920127bc87a4d3e555f9f2db4a90))
* le clone devient un run du registre (§3.58, poste 7) ([#156](https://github.com/anthnel/devdesk/issues/156)) ([b227b44](https://github.com/anthnel/devdesk/commit/b227b44f23a1b72c19dafe022bb48d2d523374d6))
* le pull d'une image devient un job, et la table Images l'affiche (§3.60) ([#166](https://github.com/anthnel/devdesk/issues/166)) ([6109ff2](https://github.com/anthnel/devdesk/commit/6109ff216f532d877d1372be17dc87e7e3a289d0))
* le réglage plumber, visible et inerte (§3.42, PR 1/3) ([#132](https://github.com/anthnel/devdesk/issues/132)) ([a612cb2](https://github.com/anthnel/devdesk/commit/a612cb214e1c20f524e2374270c6f313c973bbc4))
* le viewer colore Markdown, Dockerfile et shell, et rend le Markdown (§3.32) ([#82](https://github.com/anthnel/devdesk/issues/82)) ([00b4def](https://github.com/anthnel/devdesk/commit/00b4def5733bccb594db618d141b10423ebfdd19))
* le viewer numérote ses lignes, va à l'une d'elles, et sait compter la casse (§3.53) ([#142](https://github.com/anthnel/devdesk/issues/142)) ([c1b71da](https://github.com/anthnel/devdesk/commit/c1b71da35c78f39f1fc03e76d1f1eec6dba2f60b))
* les commandes deviennent `git-auth` et `git-explorer` (§3.6 étape 8, entrée close) ([#107](https://github.com/anthnel/devdesk/issues/107)) ([a6ca2df](https://github.com/anthnel/devdesk/commit/a6ca2dfc078bf762eb53e1b39999263a87a10c91))
* les neuf autres vues grisent leurs raccourcis (§3.48, Rule 130) ([#124](https://github.com/anthnel/devdesk/issues/124)) ([8e21822](https://github.com/anthnel/devdesk/commit/8e218229260472b10b3573190d9305ec5d366bb1))
* net — `K` dit quand elle ne peut pas tuer, et les adresses se séparent par famille (§3.49) ([#125](https://github.com/anthnel/devdesk/issues/125)) ([548902e](https://github.com/anthnel/devdesk/commit/548902e129bc924015c93ad4f2c42f132e6c9d5f))
* netcheck explique ses constats, sans modèle (PR 2/4) ([#88](https://github.com/anthnel/devdesk/issues/88)) ([41ff120](https://github.com/anthnel/devdesk/commit/41ff12074a243baf12e0f05fa0868e8c16d418e6))
* netdiag répond à des questions, pas à des outils (§3.33, PR 3/4) ([#89](https://github.com/anthnel/devdesk/issues/89)) ([adc6e9f](https://github.com/anthnel/devdesk/commit/adc6e9fdbb665144e1512c47331034d4db917f7b))
* one key, one meaning — the action vocabulary lands (§3.26) ([#71](https://github.com/anthnel/devdesk/issues/71)) ([a6189bb](https://github.com/anthnel/devdesk/commit/a6189bbfc869075efb809b051acd92ae12dd33ab))
* open command mode with alt+: from anywhere, including a text field ([#16](https://github.com/anthnel/devdesk/issues/16)) ([42f005c](https://github.com/anthnel/devdesk/commit/42f005c61eed42592fe9a87f3c45068fdad3c220))
* open the registry browser from cache, and drill into groups (§3.8 steps 5, 6) ([#33](https://github.com/anthnel/devdesk/issues/33)) ([496418b](https://github.com/anthnel/devdesk/commit/496418bc0427eae2c2a5463cc19b1d86c831256d))
* pick a group detector by declared provider (§3.8 step 4) ([#31](https://github.com/anthnel/devdesk/issues/31)) ([e8e39e7](https://github.com/anthnel/devdesk/commit/e8e39e75b9e3fd0d4e7adc9a56de74ee5183f788))
* plumber tourne, et le cache retient le score (§3.42, PR 2/3) ([#133](https://github.com/anthnel/devdesk/issues/133)) ([dd6bb8b](https://github.com/anthnel/devdesk/commit/dd6bb8b60df42a77c863886ba8e7b3c7985acb84))
* remove the OCI connectivity-test feature and its config option ([#172](https://github.com/anthnel/devdesk/issues/172)) ([61eff37](https://github.com/anthnel/devdesk/commit/61eff3741bf9da3934ab1e38699cda632ec59892))
* scope the scan caches to their configuration context ([#41](https://github.com/anthnel/devdesk/issues/41)) ([dc93876](https://github.com/anthnel/devdesk/commit/dc938769c43259e9360cb61d8ba1374482440eb0))
* secrets are shown where they are found, and table text takes the theme ([#63](https://github.com/anthnel/devdesk/issues/63)) ([c44b0ae](https://github.com/anthnel/devdesk/commit/c44b0ae279c5f2c9d6338fea51c2b4a57eed6358))
* security becomes an inventory (phase 2) ([#44](https://github.com/anthnel/devdesk/issues/44)) ([6f409c6](https://github.com/anthnel/devdesk/commit/6f409c68956b58035dfe222c5827a1b28dadb4a5))
* store every secret in the host secret manager (§3.9) ([#25](https://github.com/anthnel/devdesk/issues/25)) ([9610ff0](https://github.com/anthnel/devdesk/commit/9610ff055341389793f0eeda19eef6ade027f369))
* the dashboard says what to do about it (§3.24) ([#66](https://github.com/anthnel/devdesk/issues/66)) ([78832b6](https://github.com/anthnel/devdesk/commit/78832b6b4500ccad90e869a9bc2a9baf8fe2576c))
* the dashboard stops reflowing, and gains resource charts (§3.19) ([#62](https://github.com/anthnel/devdesk/issues/62)) ([82ed2f3](https://github.com/anthnel/devdesk/commit/82ed2f3f5be37dd82f003780ffc021503f82396e))
* the explorer clones what you select (§3.16) ([#50](https://github.com/anthnel/devdesk/issues/50)) ([c85af3f](https://github.com/anthnel/devdesk/commit/c85af3f44ef18741927c0df6ed5d3f9589533a0e))
* the keyboard gets a declared vocabulary, and ctrl+p opens the palette (§3.26) ([#70](https://github.com/anthnel/devdesk/issues/70)) ([2dbbda4](https://github.com/anthnel/devdesk/commit/2dbbda409f3643ed1fb17a7fd406eef80c7bf71c))
* the viewer colors YAML and TOML, and a search says where it matched (§3.29) ([#76](https://github.com/anthnel/devdesk/issues/76)) ([fe5479f](https://github.com/anthnel/devdesk/commit/fe5479fcee784fe7e555ed4e83c17a99f9f4bea1))
* un message de footer, trois niveaux, un composant (§3.30) ([#77](https://github.com/anthnel/devdesk/issues/77)) ([cb61ac6](https://github.com/anthnel/devdesk/commit/cb61ac6c538cf3205de4ea268cd0e6945b892463))
* un raccourci sans objet est grisé, pas supprimé (§3.48, Rule 130) ([#122](https://github.com/anthnel/devdesk/issues/122)) ([78ca7be](https://github.com/anthnel/devdesk/commit/78ca7be7c395f732f07e4535c67741a7d21aab8e))
* un registre de travaux, et une seule chaîne de spinner (§3.58, poste 2) ([#151](https://github.com/anthnel/devdesk/issues/151)) ([421cd3f](https://github.com/anthnel/devdesk/commit/421cd3fcfaa6962b73bbcb980cb40344ccf085d0))
* un serveur MCP en lecture seule — `dk mcp` (§3.38) ([#111](https://github.com/anthnel/devdesk/issues/111)) ([bfed637](https://github.com/anthnel/devdesk/commit/bfed6373dca58dc683d9f800aa29ea8cccefedcf))
* une checkbox décide du mode client-serveur Trivy, pas le champ d'adresse ([#171](https://github.com/anthnel/devdesk/issues/171)) ([fdf0e31](https://github.com/anthnel/devdesk/commit/fdf0e31bd12eb6965c69f80fbd05e1eb0606bde9))
* viewer — Y copies content, O opens in the configured IDE ([#184](https://github.com/anthnel/devdesk/issues/184)) ([14b1007](https://github.com/anthnel/devdesk/commit/14b10078306d04828f89c74c69742ca25d251533))
* workspaces reconciles what exists (§3.17) ([#51](https://github.com/anthnel/devdesk/issues/51)) ([b017db7](https://github.com/anthnel/devdesk/commit/b017db7e361049ec1c8416529aea99478af8457a))
* ws et :sec colorent leur colonne d'icône (§3.57) ([#149](https://github.com/anthnel/devdesk/issues/149)) ([c7b6677](https://github.com/anthnel/devdesk/commit/c7b6677ad475a1d8b1a5bdbd92750cec49a48fd6))
* ws lit le registre, et son compteur passe au footer (§3.58, poste 3) ([#152](https://github.com/anthnel/devdesk/issues/152)) ([23ad505](https://github.com/anthnel/devdesk/commit/23ad5059743954d0bbd47ec9011be9279bb6da08))
* Y copie le chemin de la ligne sélectionnée dans ws (§3.35) ([#92](https://github.com/anthnel/devdesk/issues/92)) ([97392e6](https://github.com/anthnel/devdesk/commit/97392e69267cb46dc612621a85a46fa4053674cc))


### Bug Fixes

* :sec ne liste plus une cible qui n'existe plus (§3.37) ([#94](https://github.com/anthnel/devdesk/issues/94)) ([b6b166b](https://github.com/anthnel/devdesk/commit/b6b166bb821544259fc41985e581d5d778607a6c))
* a Docker reference has no scheme (D41) ([#53](https://github.com/anthnel/devdesk/issues/53)) ([3d6be2d](https://github.com/anthnel/devdesk/commit/3d6be2d6afb7ef784e9b4a6967ab0ea0cd81cd70))
* a picker entry is identified by itself, not by its URL (D40) ([#60](https://github.com/anthnel/devdesk/issues/60)) ([c95a9a5](https://github.com/anthnel/devdesk/commit/c95a9a518afbf304edd0c4100c8b34a453df8479))
* a running scan showed nothing — the spinner was styled inside a table cell ([#73](https://github.com/anthnel/devdesk/issues/73)) ([e37c592](https://github.com/anthnel/devdesk/commit/e37c5924b39dded7c3901fdc0f64ade4485253e4))
* align locked checkboxes with the rest of their group ([#45](https://github.com/anthnel/devdesk/issues/45)) ([9882b65](https://github.com/anthnel/devdesk/commit/9882b65dcf4eee3a0a27cd8f9945594131d0dd5f))
* answer the last two defects by removing something (D21, D36) ([#52](https://github.com/anthnel/devdesk/issues/52)) ([4e2c518](https://github.com/anthnel/devdesk/commit/4e2c51867bc4d8a8aee5bfcd9f4ff7d127d492ee))
* background for default theme ([#119](https://github.com/anthnel/devdesk/issues/119)) ([60199b3](https://github.com/anthnel/devdesk/commit/60199b318bfc4d39febf921ad2f0567a67b16254))
* close the five parked defects (D4, D8, D9, D10, D11) ([#19](https://github.com/anthnel/devdesk/issues/19)) ([cb4a982](https://github.com/anthnel/devdesk/commit/cb4a98225bfa3ca32a15ec31ff826a43641d43c2))
* close the four router defects (D15, D16, D17, D18) ([#24](https://github.com/anthnel/devdesk/issues/24)) ([4f49769](https://github.com/anthnel/devdesk/commit/4f4976970874f292d94eb88a418c7b2d1357536d))
* close the three self-contained backlog defects (D7, D2, D5) ([#11](https://github.com/anthnel/devdesk/issues/11)) ([0b1ab78](https://github.com/anthnel/devdesk/commit/0b1ab78d78466ac707133909f678bf1dd77ee3ae))
* docs-build wrote to docs-site/docs-site/site, not docs-site/site ([#181](https://github.com/anthnel/devdesk/issues/181)) ([cb3f134](https://github.com/anthnel/devdesk/commit/cb3f134e40d4f6072e4cd9ddf7e8fefc048e540b))
* **forge/explorer:** hide immediate-deletion checkbox on GitHub ([#177](https://github.com/anthnel/devdesk/issues/177)) ([4853ddd](https://github.com/anthnel/devdesk/commit/4853dddc4ce4b3062feab7cd2c0523cf956fdf40))
* give "Scan Options" the blank line every other section header has ([#28](https://github.com/anthnel/devdesk/issues/28)) ([756bb1b](https://github.com/anthnel/devdesk/commit/756bb1bf2e0ae04e4e1726795591b8655235bc7b))
* Health box no longer tells the same state in two colors ([#192](https://github.com/anthnel/devdesk/issues/192)) ([60d51ff](https://github.com/anthnel/devdesk/commit/60d51ff37832ff44d7804b7c49a144ad54bc0456))
* K opened a modal nobody could see, and it ate the keyboard ([#72](https://github.com/anthnel/devdesk/issues/72)) ([b6f588d](https://github.com/anthnel/devdesk/commit/b6f588d6062c316df24e1dffb088bf247a750d2e))
* l'exemple de token suit la forge, et la garde apprend les préfixes (D54) ([#108](https://github.com/anthnel/devdesk/issues/108)) ([782ec6f](https://github.com/anthnel/devdesk/commit/782ec6f6d384874f23ea0c771fcb083c376241b2))
* l'inventaire :sec attend la réponse des caches avant de dire « rien » (D65) ([#146](https://github.com/anthnel/devdesk/issues/146)) ([a0722b1](https://github.com/anthnel/devdesk/commit/a0722b189225ab0ac4c0d7f6344f49d5a8e82550))
* l'onglet Ports lit la machine, pas la VM Docker (D55, §3.43) ([#114](https://github.com/anthnel/devdesk/issues/114)) ([15e2c00](https://github.com/anthnel/devdesk/commit/15e2c00a9c02269796bf6c0ed2ddf3b76df3ea00))
* la barre de filtre des résultats de security n'était pas dessinée (D45) ([#83](https://github.com/anthnel/devdesk/issues/83)) ([7903dbb](https://github.com/anthnel/devdesk/commit/7903dbb2bdb328ba30df5020cffc97184ef53c0c))
* la boîte Health rend les certificats dans leur vocabulaire (D64) ([#144](https://github.com/anthnel/devdesk/issues/144)) ([08a1307](https://github.com/anthnel/devdesk/commit/08a13074db6e647402006c05f3550a497e4147e7))
* la colonne CI d'un contexte GitHub rendait les mots de GitHub (D66) ([#148](https://github.com/anthnel/devdesk/issues/148)) ([7d525c6](https://github.com/anthnel/devdesk/commit/7d525c6d6041c7d355c7e80100548058df100325))
* la posture écarte aussi les images disparues (D69, suite) ([#162](https://github.com/anthnel/devdesk/issues/162)) ([0f334d6](https://github.com/anthnel/devdesk/commit/0f334d6952a0540b0c6dfe1967d9d21b4925a88d))
* la posture écarte les dépôts disparus, comme l'inventaire (D69) ([#160](https://github.com/anthnel/devdesk/issues/160)) ([bf3f9f6](https://github.com/anthnel/devdesk/commit/bf3f9f60127d6a1ceead15d516a69455a37b6b31))
* la suppression dans workspaces n'était ni marquée ni gardée (§3.23) ([#86](https://github.com/anthnel/devdesk/issues/86)) ([c4cc7c7](https://github.com/anthnel/devdesk/commit/c4cc7c70995e97f5fcd099900ecd4ae5b0eca012))
* la table des ports restait affichée après l'arrêt de Docker (D47) ([#90](https://github.com/anthnel/devdesk/issues/90)) ([aecc6ea](https://github.com/anthnel/devdesk/commit/aecc6ea047103fd8f05c4b8d7cfbbc8b9ead237c))
* le binaire installé porte l'extension que Windows exige (D49) ([#95](https://github.com/anthnel/devdesk/issues/95)) ([59b95e5](https://github.com/anthnel/devdesk/commit/59b95e588d2839d136eac37165a8d3d3e8025d55))
* le cache de scan d'images cesse d'être scopé au contexte (§3.39) ([#110](https://github.com/anthnel/devdesk/issues/110)) ([54d6f3d](https://github.com/anthnel/devdesk/commit/54d6f3dca7227d984823822c424fe419cc1c69a5))
* le CPU Docker du dashboard montait à 1400 %, la RAM sommait des touts différents (D46) ([#85](https://github.com/anthnel/devdesk/issues/85)) ([c7963e8](https://github.com/anthnel/devdesk/commit/c7963e8f52b3400a021180103eec6d5857d294a5))
* le dashboard réclamait « Net Diag », un outil que rien ne détecte (D60) ([#121](https://github.com/anthnel/devdesk/issues/121)) ([d84b0ec](https://github.com/anthnel/devdesk/commit/d84b0ece4c4f110042e7422ff22291c67f6d3753))
* le spinner d'un conteneur qu'on arrête restait figé (D44) ([#81](https://github.com/anthnel/devdesk/issues/81)) ([d2f2e59](https://github.com/anthnel/devdesk/commit/d2f2e591e11a660b6b4393b2c4344812bd7b0ef0))
* le viewer suit dans le viewport, et son pager marche enfin sous Windows (D50, D51) ([#98](https://github.com/anthnel/devdesk/issues/98)) ([73a8b47](https://github.com/anthnel/devdesk/commit/73a8b47a2a73480d2c198debf8ffc7f3093c63de))
* le walk des dépôts imbriqués n'a plus de limite de profondeur (D59) ([#116](https://github.com/anthnel/devdesk/issues/116)) ([3e9c6b9](https://github.com/anthnel/devdesk/commit/3e9c6b9b3a2db713a2537429c03e84dee5e2f00b))
* les trois défauts ouverts de §1.3 — D62, D59, D63 ([#143](https://github.com/anthnel/devdesk/issues/143)) ([1361584](https://github.com/anthnel/devdesk/commit/1361584c664ecc12be29579f987825510add351b))
* make GitLab credential contexts actually isolated ([#8](https://github.com/anthnel/devdesk/issues/8)) ([10a19a1](https://github.com/anthnel/devdesk/commit/10a19a17315ebac18ae55a56843cf6f5333fdeae))
* measure text in columns, not bytes, when truncating and wrapping ([#5](https://github.com/anthnel/devdesk/issues/5)) ([f7f7128](https://github.com/anthnel/devdesk/commit/f7f7128dfe40ab9e592a31bf360f573b11d84a0f))
* mise run install puts dk where it says it does ([#56](https://github.com/anthnel/devdesk/issues/56)) ([3e10f92](https://github.com/anthnel/devdesk/commit/3e10f92b7c933f15021e1b54ec417933ae5135f2))
* never report an image as clean when nothing scanned it (D20, D22) ([#27](https://github.com/anthnel/devdesk/issues/27)) ([8a91c86](https://github.com/anthnel/devdesk/commit/8a91c8617b0fbcaf91d85ae9dfb1ac6ff88e62cb))
* never send credentials to a registry marked anonymous (D12, §3.8 step 2) ([#30](https://github.com/anthnel/devdesk/issues/30)) ([9e5dd8c](https://github.com/anthnel/devdesk/commit/9e5dd8c585b7249f981f6c06d0a3c69ac1dd11a1))
* one scan-option builder, closing D26 ([#40](https://github.com/anthnel/devdesk/issues/40)) ([aa43033](https://github.com/anthnel/devdesk/commit/aa43033e946adf9c62b209d1e8d3e6ec4665ce3b))
* plumber — répertoire, score fractionnaire, et les erreurs vont au log ([#136](https://github.com/anthnel/devdesk/issues/136)) ([5ef0797](https://github.com/anthnel/devdesk/commit/5ef0797a21494ac870287c01db14f7809335e2a3))
* read the configured tool paths and let the source be chosen (D27) ([#42](https://github.com/anthnel/devdesk/issues/42)) ([3131d56](https://github.com/anthnel/devdesk/commit/3131d56d097c3b7b94adfc8558c6006c61a5e119))
* repair Entire hook matchers that no longer matched any tool ([57724eb](https://github.com/anthnel/devdesk/commit/57724eb28b2810f4c37c00730b74248b552c29cb))
* report unplugged interfaces as DOWN in the topology view ([2972046](https://github.com/anthnel/devdesk/commit/29720461fcddf92fc2488a1034665131c311692b))
* serialize Trivy invocations to stop the cache-lock race ([#197](https://github.com/anthnel/devdesk/issues/197)) ([04a0c0c](https://github.com/anthnel/devdesk/commit/04a0c0c07617c65103228d12cfdccff5484942b5))
* status acted on the wrong monitor under a filter (D25) + §2 step 6 — the refactor closes ([#39](https://github.com/anthnel/devdesk/issues/39)) ([e4eda00](https://github.com/anthnel/devdesk/commit/e4eda0000b7cd15f03e032dd5ccc7472f678d4b5))
* tenir `→` empilait le même chemin dans le fil d'Ariane (D43) ([#80](https://github.com/anthnel/devdesk/issues/80)) ([be3ceb1](https://github.com/anthnel/devdesk/commit/be3ceb17d1a5a405d9ab4d8275788c8bb6f5a122))
* the about body opens on a blank line ([#186](https://github.com/anthnel/devdesk/issues/186)) ([bf528d6](https://github.com/anthnel/devdesk/commit/bf528d6da832e5e629d39da1cf191f51e4140d94))
* the background ignored the theme — a var rendered before main() ran (D42) ([#75](https://github.com/anthnel/devdesk/issues/75)) ([55d266e](https://github.com/anthnel/devdesk/commit/55d266eb93fc6985db20d0c73a8cf051656893b0))
* the status view stops advertising settings it does not own ([#59](https://github.com/anthnel/devdesk/issues/59)) ([ec7a4b2](https://github.com/anthnel/devdesk/commit/ec7a4b2ada879e02a07eb9322de4e00c5cfcd4e8))
* un datatable vide reste une table, jamais un message (Rule 139) ([#158](https://github.com/anthnel/devdesk/issues/158)) ([820ab7e](https://github.com/anthnel/devdesk/commit/820ab7ee75f6bedc72aeda49d0a454a89ff9ba28))
* un fichier de règles gitleaks est monté, et un scan qui n'a rien lu n'est plus « propre » (§3.50, D56) ([#129](https://github.com/anthnel/devdesk/issues/129)) ([ca339a9](https://github.com/anthnel/devdesk/commit/ca339a906420f28dbe763d21a72375704c263c1a))
* un membre de registry est une adresse, pas une URL (D39, §3.18) ([#109](https://github.com/anthnel/devdesk/issues/109)) ([044cdb2](https://github.com/anthnel/devdesk/commit/044cdb27613be4147e3808bcfcc3956e50adeae6))
* un scan de ws ne reprend plus l'écran à chaque dépôt (D67) ([#150](https://github.com/anthnel/devdesk/issues/150)) ([37b72e6](https://github.com/anthnel/devdesk/commit/37b72e6e3a0cafa0ae0c09ad0bfe98bda8b98283))
* un scan écrit dans le contexte où il a été lancé (D68) ([#154](https://github.com/anthnel/devdesk/issues/154)) ([cd62621](https://github.com/anthnel/devdesk/commit/cd6262169a62f76635154fe05cbb42fa2c721633))
* workspaces acted on the wrong directory under a filter (D24) + §2 step 5 ([#38](https://github.com/anthnel/devdesk/issues/38)) ([d9092de](https://github.com/anthnel/devdesk/commit/d9092dee2a6f41b3d6a1af8fd09872b45ccd21df))


### Refactoring

* drop gitlab.pull.target_dir ([e0b2c48](https://github.com/anthnel/devdesk/commit/e0b2c4828b73f942838a20de7b91b30a16c6aed7))
* l'onglet Certificates range ses colonnes par nature (§3.54) ([#145](https://github.com/anthnel/devdesk/issues/145)) ([bbe3e2c](https://github.com/anthnel/devdesk/commit/bbe3e2c5b1e6aa41cc6ba48f216d482cb13d700d))
* la colonne Status du clone passe à droite ([#84](https://github.com/anthnel/devdesk/issues/84)) ([512bf3a](https://github.com/anthnel/devdesk/commit/512bf3a482cf8055ea66a05009f776fdf4c08786))
* la forge remplace le client GitLab dans shared.State (§3.6 étape 3) ([#102](https://github.com/anthnel/devdesk/issues/102)) ([d2c841e](https://github.com/anthnel/devdesk/commit/d2c841ebfffa6130a03419b31ad1571c688a3be8))
* move the containers and images tables onto datatable (§2 step 4) ([#37](https://github.com/anthnel/devdesk/issues/37)) ([d0b88a3](https://github.com/anthnel/devdesk/commit/d0b88a37a6cac8d0fd185a52e6f064538d248275))
* move the netdiag ports table onto datatable (§2 step 3) ([#36](https://github.com/anthnel/devdesk/issues/36)) ([d547a13](https://github.com/anthnel/devdesk/commit/d547a1380badebe53b594b8260248abf20b62c1a))
* move the networks and volumes tables onto datatable (§2 step 2) ([#35](https://github.com/anthnel/devdesk/issues/35)) ([367326d](https://github.com/anthnel/devdesk/commit/367326d4998a4c5c4e76bcd39727a5d12097616d))
* remove SBOM generation (§3.14) ([#47](https://github.com/anthnel/devdesk/issues/47)) ([7dea42f](https://github.com/anthnel/devdesk/commit/7dea42fe3ef722a13dedefc5954594400f6a2052))
* split docker/client.go and route every CLI call through a seam ([#6](https://github.com/anthnel/devdesk/issues/6)) ([343886c](https://github.com/anthnel/devdesk/commit/343886c5a6adc399ed48b0de285b587f1d237d34))
* the last four tables move to datatable (§3.21) ([#64](https://github.com/anthnel/devdesk/issues/64)) ([244a0d3](https://github.com/anthnel/devdesk/commit/244a0d36c8b0623ba369d28edb2a6ab288399c44))

## [1.1.1](https://github.com/anthnel/devdesk/compare/v1.0.0...v1.1.1) (2026-09-09)


### Features

* dashboard Network box shows a cumulative total and RX/TX errors ([#191](https://github.com/anthnel/devdesk/issues/191)) ([3591952](https://github.com/anthnel/devdesk/commit/3591952a901de9bb4473f7398c2a402be3221648))
* viewer — Y copies content, O opens in the configured IDE ([#184](https://github.com/anthnel/devdesk/issues/184)) ([14b1007](https://github.com/anthnel/devdesk/commit/14b10078306d04828f89c74c69742ca25d251533))


### Bug Fixes

* docs-build wrote to docs-site/docs-site/site, not docs-site/site ([#181](https://github.com/anthnel/devdesk/issues/181)) ([cb3f134](https://github.com/anthnel/devdesk/commit/cb3f134e40d4f6072e4cd9ddf7e8fefc048e540b))
* **forge/explorer:** hide immediate-deletion checkbox on GitHub ([#177](https://github.com/anthnel/devdesk/issues/177)) ([4853ddd](https://github.com/anthnel/devdesk/commit/4853dddc4ce4b3062feab7cd2c0523cf956fdf40))
* Health box no longer tells the same state in two colors ([#192](https://github.com/anthnel/devdesk/issues/192)) ([60d51ff](https://github.com/anthnel/devdesk/commit/60d51ff37832ff44d7804b7c49a144ad54bc0456))
* the about body opens on a blank line ([#186](https://github.com/anthnel/devdesk/issues/186)) ([bf528d6](https://github.com/anthnel/devdesk/commit/bf528d6da832e5e629d39da1cf191f51e4140d94))

## 1.0.0 (2026-09-06)


### Features

* :sec et oci lisent le registre (§3.58, poste 4) ([#153](https://github.com/anthnel/devdesk/issues/153)) ([434dd61](https://github.com/anthnel/devdesk/commit/434dd619d85ae441c351e81ad3b3a6d8bc4d4a26))
* :sec nomme une image par son alias de registry (§3.36) ([#93](https://github.com/anthnel/devdesk/issues/93)) ([b593c8a](https://github.com/anthnel/devdesk/commit/b593c8aad3dfaf9eb892dd5c4861e73fcad58871))
* `ws` affiche une icône par ligne, la colonne Type disparaît (§3.46) ([#120](https://github.com/anthnel/devdesk/issues/120)) ([a7468c3](https://github.com/anthnel/devdesk/commit/a7468c390756ca6f0068dd3d96d29ac82743fdf8))
* `ws` montre les fichiers cachés, si on le lui demande (§3.31) ([#78](https://github.com/anthnel/devdesk/issues/78)) ([95c8597](https://github.com/anthnel/devdesk/commit/95c85972baca8b77f71ed8656ed18d7faf0e421c))
* a configuration view (phase 1) ([#43](https://github.com/anthnel/devdesk/issues/43)) ([8174907](https://github.com/anthnel/devdesk/commit/8174907155552838672bca8e88c8bc01db46693f))
* a row says what it is, and what is happening to it (§3.22) ([#65](https://github.com/anthnel/devdesk/issues/65)) ([65bdaaf](https://github.com/anthnel/devdesk/commit/65bdaaf4ce5b0c4470f2e7f36fbf63a97d54e0b9))
* a viewer for documents — text, JSON, XML and logs (§3.25) ([#67](https://github.com/anthnel/devdesk/issues/67)) ([90f178e](https://github.com/anthnel/devdesk/commit/90f178e55757e72960cd83a59ec2b282f2bd9f4d))
* add internal/ui/datatable, the shared table mechanism (§2 step 1) ([#34](https://github.com/anthnel/devdesk/issues/34)) ([71217aa](https://github.com/anthnel/devdesk/commit/71217aa24669c033dfdbee6ce18219038fccaed3))
* ajouter les thèmes Windows Terminal Dark+, Nord, Solarized et GitHub ([#169](https://github.com/anthnel/devdesk/issues/169)) ([1a4e511](https://github.com/anthnel/devdesk/commit/1a4e5117c935ed89b78834d77238837567f15c78))
* cache discovered group members and refresh them on demand (§3.8 step 3) ([#32](https://github.com/anthnel/devdesk/issues/32)) ([654186b](https://github.com/anthnel/devdesk/commit/654186bda0f43bd83cf04d2f5c59ce7943a75f5a))
* colour in table cells — datatable renders its own rows ([#54](https://github.com/anthnel/devdesk/issues/54)) ([02cf0e7](https://github.com/anthnel/devdesk/commit/02cf0e72feda821d93418067e5b7f913a935a935))
* créer et supprimer sont du travail, pas une attente (§3.59) ([#159](https://github.com/anthnel/devdesk/issues/159)) ([c8ac122](https://github.com/anthnel/devdesk/commit/c8ac122d3c4e2992527e791fe33588fc0037d934))
* ct filtre par état et raccourcit sa colonne Ports (§3.27) ([#97](https://github.com/anthnel/devdesk/issues/97)) ([7aa33c4](https://github.com/anthnel/devdesk/commit/7aa33c4e733d0e159a464f77e72d1ac5525c6d04))
* datatable — chaque colonne déclare sa nature, et les largeurs suivent (§3.45, D61) ([#127](https://github.com/anthnel/devdesk/issues/127)) ([64569bf](https://github.com/anthnel/devdesk/commit/64569bfc7fd682a8acc4f7c3d56d1749046be692))
* forge.Vocabulary, et aucune vue n'écrit le nom d'une forge (§3.6 étape 5) ([#104](https://github.com/anthnel/devdesk/issues/104)) ([22a5e45](https://github.com/anthnel/devdesk/commit/22a5e4589dd0b42f6037c14041a11af0f261e6e1))
* give a registry a slug, a kind and a provider (§3.8 step 1) ([#29](https://github.com/anthnel/devdesk/issues/29)) ([4c9bbc1](https://github.com/anthnel/devdesk/commit/4c9bbc1fcccc40e12ed740ddba03aa3b5bf16ec0))
* groundwork for the explorer clone rework (§3.16) — pagination, lighter discovery, selection model ([#49](https://github.com/anthnel/devdesk/issues/49)) ([7fab711](https://github.com/anthnel/devdesk/commit/7fab711126911f75f466823174d4204bc77889cd))
* internal/forge, ce que DevDesk demande à une plateforme (§3.6 étape 1) ([#100](https://github.com/anthnel/devdesk/issues/100)) ([3c0c8af](https://github.com/anthnel/devdesk/commit/3c0c8afd3ce82cb71b1f2883a8a3b24a3d6f9655))
* internal/forge/github, le second backend (§3.6 étape 7) ([#106](https://github.com/anthnel/devdesk/issues/106)) ([5e500d6](https://github.com/anthnel/devdesk/commit/5e500d68492d54367ceae79a4788bd72d174ca59))
* internal/forge/gitlab, le backend GitLab derrière l'interface (§3.6 étape 2) ([#101](https://github.com/anthnel/devdesk/issues/101)) ([d481d3f](https://github.com/anthnel/devdesk/commit/d481d3f88f8aa4a00134d1408405b6796f76f763))
* internal/netcheck répond à des questions plutôt qu'à des outils (PR 1/4) ([#87](https://github.com/anthnel/devdesk/issues/87)) ([096b575](https://github.com/anthnel/devdesk/commit/096b575146a59bab2e3c7fe8f318a5b47213eac4))
* K arrête un run ou une de ses cibles (§3.58, poste 8) ([#157](https://github.com/anthnel/devdesk/issues/157)) ([46b012c](https://github.com/anthnel/devdesk/commit/46b012c1d67c9e0b2e5c763fbefdee9634bd6a5d))
* l'explorer passe aux icônes, et le thème apprend à les colorer (§3.56) ([#147](https://github.com/anthnel/devdesk/issues/147)) ([f636bd9](https://github.com/anthnel/devdesk/commit/f636bd9b6a2e4d849b5a9db7a4d4c783a96c2421))
* l'onglet de configuration suit la forge du contexte (§3.6 étape 6) ([#105](https://github.com/anthnel/devdesk/issues/105)) ([7514903](https://github.com/anthnel/devdesk/commit/751490317767ddd8fa7147610ba244cd4b2dc73d))
* l'onglet Topology devient Interfaces, et la route passe dans Diagnostics (D57, D58, §3.44) ([#117](https://github.com/anthnel/devdesk/issues/117)) ([6c2e7b4](https://github.com/anthnel/devdesk/commit/6c2e7b431de42e9b9a30b391583577e58c8e743c))
* la colonne CI et le cinquième onglet (§3.42, PR 3/3) ([#134](https://github.com/anthnel/devdesk/issues/134)) ([49a224d](https://github.com/anthnel/devdesk/commit/49a224d54b8312f3875f67007df67915fd425e82))
* la colonne CI se colore, et migre vers :sec (§3.52) ([#140](https://github.com/anthnel/devdesk/issues/140)) ([5a96cdd](https://github.com/anthnel/devdesk/commit/5a96cdddc234825dfe2f80680dd2b4be185a3d2f))
* la section `gitlab:` devient `forge:` avec un `type:` (§3.6 étape 4) ([#103](https://github.com/anthnel/devdesk/issues/103)) ([5f55bb1](https://github.com/anthnel/devdesk/commit/5f55bb150770425afac3f6874661647d0d4c6c7a))
* la trace de route est supprimée, `network.tool_image` avec (D57, §3.47) ([#118](https://github.com/anthnel/devdesk/issues/118)) ([5d0502d](https://github.com/anthnel/devdesk/commit/5d0502d7627596c430892e41119afb8dcd0182f3))
* la vue :jobs, et le dashboard compte ce qui tourne (§3.58, poste 6) ([#155](https://github.com/anthnel/devdesk/issues/155)) ([f88f79a](https://github.com/anthnel/devdesk/commit/f88f79adce62fcb53411a4dbbdff68713d3f3350))
* la vue config nomme son contexte, et l'onglet docker devient network (D48, §3.34) ([#91](https://github.com/anthnel/devdesk/issues/91)) ([998d298](https://github.com/anthnel/devdesk/commit/998d298b49937bf39329c9f370c57cec46c84d69))
* le binaire connaît sa version, publiée par release-please + goreleaser (§3.62) ([#175](https://github.com/anthnel/devdesk/issues/175)) ([8387ed0](https://github.com/anthnel/devdesk/commit/8387ed03a7cf920127bc87a4d3e555f9f2db4a90))
* le clone devient un run du registre (§3.58, poste 7) ([#156](https://github.com/anthnel/devdesk/issues/156)) ([b227b44](https://github.com/anthnel/devdesk/commit/b227b44f23a1b72c19dafe022bb48d2d523374d6))
* le pull d'une image devient un job, et la table Images l'affiche (§3.60) ([#166](https://github.com/anthnel/devdesk/issues/166)) ([6109ff2](https://github.com/anthnel/devdesk/commit/6109ff216f532d877d1372be17dc87e7e3a289d0))
* le réglage plumber, visible et inerte (§3.42, PR 1/3) ([#132](https://github.com/anthnel/devdesk/issues/132)) ([a612cb2](https://github.com/anthnel/devdesk/commit/a612cb214e1c20f524e2374270c6f313c973bbc4))
* le viewer colore Markdown, Dockerfile et shell, et rend le Markdown (§3.32) ([#82](https://github.com/anthnel/devdesk/issues/82)) ([00b4def](https://github.com/anthnel/devdesk/commit/00b4def5733bccb594db618d141b10423ebfdd19))
* le viewer numérote ses lignes, va à l'une d'elles, et sait compter la casse (§3.53) ([#142](https://github.com/anthnel/devdesk/issues/142)) ([c1b71da](https://github.com/anthnel/devdesk/commit/c1b71da35c78f39f1fc03e76d1f1eec6dba2f60b))
* les commandes deviennent `git-auth` et `git-explorer` (§3.6 étape 8, entrée close) ([#107](https://github.com/anthnel/devdesk/issues/107)) ([a6ca2df](https://github.com/anthnel/devdesk/commit/a6ca2dfc078bf762eb53e1b39999263a87a10c91))
* les neuf autres vues grisent leurs raccourcis (§3.48, Rule 130) ([#124](https://github.com/anthnel/devdesk/issues/124)) ([8e21822](https://github.com/anthnel/devdesk/commit/8e218229260472b10b3573190d9305ec5d366bb1))
* net — `K` dit quand elle ne peut pas tuer, et les adresses se séparent par famille (§3.49) ([#125](https://github.com/anthnel/devdesk/issues/125)) ([548902e](https://github.com/anthnel/devdesk/commit/548902e129bc924015c93ad4f2c42f132e6c9d5f))
* netcheck explique ses constats, sans modèle (PR 2/4) ([#88](https://github.com/anthnel/devdesk/issues/88)) ([41ff120](https://github.com/anthnel/devdesk/commit/41ff12074a243baf12e0f05fa0868e8c16d418e6))
* netdiag répond à des questions, pas à des outils (§3.33, PR 3/4) ([#89](https://github.com/anthnel/devdesk/issues/89)) ([adc6e9f](https://github.com/anthnel/devdesk/commit/adc6e9fdbb665144e1512c47331034d4db917f7b))
* one key, one meaning — the action vocabulary lands (§3.26) ([#71](https://github.com/anthnel/devdesk/issues/71)) ([a6189bb](https://github.com/anthnel/devdesk/commit/a6189bbfc869075efb809b051acd92ae12dd33ab))
* open command mode with alt+: from anywhere, including a text field ([#16](https://github.com/anthnel/devdesk/issues/16)) ([42f005c](https://github.com/anthnel/devdesk/commit/42f005c61eed42592fe9a87f3c45068fdad3c220))
* open the registry browser from cache, and drill into groups (§3.8 steps 5, 6) ([#33](https://github.com/anthnel/devdesk/issues/33)) ([496418b](https://github.com/anthnel/devdesk/commit/496418bc0427eae2c2a5463cc19b1d86c831256d))
* pick a group detector by declared provider (§3.8 step 4) ([#31](https://github.com/anthnel/devdesk/issues/31)) ([e8e39e7](https://github.com/anthnel/devdesk/commit/e8e39e75b9e3fd0d4e7adc9a56de74ee5183f788))
* plumber tourne, et le cache retient le score (§3.42, PR 2/3) ([#133](https://github.com/anthnel/devdesk/issues/133)) ([dd6bb8b](https://github.com/anthnel/devdesk/commit/dd6bb8b60df42a77c863886ba8e7b3c7985acb84))
* remove the OCI connectivity-test feature and its config option ([#172](https://github.com/anthnel/devdesk/issues/172)) ([61eff37](https://github.com/anthnel/devdesk/commit/61eff3741bf9da3934ab1e38699cda632ec59892))
* scope the scan caches to their configuration context ([#41](https://github.com/anthnel/devdesk/issues/41)) ([dc93876](https://github.com/anthnel/devdesk/commit/dc938769c43259e9360cb61d8ba1374482440eb0))
* secrets are shown where they are found, and table text takes the theme ([#63](https://github.com/anthnel/devdesk/issues/63)) ([c44b0ae](https://github.com/anthnel/devdesk/commit/c44b0ae279c5f2c9d6338fea51c2b4a57eed6358))
* security becomes an inventory (phase 2) ([#44](https://github.com/anthnel/devdesk/issues/44)) ([6f409c6](https://github.com/anthnel/devdesk/commit/6f409c68956b58035dfe222c5827a1b28dadb4a5))
* store every secret in the host secret manager (§3.9) ([#25](https://github.com/anthnel/devdesk/issues/25)) ([9610ff0](https://github.com/anthnel/devdesk/commit/9610ff055341389793f0eeda19eef6ade027f369))
* the dashboard says what to do about it (§3.24) ([#66](https://github.com/anthnel/devdesk/issues/66)) ([78832b6](https://github.com/anthnel/devdesk/commit/78832b6b4500ccad90e869a9bc2a9baf8fe2576c))
* the dashboard stops reflowing, and gains resource charts (§3.19) ([#62](https://github.com/anthnel/devdesk/issues/62)) ([82ed2f3](https://github.com/anthnel/devdesk/commit/82ed2f3f5be37dd82f003780ffc021503f82396e))
* the explorer clones what you select (§3.16) ([#50](https://github.com/anthnel/devdesk/issues/50)) ([c85af3f](https://github.com/anthnel/devdesk/commit/c85af3f44ef18741927c0df6ed5d3f9589533a0e))
* the keyboard gets a declared vocabulary, and ctrl+p opens the palette (§3.26) ([#70](https://github.com/anthnel/devdesk/issues/70)) ([2dbbda4](https://github.com/anthnel/devdesk/commit/2dbbda409f3643ed1fb17a7fd406eef80c7bf71c))
* the viewer colors YAML and TOML, and a search says where it matched (§3.29) ([#76](https://github.com/anthnel/devdesk/issues/76)) ([fe5479f](https://github.com/anthnel/devdesk/commit/fe5479fcee784fe7e555ed4e83c17a99f9f4bea1))
* un message de footer, trois niveaux, un composant (§3.30) ([#77](https://github.com/anthnel/devdesk/issues/77)) ([cb61ac6](https://github.com/anthnel/devdesk/commit/cb61ac6c538cf3205de4ea268cd0e6945b892463))
* un raccourci sans objet est grisé, pas supprimé (§3.48, Rule 130) ([#122](https://github.com/anthnel/devdesk/issues/122)) ([78ca7be](https://github.com/anthnel/devdesk/commit/78ca7be7c395f732f07e4535c67741a7d21aab8e))
* un registre de travaux, et une seule chaîne de spinner (§3.58, poste 2) ([#151](https://github.com/anthnel/devdesk/issues/151)) ([421cd3f](https://github.com/anthnel/devdesk/commit/421cd3fcfaa6962b73bbcb980cb40344ccf085d0))
* un serveur MCP en lecture seule — `dk mcp` (§3.38) ([#111](https://github.com/anthnel/devdesk/issues/111)) ([bfed637](https://github.com/anthnel/devdesk/commit/bfed6373dca58dc683d9f800aa29ea8cccefedcf))
* une checkbox décide du mode client-serveur Trivy, pas le champ d'adresse ([#171](https://github.com/anthnel/devdesk/issues/171)) ([fdf0e31](https://github.com/anthnel/devdesk/commit/fdf0e31bd12eb6965c69f80fbd05e1eb0606bde9))
* workspaces reconciles what exists (§3.17) ([#51](https://github.com/anthnel/devdesk/issues/51)) ([b017db7](https://github.com/anthnel/devdesk/commit/b017db7e361049ec1c8416529aea99478af8457a))
* ws et :sec colorent leur colonne d'icône (§3.57) ([#149](https://github.com/anthnel/devdesk/issues/149)) ([c7b6677](https://github.com/anthnel/devdesk/commit/c7b6677ad475a1d8b1a5bdbd92750cec49a48fd6))
* ws lit le registre, et son compteur passe au footer (§3.58, poste 3) ([#152](https://github.com/anthnel/devdesk/issues/152)) ([23ad505](https://github.com/anthnel/devdesk/commit/23ad5059743954d0bbd47ec9011be9279bb6da08))
* Y copie le chemin de la ligne sélectionnée dans ws (§3.35) ([#92](https://github.com/anthnel/devdesk/issues/92)) ([97392e6](https://github.com/anthnel/devdesk/commit/97392e69267cb46dc612621a85a46fa4053674cc))


### Bug Fixes

* :sec ne liste plus une cible qui n'existe plus (§3.37) ([#94](https://github.com/anthnel/devdesk/issues/94)) ([b6b166b](https://github.com/anthnel/devdesk/commit/b6b166bb821544259fc41985e581d5d778607a6c))
* a Docker reference has no scheme (D41) ([#53](https://github.com/anthnel/devdesk/issues/53)) ([3d6be2d](https://github.com/anthnel/devdesk/commit/3d6be2d6afb7ef784e9b4a6967ab0ea0cd81cd70))
* a picker entry is identified by itself, not by its URL (D40) ([#60](https://github.com/anthnel/devdesk/issues/60)) ([c95a9a5](https://github.com/anthnel/devdesk/commit/c95a9a518afbf304edd0c4100c8b34a453df8479))
* a running scan showed nothing — the spinner was styled inside a table cell ([#73](https://github.com/anthnel/devdesk/issues/73)) ([e37c592](https://github.com/anthnel/devdesk/commit/e37c5924b39dded7c3901fdc0f64ade4485253e4))
* align locked checkboxes with the rest of their group ([#45](https://github.com/anthnel/devdesk/issues/45)) ([9882b65](https://github.com/anthnel/devdesk/commit/9882b65dcf4eee3a0a27cd8f9945594131d0dd5f))
* answer the last two defects by removing something (D21, D36) ([#52](https://github.com/anthnel/devdesk/issues/52)) ([4e2c518](https://github.com/anthnel/devdesk/commit/4e2c51867bc4d8a8aee5bfcd9f4ff7d127d492ee))
* background for default theme ([#119](https://github.com/anthnel/devdesk/issues/119)) ([60199b3](https://github.com/anthnel/devdesk/commit/60199b318bfc4d39febf921ad2f0567a67b16254))
* close the five parked defects (D4, D8, D9, D10, D11) ([#19](https://github.com/anthnel/devdesk/issues/19)) ([cb4a982](https://github.com/anthnel/devdesk/commit/cb4a98225bfa3ca32a15ec31ff826a43641d43c2))
* close the four router defects (D15, D16, D17, D18) ([#24](https://github.com/anthnel/devdesk/issues/24)) ([4f49769](https://github.com/anthnel/devdesk/commit/4f4976970874f292d94eb88a418c7b2d1357536d))
* close the three self-contained backlog defects (D7, D2, D5) ([#11](https://github.com/anthnel/devdesk/issues/11)) ([0b1ab78](https://github.com/anthnel/devdesk/commit/0b1ab78d78466ac707133909f678bf1dd77ee3ae))
* give "Scan Options" the blank line every other section header has ([#28](https://github.com/anthnel/devdesk/issues/28)) ([756bb1b](https://github.com/anthnel/devdesk/commit/756bb1bf2e0ae04e4e1726795591b8655235bc7b))
* K opened a modal nobody could see, and it ate the keyboard ([#72](https://github.com/anthnel/devdesk/issues/72)) ([b6f588d](https://github.com/anthnel/devdesk/commit/b6f588d6062c316df24e1dffb088bf247a750d2e))
* l'exemple de token suit la forge, et la garde apprend les préfixes (D54) ([#108](https://github.com/anthnel/devdesk/issues/108)) ([782ec6f](https://github.com/anthnel/devdesk/commit/782ec6f6d384874f23ea0c771fcb083c376241b2))
* l'inventaire :sec attend la réponse des caches avant de dire « rien » (D65) ([#146](https://github.com/anthnel/devdesk/issues/146)) ([a0722b1](https://github.com/anthnel/devdesk/commit/a0722b189225ab0ac4c0d7f6344f49d5a8e82550))
* l'onglet Ports lit la machine, pas la VM Docker (D55, §3.43) ([#114](https://github.com/anthnel/devdesk/issues/114)) ([15e2c00](https://github.com/anthnel/devdesk/commit/15e2c00a9c02269796bf6c0ed2ddf3b76df3ea00))
* la barre de filtre des résultats de security n'était pas dessinée (D45) ([#83](https://github.com/anthnel/devdesk/issues/83)) ([7903dbb](https://github.com/anthnel/devdesk/commit/7903dbb2bdb328ba30df5020cffc97184ef53c0c))
* la boîte Health rend les certificats dans leur vocabulaire (D64) ([#144](https://github.com/anthnel/devdesk/issues/144)) ([08a1307](https://github.com/anthnel/devdesk/commit/08a13074db6e647402006c05f3550a497e4147e7))
* la colonne CI d'un contexte GitHub rendait les mots de GitHub (D66) ([#148](https://github.com/anthnel/devdesk/issues/148)) ([7d525c6](https://github.com/anthnel/devdesk/commit/7d525c6d6041c7d355c7e80100548058df100325))
* la posture écarte aussi les images disparues (D69, suite) ([#162](https://github.com/anthnel/devdesk/issues/162)) ([0f334d6](https://github.com/anthnel/devdesk/commit/0f334d6952a0540b0c6dfe1967d9d21b4925a88d))
* la posture écarte les dépôts disparus, comme l'inventaire (D69) ([#160](https://github.com/anthnel/devdesk/issues/160)) ([bf3f9f6](https://github.com/anthnel/devdesk/commit/bf3f9f60127d6a1ceead15d516a69455a37b6b31))
* la suppression dans workspaces n'était ni marquée ni gardée (§3.23) ([#86](https://github.com/anthnel/devdesk/issues/86)) ([c4cc7c7](https://github.com/anthnel/devdesk/commit/c4cc7c70995e97f5fcd099900ecd4ae5b0eca012))
* la table des ports restait affichée après l'arrêt de Docker (D47) ([#90](https://github.com/anthnel/devdesk/issues/90)) ([aecc6ea](https://github.com/anthnel/devdesk/commit/aecc6ea047103fd8f05c4b8d7cfbbc8b9ead237c))
* le binaire installé porte l'extension que Windows exige (D49) ([#95](https://github.com/anthnel/devdesk/issues/95)) ([59b95e5](https://github.com/anthnel/devdesk/commit/59b95e588d2839d136eac37165a8d3d3e8025d55))
* le cache de scan d'images cesse d'être scopé au contexte (§3.39) ([#110](https://github.com/anthnel/devdesk/issues/110)) ([54d6f3d](https://github.com/anthnel/devdesk/commit/54d6f3dca7227d984823822c424fe419cc1c69a5))
* le CPU Docker du dashboard montait à 1400 %, la RAM sommait des touts différents (D46) ([#85](https://github.com/anthnel/devdesk/issues/85)) ([c7963e8](https://github.com/anthnel/devdesk/commit/c7963e8f52b3400a021180103eec6d5857d294a5))
* le dashboard réclamait « Net Diag », un outil que rien ne détecte (D60) ([#121](https://github.com/anthnel/devdesk/issues/121)) ([d84b0ec](https://github.com/anthnel/devdesk/commit/d84b0ece4c4f110042e7422ff22291c67f6d3753))
* le spinner d'un conteneur qu'on arrête restait figé (D44) ([#81](https://github.com/anthnel/devdesk/issues/81)) ([d2f2e59](https://github.com/anthnel/devdesk/commit/d2f2e591e11a660b6b4393b2c4344812bd7b0ef0))
* le viewer suit dans le viewport, et son pager marche enfin sous Windows (D50, D51) ([#98](https://github.com/anthnel/devdesk/issues/98)) ([73a8b47](https://github.com/anthnel/devdesk/commit/73a8b47a2a73480d2c198debf8ffc7f3093c63de))
* le walk des dépôts imbriqués n'a plus de limite de profondeur (D59) ([#116](https://github.com/anthnel/devdesk/issues/116)) ([3e9c6b9](https://github.com/anthnel/devdesk/commit/3e9c6b9b3a2db713a2537429c03e84dee5e2f00b))
* les trois défauts ouverts de §1.3 — D62, D59, D63 ([#143](https://github.com/anthnel/devdesk/issues/143)) ([1361584](https://github.com/anthnel/devdesk/commit/1361584c664ecc12be29579f987825510add351b))
* make GitLab credential contexts actually isolated ([#8](https://github.com/anthnel/devdesk/issues/8)) ([10a19a1](https://github.com/anthnel/devdesk/commit/10a19a17315ebac18ae55a56843cf6f5333fdeae))
* measure text in columns, not bytes, when truncating and wrapping ([#5](https://github.com/anthnel/devdesk/issues/5)) ([f7f7128](https://github.com/anthnel/devdesk/commit/f7f7128dfe40ab9e592a31bf360f573b11d84a0f))
* mise run install puts dk where it says it does ([#56](https://github.com/anthnel/devdesk/issues/56)) ([3e10f92](https://github.com/anthnel/devdesk/commit/3e10f92b7c933f15021e1b54ec417933ae5135f2))
* never report an image as clean when nothing scanned it (D20, D22) ([#27](https://github.com/anthnel/devdesk/issues/27)) ([8a91c86](https://github.com/anthnel/devdesk/commit/8a91c8617b0fbcaf91d85ae9dfb1ac6ff88e62cb))
* never send credentials to a registry marked anonymous (D12, §3.8 step 2) ([#30](https://github.com/anthnel/devdesk/issues/30)) ([9e5dd8c](https://github.com/anthnel/devdesk/commit/9e5dd8c585b7249f981f6c06d0a3c69ac1dd11a1))
* one scan-option builder, closing D26 ([#40](https://github.com/anthnel/devdesk/issues/40)) ([aa43033](https://github.com/anthnel/devdesk/commit/aa43033e946adf9c62b209d1e8d3e6ec4665ce3b))
* plumber — répertoire, score fractionnaire, et les erreurs vont au log ([#136](https://github.com/anthnel/devdesk/issues/136)) ([5ef0797](https://github.com/anthnel/devdesk/commit/5ef0797a21494ac870287c01db14f7809335e2a3))
* read the configured tool paths and let the source be chosen (D27) ([#42](https://github.com/anthnel/devdesk/issues/42)) ([3131d56](https://github.com/anthnel/devdesk/commit/3131d56d097c3b7b94adfc8558c6006c61a5e119))
* repair Entire hook matchers that no longer matched any tool ([57724eb](https://github.com/anthnel/devdesk/commit/57724eb28b2810f4c37c00730b74248b552c29cb))
* report unplugged interfaces as DOWN in the topology view ([2972046](https://github.com/anthnel/devdesk/commit/29720461fcddf92fc2488a1034665131c311692b))
* status acted on the wrong monitor under a filter (D25) + §2 step 6 — the refactor closes ([#39](https://github.com/anthnel/devdesk/issues/39)) ([e4eda00](https://github.com/anthnel/devdesk/commit/e4eda0000b7cd15f03e032dd5ccc7472f678d4b5))
* tenir `→` empilait le même chemin dans le fil d'Ariane (D43) ([#80](https://github.com/anthnel/devdesk/issues/80)) ([be3ceb1](https://github.com/anthnel/devdesk/commit/be3ceb17d1a5a405d9ab4d8275788c8bb6f5a122))
* the background ignored the theme — a var rendered before main() ran (D42) ([#75](https://github.com/anthnel/devdesk/issues/75)) ([55d266e](https://github.com/anthnel/devdesk/commit/55d266eb93fc6985db20d0c73a8cf051656893b0))
* the status view stops advertising settings it does not own ([#59](https://github.com/anthnel/devdesk/issues/59)) ([ec7a4b2](https://github.com/anthnel/devdesk/commit/ec7a4b2ada879e02a07eb9322de4e00c5cfcd4e8))
* un datatable vide reste une table, jamais un message (Rule 139) ([#158](https://github.com/anthnel/devdesk/issues/158)) ([820ab7e](https://github.com/anthnel/devdesk/commit/820ab7ee75f6bedc72aeda49d0a454a89ff9ba28))
* un fichier de règles gitleaks est monté, et un scan qui n'a rien lu n'est plus « propre » (§3.50, D56) ([#129](https://github.com/anthnel/devdesk/issues/129)) ([ca339a9](https://github.com/anthnel/devdesk/commit/ca339a906420f28dbe763d21a72375704c263c1a))
* un membre de registry est une adresse, pas une URL (D39, §3.18) ([#109](https://github.com/anthnel/devdesk/issues/109)) ([044cdb2](https://github.com/anthnel/devdesk/commit/044cdb27613be4147e3808bcfcc3956e50adeae6))
* un scan de ws ne reprend plus l'écran à chaque dépôt (D67) ([#150](https://github.com/anthnel/devdesk/issues/150)) ([37b72e6](https://github.com/anthnel/devdesk/commit/37b72e6e3a0cafa0ae0c09ad0bfe98bda8b98283))
* un scan écrit dans le contexte où il a été lancé (D68) ([#154](https://github.com/anthnel/devdesk/issues/154)) ([cd62621](https://github.com/anthnel/devdesk/commit/cd6262169a62f76635154fe05cbb42fa2c721633))
* workspaces acted on the wrong directory under a filter (D24) + §2 step 5 ([#38](https://github.com/anthnel/devdesk/issues/38)) ([d9092de](https://github.com/anthnel/devdesk/commit/d9092dee2a6f41b3d6a1af8fd09872b45ccd21df))


### Refactoring

* drop gitlab.pull.target_dir ([e0b2c48](https://github.com/anthnel/devdesk/commit/e0b2c4828b73f942838a20de7b91b30a16c6aed7))
* l'onglet Certificates range ses colonnes par nature (§3.54) ([#145](https://github.com/anthnel/devdesk/issues/145)) ([bbe3e2c](https://github.com/anthnel/devdesk/commit/bbe3e2c5b1e6aa41cc6ba48f216d482cb13d700d))
* la colonne Status du clone passe à droite ([#84](https://github.com/anthnel/devdesk/issues/84)) ([512bf3a](https://github.com/anthnel/devdesk/commit/512bf3a482cf8055ea66a05009f776fdf4c08786))
* la forge remplace le client GitLab dans shared.State (§3.6 étape 3) ([#102](https://github.com/anthnel/devdesk/issues/102)) ([d2c841e](https://github.com/anthnel/devdesk/commit/d2c841ebfffa6130a03419b31ad1571c688a3be8))
* move the containers and images tables onto datatable (§2 step 4) ([#37](https://github.com/anthnel/devdesk/issues/37)) ([d0b88a3](https://github.com/anthnel/devdesk/commit/d0b88a37a6cac8d0fd185a52e6f064538d248275))
* move the netdiag ports table onto datatable (§2 step 3) ([#36](https://github.com/anthnel/devdesk/issues/36)) ([d547a13](https://github.com/anthnel/devdesk/commit/d547a1380badebe53b594b8260248abf20b62c1a))
* move the networks and volumes tables onto datatable (§2 step 2) ([#35](https://github.com/anthnel/devdesk/issues/35)) ([367326d](https://github.com/anthnel/devdesk/commit/367326d4998a4c5c4e76bcd39727a5d12097616d))
* remove SBOM generation (§3.14) ([#47](https://github.com/anthnel/devdesk/issues/47)) ([7dea42f](https://github.com/anthnel/devdesk/commit/7dea42fe3ef722a13dedefc5954594400f6a2052))
* split docker/client.go and route every CLI call through a seam ([#6](https://github.com/anthnel/devdesk/issues/6)) ([343886c](https://github.com/anthnel/devdesk/commit/343886c5a6adc399ed48b0de285b587f1d237d34))
* the last four tables move to datatable (§3.21) ([#64](https://github.com/anthnel/devdesk/issues/64)) ([244a0d3](https://github.com/anthnel/devdesk/commit/244a0d36c8b0623ba369d28edb2a6ab288399c44))
