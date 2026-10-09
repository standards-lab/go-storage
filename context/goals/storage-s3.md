# goal · storage-s3

- **State:** building
- **Task:** hardening
- **Branch:** hardening

## Tasks

1. [x] port
2. [ ] hardening

## Task brief · hardening

Repositories: go-storage (root), sqlate, blobfs, go-web-service; branch `hardening` in each.

```
## Task brief · storage-s3 · hardening (revised at BRIEF)
Problem       s3 falls short of azureblob's provider bar (slices 1–6, built).
              And the workspace has no convention for test and dev containers:
              go-storage starts SeaweedFS with a shell script and Azurite with an
              inline loop, azureblob has no local task, image pins live on
              image: lines CI duplicates, and task names mix db-up with
              seaweedfs:start.
Behaviors     1–9. As approved (try_timeout; resuming Get; 412/deleted ->
                 ErrNotFound; stalled -> ErrUnavailable; concurrency and stated
                 bounds; "s3:" prefix; SeaweedFS acceptance of resumption; docs;
                 changelogs dated for s3/v0.1.0 and azureblob/v0.5.0).
              10. Every compose service in go-storage, sqlate, blobfs, and
                 go-web-service builds from a Dockerfile at
                 compose/<service>/Dockerfile under a root compose.yml; its FROM
                 line is the service's one image pin; its configuration is
                 COPY'd in; where the base image can run a probe, a HEALTHCHECK
                 in the Dockerfile defines readiness (distroless images have
                 none and are waited on as running).
              11. Stacks start with `docker compose up -d --wait --build` and
                 stop with `docker compose down`; a test-only harness
                 (go-storage's) keeps data on tmpfs, so every start is empty;
                 development stacks keep named volumes that <stack>:down keeps
                 and <stack>:reset deletes.
              12. Multi-part mise tasks are named <group>:<member> with a colon,
                 group first, in all four repositories (db:up, db:down,
                 db:reset, db:state, otel:*, stack:*, acceptance:s3,
                 acceptance:azureblob); go-storage's seaweedfs:* retire; no doc,
                 test comment, or workflow names an old task.
              13. go-storage: `mise run acceptance` (every provider) and
                 acceptance:s3 / acceptance:azureblob each bring their service
                 up healthy, run that module's tests with its endpoint set, and
                 tear down pass or fail; azureblob runs locally for the first
                 time. No shell script backs the harness.
              14. Each repository's CI job that needs containers runs the same
                 mise task a developer runs, with container logs on failure.
              15. Each repository's currency reports a Dockerfile FROM pin that
                 trails, and ignores build-only compose services; currency exits
                 0 in all four.
              16. READMEs, package docs, compose READMEs, STANDARDS.md, and
                 changelogs describe the tasks and the convention.
Test seams    as approved; plus each repository's mise tasks against Docker
              (acceptance, integration, db:up), mise run check and currency
Slices        1–6. committed (go-storage)
              7. upgrade sqlate: go 1.27.2; currency exits 0, check passes
              8. upgrade blobfs: go 1.27.2, x/text v0.43.0, example on
                 go-storage v0.5.0; currency exits 0, check passes
              9. upgrade go-web-service: slab x/sys v0.49.0, x/term v0.47.0;
                 currency exits 0, check passes
              10. go-storage harness: SeaweedFS and Azurite Dockerfiles,
                 compose.yml, acceptance tasks, currency FROM scan, script
                 retired; demo: acceptance green for s3 and azureblob locally
              11. go-storage CI and docs: the acceptance job runs mise run
                 acceptance; README, both doc.go, STANDARDS.md; demo: the PR's
                 acceptance job green
              12. sqlate convention: postgres Dockerfile, db:* tasks, currency,
                 CI, docs; demo: db:up healthy, live tests green
              13. blobfs convention: postgres and Azurite Dockerfiles, tasks,
                 currency, CI, docs; demo: acceptance green
              14. go-web-service convention: postgres, Azurite, otel collector,
                 loki, tempo, mimir, grafana Dockerfiles with configs COPY'd;
                 db:/otel:/stack: tasks; integration task and CI job; currency;
                 docs, slab docs, test comments; demo: integration green,
                 stack:up healthy
              15. release re-check: both changelogs and module docs current;
                 check green in all four
Out of scope  a base go-storage release; tags in sqlate, blobfs, go-web-service;
              Put's buffering; transfermanager's log output; archived spikes'
              task names; spike-model-hosting; architecture-page edits (sync)
Door          two-way through slice 15 (images, tasks, CI revert with the
              merges); one-way at SHIP for the tags
Release       s3/v0.1.0, azureblob/v0.5.0
```

## Progress

slices 6/15 committed (go-storage: d3b165f, 8afbb0d, 13179c2, 4c12efe, 22fc733, 445b840; reviews and editor on 1–6: a911618, 82e23f5, 31a5d12) · standards — · spec — · editor —

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

## Pending edits

- coordinator · roadmap: consider a backlog goal for s3 `Put`'s memory: transfermanager copies the first part out of `Put`'s decision buffer unpooled, and for an unknown size that buffer grows by doubling, so a multipart `Put` holds part_size × (concurrency + 3) or (concurrency + 4) while it starts, against the part_size × (concurrency + 2) it reads ahead (56 and 64 MiB against 48 MiB at the defaults).
- coordinator · roadmap: consider a backlog goal for transfermanager's standard-library `log` output when a multipart upload's completion fails, which bypasses the application's logger.
- coordinator · references.toml: under the archived spikes, add `[repos.spike-s3-storage]` with `remote = "https://github.com/JaimeStill/spike-s3-storage.git"` and `archived = true`.
- coordinator · references.md, "Experiments — archived spikes": add `### spike-s3-storage`: "Asked whether go-storage's `Client` interface holds over S3, with an aws-sdk-go-v2 provider validated against SeaweedFS's S3 gateway, and whether the blobfs CLI runs unchanged on it. Its `s3` module became go-storage's `s3` provider."
- spike-s3-storage · GitHub: archive the remote, JaimeStill/spike-s3-storage.
- coordinator · context/service-organization.md, "Anticipated services and their providers": replace "an Azure Blob provider (azurite ↔ Azure Blob) and an S3 provider (minio ↔ S3)" with "go-storage's `azureblob` provider (Azurite ↔ Azure Blob) and its `s3` provider (SeaweedFS ↔ S3)".
- architecture · standards/go-elemental/principles/release-and-ci.md, "CI": "go-storage's `acceptance` job runs azureblob against Azurite" becomes "go-storage's `acceptance` job runs azureblob against Azurite and s3 against SeaweedFS".
- architecture · standards/go-elemental/principles/release-and-ci.md, "Tasks": "and tasks that start and stop its compose stack" becomes "and tasks that start and stop the services the suite runs against, a compose stack or, in go-storage, a SeaweedFS container (`seaweedfs:start`, `seaweedfs:stop`)".
- architecture · standards/go-elemental/principles/tests-and-docs.md, "Integration and acceptance suites": replace the "go-storage's azureblob acceptance tests" bullet with "go-storage's acceptance tests" — `azureblob`'s run the storage conformance suite against a real service when `AZUREBLOB_TEST_ENDPOINT` names one and `s3`'s when `S3_TEST_ENDPOINT` does, skipping otherwise; CI's `acceptance` job runs them against Azurite and SeaweedFS containers on every pull request into `main` and every push to it; locally, `mise run seaweedfs:start`, `mise run acceptance`, and `mise run seaweedfs:stop` run `s3`'s, and no task runs `azureblob`'s.
- architecture · standards/go-elemental/principles/dependencies.md, the "No provider in a base" bullet: "go-storage's `azureblob`" becomes "go-storage's `azureblob` and `s3`".
- architecture · standards/go-elemental/principles/dependencies.md, "Sourcing", after go-observability's worked case: add that go-storage's README admits `s3`'s `feature/s3/transfermanager` as a stated v0 exception in a provider sub-module, sourced under the specification category (S3's multipart upload protocol).
- architecture · standards/go-elemental/README.md, the go-storage row: "with the Azure Blob provider as a sub-module" becomes "with the Azure Blob and S3 providers as sub-modules".
