# goal · storage-s3

- **State:** building
- **Task:** hardening
- **Branch:** hardening

## Tasks

1. [x] port
2. [ ] hardening

## Task brief · hardening

Repositories: go-storage (root), go-core, sqlate, go-database, go-observability, go-web-sdk,
go-web-sdk-template, blobfs, go-web-service — branch `hardening` in each (the five new ones
branch from main on approval; all on main and clean, no other goal locks them).

```
## Task brief · storage-s3 · hardening (revised at BRIEF, second time)
Problem       As before (s3 at the provider bar; one container convention). And
              time.Time values leave the workspace's libraries in whatever
              Location their source picked: pgx returns Postgres's UTC instants
              in the host's zone, azureblob's Put/Get/Stat return a GMT zone (or
              Local on GMT-abbreviated hosts) while its List returns UTC, go-core
              logs in the host zone. JSON, cursors, logs, and == then depend on
              the host. Postgres already stores UTC; the read side doesn't.
Behaviors     1–16. As approved (provider bar; container convention; task names).
              17. Every time.Time a workspace library returns is in time.UTC,
                 whatever the process's TZ: azureblob and s3 Put/Get/Stat/List;
                 sqlate's Scanner and Scalar; go-database/postgres connections
                 (pgx ScanLocation UTC); blobfs and go-web-service reads through
                 them. Tests compare whole values against UTC and run under
                 TZ=Europe/London (a GMT-abbreviated zone).
              18. go-storage's contract documents ModifiedAt as UTC; the Fake
                 stamps UTC; the conformance suite checks Location.
              19. sqlate's migration history records applied_at as timestamp with
                 time zone; an existing history table is altered on the next
                 migrate, and its rows keep their instants.
              20. go-core's slog handlers write record times in UTC.
              21. Every repository's currency exits 0 at SHIP: each layer bumps to
                 the layer below's new tags before its own merge and tags.
              22. Changelogs record each release; architecture-layer edits wait
                 for sync.
Test seams    as approved; plus each library's exported read API under
              TZ=Europe/London; sqlate's migrator against an existing history
              table; go-core's logger output
Slices        1–14. committed
              15. upgrade go-core: Go 1.27.2
              16. upgrade go-database: Go 1.27.2, go-core v0.6.0 (absorb its
                 removed lifecycle.Service/Add/stages, docs included)
              17. upgrade go-observability: Go 1.27.2, go-core v0.6.0
              18. upgrade go-web-sdk: Go 1.27.2, rate-limit on go-web-sdk v0.15.0
              19. go-core: UTC log times; changelog for v0.7.0
              20. sqlate: Scanner/Scalar UTC; history applied_at timestamptz with
                 in-place alter; changelogs for v0.5.0, postgres/v0.5.0,
                 sqlint/v0.2.2
              21. go-storage: azureblob and s3 UTC (List comment corrected),
                 contract, Fake, suite; changelogs: base v0.6.0, azureblob v0.5.0
                 gains a Fixed entry and the v0.6.0 requirement, s3 v0.1.0 likewise
              22. go-database/postgres: ScanLocation UTC; changelogs for v0.8.0,
                 postgres/v0.5.0
              23. release prep in go-observability, go-web-sdk, blobfs,
                 go-web-sdk-template: changelog sections for their tags
              24. re-check: check, currency (against main's tags), and container
                 tasks green in every touched repository
              SHIP walks `order` layer by layer: go-core, sqlate → (bump) go-database,
              go-web-sdk, go-observability, go-storage (base, then providers), blobfs →
              (bump) go-web-sdk-template → (bump) go-web-service; each layer merges, passes
              ci, tags, then the next bumps.
Out of scope  Put's buffering; transfermanager's log output; archived spikes;
              spike-model-hosting; claude-plugins; architecture-page edits (sync);
              a go-core clock abstraction (callers convert with .UTC())
Door          two-way through slice 24; one-way at SHIP: every tag below is
              pinned by the module proxy and checksum database
Release       go-core v0.7.0
              sqlate v0.5.0, postgres/v0.5.0, sqlint/v0.2.2
              go-database v0.8.0, postgres/v0.5.0
              go-observability v0.2.0, otlp/v0.2.0
              go-web-sdk v0.15.1, middleware/rate-limit/v0.2.1
              go-storage v0.6.0, azureblob/v0.5.0, s3/v0.1.0
              blobfs v0.6.0, postgres/v0.4.0
              go-web-sdk-template template/v0.3.1
```

Version rule applied (for approval with the Release line): minor where returned times change
(go-core, sqlate, sqlate/postgres, go-database/postgres, go-storage base, blobfs and
blobfs/postgres through sqlate) or where a breaking requirement is pulled into importers
(go-database and go-observability off go-core v0.5.0, otlp through it — precedent:
azureblob's go-storage v0.5.0 entry); patch where only a non-breaking requirement moves
(sqlint, go-web-sdk, rate-limit, template). go-web-service is an application: no tag.

## Progress

slices 14/24 committed (go-storage: d3b165f, 8afbb0d, 13179c2, 4c12efe, 22fc733, 445b840, 521f571, 341cd14; sqlate: a6e772f, 87b176d; blobfs: 3e5290f, 45c1260; go-web-service: ef628fe, b5695a1) · reviews of 1–14: standards ✓, spec ✓, editor ✓ · reviews of 15–24: standards — · spec — · editor —

## Decisions

- port: the spike's `acceptance` mise task comes along with seaweedfs:start/stop, because release-and-ci asks for a local acceptance task.
- plan: port's brief was written at plan, so start re-runs currency and presents it for approval.
- port: currency re-run at start (Go 1.27.2, go-storage v0.5.0, AWS SDK patch releases only); the brief covered it, so no round.
- port: azureblob's changelog marks the go-storage v0.5.0 requirement Breaking, because it pulls go-core v0.6.0 into an importer's build.
- port: s3's CI test step has no `if:` condition, so a failing azureblob step skips it, as a failing step skips the rest of the job.
- port: s3's changelog links Unreleased to `commits/HEAD/s3` until s3 has a tag.
- port: STANDARDS.md names s3 in the pointers whose principles s3 already meets; the timeouts pointer names s3 at hardening.
- hardening: currency at start was current; no upgrade slice.
- hardening: try_timeout is the stdlib HTTP client timeout (per attempt, body included, retried by the SDK); a per-attempt SDK middleware deadline was rejected because the SDK turns an expired attempt context into CanceledError and never retries it.
- hardening: concurrency defaults to 4 within 1–32, as azureblob's; a multipart Put holds part_size × (concurrency + 2).
- hardening: the "s3:" prefix goes on every multipart failure Put returns (cancellation, part, completion), not on classified SDK errors, as in azureblob.
- hardening: a resumed Get tail goes unvalidated by checksum, documented, as azureblob's retry reader does; resumption applies to every Get.
- hardening: Get resumption is proven on the unit tier and in SeaweedFS acceptance.
- hardening: tag azureblob/v0.5.0 beside s3/v0.1.0, completing the go-storage v0.5.0 ripple.
- hardening: no base go-storage v0.5.1; the docs-only changes since v0.5.0 ride the next base change.
- hardening: measured, a multipart Put reads ahead part_size × (concurrency + 2) (48 MiB at the defaults) but holds part_size × (concurrency + 3) with a declared Size (56 MiB) and part_size × (concurrency + 4) without one (64 MiB), since transfermanager copies the first part unpooled and Put's decision buffer grows by doubling; the docs state all three, and the buffering itself is left as it is (out of scope).
- hardening: a Get body resumes up to max_retries times per Read call, not per body, as azblob's RetryReader counts; a body that trickles a byte per try keeps resuming, each resume costing a try_timeout, and ReadIdleTimeout bounds it under a Store.
- hardening: the context README has no timeouts content, so it names azureblob where its notes were azureblob-only rather than gaining a timeouts line; STANDARDS.md's timeouts pointer names s3.
- hardening: s3's first changelog states the "s3:" prefix under Added, not Fixed, since the provider was never released.
- hardening: a Get body does not resume when the caller's context is done, after Close, when the first answer carried no ETag, or when the failure came at or past the last byte, where only the SDK's end-of-body checksum check fails and a resume would get 416.
- hardening: a resumption whose Content-Range does not start at the offset, from a gateway that ignored the Range, fails the read with ErrUnavailable, wrapping the failure it could not recover, rather than repeat or skip bytes.
- hardening: a failed read that arrives with bytes delivers them first and resumes on the next Read.
- hardening: a Get body's final failure is sticky: later Reads return it without resuming.
- hardening: Close may cut off a Read from another goroutine; a mutex guards the current body, and a Read that Close cuts off never resumes.
- hardening: If-Match sends the first answer's ETag verbatim, as the service sent it, not the quoted entity-tag form Get reports.
- hardening: the multipart prefix is a bare "s3: ", since transfermanager's text already says "upload multipart failed"; it sits outside the sentinel ("s3: storage unavailable: …"), and the abort-failure suffix follows it.
- hardening: the resumption's classifier is classifyResume, not azureblob's classifyRead, because it classifies only the resumption's GetObject; every other Get body failure goes through classify.
- hardening: design.md's write path holds the AbortIncompleteMultipartUpload lifecycle-rule recommendation, since it extends the crash case there; the rule is set where the bucket is provisioned, as EnsureContainer never configures a bucket.
- hardening: provider-assumptions marks the repeated abort's NoSuchUpload path untested, since no unit test answers an abort with it.
- hardening (redirect at BRIEF): the architect asked for a standard container convention used by CI and mise alike, with no shell scripts; it is built in hardening, before the tags, because both providers' package docs describe the harness. It replaces port's three harness decisions (pin on CI's image: line, docker run, scripts/seaweedfs.sh).
- hardening: each compose service builds from compose/<service>/Dockerfile under a root compose.yml; the FROM line is its one pin; Dockerfile, not Containerfile, because docker and compose find it with no setting.
- hardening: readiness is the image's HEALTHCHECK where the base image can run a probe; distroless images get a Dockerfile with none and are waited on as running.
- hardening: stacks start with `docker compose up -d --wait --build`; configuration is COPY'd into the image, not bind-mounted.
- hardening: a test-only harness (go-storage's) keeps data on tmpfs; development stacks keep named volumes that <stack>:down keeps and <stack>:reset deletes.
- hardening: CI runs the same mise task a developer runs; currency reads Dockerfile FROM lines and skips build-only compose services; Renovate-style updaters stay excluded.
- hardening: go-storage's tasks are acceptance, acceptance:s3, acceptance:azureblob, up, down; seaweedfs:* retire; Azurite keeps --loose, under which azureblob's suite is proven.
- hardening: multi-part mise task names are <group>:<member>, colon, group first, workspace-wide (mise's own namespace separator; globs a group).
- hardening: the sweep runs in this task across sqlate, blobfs, and go-web-service, not as its own goal (architect); archived spikes keep their task names.
- hardening: one convention across the four repositories: no container_name; HEALTHCHECK --start-period=30s --start-interval=1s --interval=5s --timeout=3s --retries=5; Postgres probed over TCP (the first-start init server listens on the socket only); every published port overridable as <SERVICE>_PORT, and the settings that name it follow it (except go-web-service's, below).
- hardening: blobfs's up/down/reset became db:up/db:down/db:reset, as go-web-service's db: group covers the same Postgres-plus-Azurite stack; go-storage keeps up/down for its test harness, as round 3 settled.
- hardening: go-web-service's application settings keep their own ports rather than following POSTGRES_PORT/AZURITE_BLOB_PORT, as its README documents; changing how the service reads configuration is out of scope.
- hardening: volume names that repeat the project name (sqlate_sqlate-postgres) stay, since renaming would orphan developers' data.
- hardening: tutorials (sqlate and blobfs quick-starts) show the convention with their own example pins, which currency doesn't scan.
- hardening: the sqlate and blobfs quick-starts define db:up and db:reset, not db:down, because their cleanup step drops the volume, which is what reset does.
- hardening: Postgres's user, password, and database are ENV app/app/app in each Dockerfile, so the POSTGRES_USER/PASSWORD/DB compose overrides are dropped in sqlate, blobfs, and go-web-service; no setting ever followed them.
- hardening: SeaweedFS is healthy once curl --fail gets a SigV4-signed ListBuckets, because the gateway answers unsigned requests before it loads the credential.
- hardening: `mise run acceptance` runs acceptance:s3 and then acceptance:azureblob, each against a fresh service, and runs every provider even after one fails; acceptance:* run with GOWORK=off and -race -count=1 -v.
- hardening: go-storage's Azurite keeps --loose; blobfs's and go-web-service's keep their command without it, as before.
- hardening: go-web-service keeps its five observability services in one group file, compose/observability.yml, behind one profile; the convention is a Dockerfile per service, not a compose file per service.
- hardening: go-web-service's otel:up runs under set -e, so a compose failure for any service fails the task instead of passing once Mimir answers.
- hardening: currency's shared latest_tag returns crane's failure, so a registry error fails currency rather than reporting an empty latest.
- hardening: blobfs commits the go.work.sum hashes `mise run example` adds, so acceptance leaves the tree clean.
- hardening: blobfs's example takes go-storage v0.5.0 while go-storage/azureblob stays at v0.4.0, its latest tag until SHIP.
- hardening: the upgrade slices add no CHANGELOG entries, since the changelogs record library behavior; sqlate's go directives stay 1.27, as the minor has not moved; go-web-service's slab bump is a targeted go get, not the full upgrade sweep.
- hardening: sqlate's and blobfs's CHANGELOGs gain no entry for the stack, which is development-only; go-web-service's Unreleased Changed records it; go-storage records the local harness in azureblob v0.5.0 and s3 v0.1.0, not in the base CHANGELOG.
- hardening (second redirect at BRIEF): every time.Time a workspace library returns is in time.UTC, so JSON, cursors, logs, and == don't depend on the host; Postgres already stores timestamptz (UTC), so the work is on the read side.
- hardening: azureblob's Put/Get/Stat (GMT zone, or Local on GMT-abbreviated hosts) and s3's header paths convert to UTC before the tags; List's comment, which claimed they agreed, is corrected.
- hardening: go-storage's contract documents ModifiedAt as UTC, the Fake stamps UTC, and the conformance suite checks Location, released as base v0.6.0 that both providers require.
- hardening: database reads become UTC in two places: go-database/postgres sets pgx's TimestamptzCodec.ScanLocation to UTC on its connections, and sqlate's Scanner and Scalar convert any time.Time to UTC whatever driver produced it; a session TimeZone setting was ruled out because pgx ignores it when decoding.
- hardening: sqlate's history applied_at becomes timestamp with time zone, existing tables altered on the next migrate.
- hardening: go-core's slog handlers write UTC record times.
- hardening: versions are minor where returned times change or where a breaking requirement (go-core v0.6.0) is pulled into importers, patch where only a non-breaking requirement moves; go-web-service, an application, takes no tag.
- hardening: SHIP walks the coordinator's order layer by layer; each layer bumps to the layer below's new tags, merges, passes ci, and tags before the next (architect: the session carries every currency update its releases cause).
- hardening: no go-core clock abstraction; callers convert with .UTC().

## Pending edits

- coordinator · roadmap: consider a backlog goal for s3 `Put`'s memory: transfermanager copies the first part out of `Put`'s decision buffer unpooled, and for an unknown size that buffer grows by doubling, so a multipart `Put` holds part_size × (concurrency + 3) or (concurrency + 4) while it starts, against the part_size × (concurrency + 2) it reads ahead (56 and 64 MiB against 48 MiB at the defaults).
- coordinator · roadmap: consider a backlog goal for transfermanager's standard-library `log` output when a multipart upload's completion fails, which bypasses the application's logger.
- coordinator · roadmap: consider a backlog goal for go-web-service's application settings to follow `POSTGRES_PORT` and `AZURITE_BLOB_PORT`, as sqlate's `SQLATE_DSN` and blobfs's `BLOBFS_DSN` and `BLOBFS_STORAGE_ENDPOINT` now do. Its README documents that moving a port splits the two until `APP_DATABASE_PORT` or `APP_STORAGE_ENDPOINT` follows.
- coordinator · references.toml: under the archived spikes, after `[repos.spike-cli-architecture]`, add `[repos.spike-s3-storage]` with `remote = "https://github.com/JaimeStill/spike-s3-storage.git"` and `archived = true`.
- coordinator · references.md, "Experiments — archived spikes": add `### spike-s3-storage`: "Asked whether go-storage's `Client` interface holds over S3, with an aws-sdk-go-v2 provider validated against SeaweedFS's S3 gateway, and whether the blobfs CLI runs unchanged on it. Its `s3` module became go-storage's `s3` provider."
- spike-s3-storage · GitHub: archive the remote, JaimeStill/spike-s3-storage.
- coordinator · context/service-organization.md, "Anticipated services and their providers": replace "an Azure Blob provider (azurite ↔ Azure Blob) and an S3 provider (minio ↔ S3)" with "go-storage's `azureblob` provider (Azurite ↔ Azure Blob) and its `s3` provider (SeaweedFS ↔ S3)".
- architecture · standards/go-elemental/principles/release-and-ci.md, "One check per repository", CI paragraph: "go-storage's `acceptance` job runs azureblob against Azurite" becomes "go-storage's `acceptance` job runs `mise run acceptance`, azureblob against Azurite and s3 against SeaweedFS". After "they run on a developer's machine ([tests and documentation](tests-and-docs.md))." add: "A job that needs containers runs the same mise task a developer runs, and the task prints the containers' logs when it fails."
- architecture · standards/go-elemental/principles/release-and-ci.md, "Currency and upgrade", the `mise run currency` bullet: "and where the repository has one, every container image tag," becomes "and where the repository has one, every container image tag, read from each compose service's Dockerfile `FROM` line,".
- architecture · standards/go-elemental/principles/release-and-ci.md: add a section `## Containers for tests and development` before `## Tasks`:
  "A repository that runs a suite or a development stack against containers defines them with Docker Compose, in one layout:
  - A root `compose.yml` names the project with `name:` and includes the files under `compose/`, one per service group.
  - Every service builds from `compose/<service>/Dockerfile`. Its `FROM` line is the service's one image pin; no `image:` line names an image, in compose or in CI.
  - The Dockerfile carries the service's configuration: `ENV` and `CMD`, and `COPY` for configuration files. Compose adds only the build context, the published port, and the data mount, and bind-mounts no configuration.
  - Where the base image can run a probe, a `HEALTHCHECK` in the Dockerfile defines readiness, with `--start-period=30s --start-interval=1s --interval=5s --timeout=3s --retries=5`. A distroless image has no `HEALTHCHECK`, and compose waits on it as running.
  - A stack starts with `docker compose up -d --wait --build` and stops with `docker compose down`.
  - A test-only harness, go-storage's, keeps its data on tmpfs, so every start is empty. A development stack keeps its data in named volumes, which `<group>:down` keeps and `<group>:reset` deletes.
  - Every published port binds `127.0.0.1` and moves with an environment variable (`POSTGRES_PORT`, `AZURITE_BLOB_PORT`), and the repository's settings that name the port follow the variable. go-web-service's application settings are the exception: its README documents that they keep their own ports.
  - mise tasks drive compose directly; no shell script backs a harness."
- architecture · standards/go-elemental/principles/release-and-ci.md, "Tasks": "plus `integration` or `acceptance` where the repository runs such a suite locally and tasks that start and stop its compose stack." becomes "plus `integration` or `acceptance` where the repository runs such a suite locally, and tasks that start and stop the containers it runs against: `db:up`, `db:down`, and `db:reset` for sqlate's, blobfs's, and go-web-service's development stacks, with go-web-service's `otel:*` and `stack:*` beside them, and `up`, `down`, `acceptance:s3`, and `acceptance:azureblob` for go-storage's test harness." Then add: "A task name with more than one part is `<group>:<member>`, group first, joined by a colon, which is mise's namespace separator, so `mise run 'db:*'` matches a group."
- architecture · standards/go-elemental/principles/tests-and-docs.md, "Integration and acceptance suites, and where each runs": replace the "go-storage's azureblob acceptance tests" bullet with: "**go-storage's acceptance tests** run the storage conformance suite against a real service, `azureblob`'s when `AZUREBLOB_TEST_ENDPOINT` names one and `s3`'s when `S3_TEST_ENDPOINT` does, and skip otherwise. `mise run acceptance:azureblob` and `mise run acceptance:s3` each start their provider's service from the compose harness (Azurite or SeaweedFS), run that module's tests against it, and stop it whatever the result; `mise run acceptance` runs both. CI's `acceptance` job runs `mise run acceptance` on every pull request into `main` and every push to it."
- architecture · principles/rolling-currency.md, "Which versions the principle covers": "the container images a compose file or a CI workflow starts." becomes "the container images a compose file or a CI workflow starts, each pinned once: a compose service's pin is the `FROM` line of the Dockerfile it builds from, which currency reads."
- architecture · standards/go-elemental/principles/dependencies.md, the "No provider in a base" bullet: "go-storage's `azureblob`" becomes "go-storage's `azureblob` and `s3`".
- architecture · standards/go-elemental/principles/dependencies.md, "Sourcing", after the paragraph ending "as a stated v0 exception that passes every other marker.": add that go-storage's README admits `s3`'s `feature/s3/transfermanager` as a stated v0 exception in a provider sub-module, sourced under the specification category (S3's multipart upload protocol).
- architecture · standards/go-elemental/README.md, the go-storage row: "with the Azure Blob provider as a sub-module" becomes "with the Azure Blob and S3 providers as sub-modules".
