# goal · storage-s3

- **State:** building
- **Task:** hardening
- **Branch:** hardening

## Tasks

1. [x] port
2. [ ] hardening

## Task brief · hardening

```
## Task brief · storage-s3 · hardening
Problem       s3 is in go-storage and accepted in CI, but it falls short of
              azureblob's provider bar: no per-try deadline, a Get body that
              dies with its first stalled try, an unstated read-ahead bound,
              unprefixed multipart errors, and docs that still call the tier
              a proposal. Until it meets the bar, the tier isn't validated and
              s3 can't be released.
Behaviors     1. try_timeout is a positive Go duration bounding each try of
                 every s3 request, a Get body's reads within that try
                 included; it is the stdlib HTTP client timeout, with no
                 custom SDK middleware. Unset means no deadline; a malformed
                 or non-positive value fails construction. A stalled try is
                 retried; all tries spent is ErrUnavailable.
              2. A Get body whose try fails mid-read resumes from its offset
                 with a ranged GetObject conditioned (If-Match) on the first
                 response's ETag, up to max_retries resumes per read, and
                 yields bytes identical to the object. With max_retries 0 it
                 does not resume.
              3. An object replaced (412) or deleted before a resume fails the
                 read with ErrNotFound.
              4. A body that stalls on every try fails the read with
                 ErrUnavailable once its resumes are spent.
              5. concurrency (default 4, range 1–32; out of range or malformed
                 fails construction) bounds a multipart Put's parts in flight.
                 The docs state the per-Put memory and read-ahead:
                 part_size × (concurrency + 2), 48 MiB at the defaults, and
                 that a declared Size can raise the part size.
              6. Every multipart failure Put returns (cancellation, a part,
                 completion) begins "s3:" and keeps its classification;
                 cancellation stays unclassified.
              7. Against SeaweedFS, acceptance shows a read paused past a short
                 try_timeout resuming to identical bytes, and an object
                 replaced mid-read failing ErrNotFound.
              8. Docs: README names s3 the second provider and states
                 transfermanager's v0 exception beside its link to the
                 standard; design.md calls the tier validated, covers s3's
                 try_timeout and upload defaults, states that a resumed tail
                 goes unvalidated by checksum, and recommends an
                 AbortIncompleteMultipartUpload lifecycle rule for uploads a
                 killed Put orphans; provider-assumptions gives evidence for
                 the three S3 claims (conditional ranged GET proven) and the
                 SeaweedFS-vs-AWS differences; context README and STANDARDS.md
                 name s3 under timeouts; s3's package docs no longer claim it
                 reads no environment variable.
              9. s3/CHANGELOG.md and azureblob/CHANGELOG.md are dated for
                 s3/v0.1.0 and azureblob/v0.5.0, with tag links; check passes.
Test seams    s3's exported Client against a scripted loopback S3 service
              (stall, range, If-Match, parts in flight); s3's acceptance suite
              against SeaweedFS; mise run check as the gate
Slices        1. per-try deadline (+ env-var doc fix); demo: a stalled
                 scripted service gives ErrUnavailable within the budget
              2. resuming Get body, unit and acceptance; demo: resume and
                 replace tests pass, locally against SeaweedFS too
              3. concurrency option and stated bound; demo: in-flight parts
                 never exceed concurrency
              4. "s3:" prefix on multipart failures; demo: failure tests
              5. docs; demo: check green, docs read for coherence
              6. release prep: date both changelogs; demo: check green
Out of scope  a base go-storage release; removing Put's double-buffered first
              part; transfermanager's stdlib log output; architecture-page and
              catalog edits (sync's pending edits); local Azurite tasks
Door          two-way through slice 6; one-way at SHIP: pushed tags are pinned
              by the module proxy and checksum database and never re-cut
Release       s3/v0.1.0, azureblob/v0.5.0
```

## Progress

slices 6/6 committed (d3b165f, 8afbb0d, 13179c2, 4c12efe, 22fc733, 445b840) · standards ✓ (a911618, 82e23f5) · spec ✓ (no gaps) · editor ✓

## Decisions

- port: SeaweedFS is pinned once, on CI's `image:` line, and seaweedfs:start reads it, because currency doesn't scan scripts, so a pin in a script would fall behind without anyone noticing.
- port: the spike's `acceptance` mise task comes along with seaweedfs:start/stop, because release-and-ci asks for a local acceptance task.
- port: the harness is docker run, as for Azurite; no new harness library.
- plan: port's brief was written at plan, so start re-runs currency and presents it for approval.
- port: currency re-run at start (Go 1.27.2, go-storage v0.5.0, AWS SDK patch releases only); the brief covered it, so no round.
- port: azureblob's changelog marks the go-storage v0.5.0 requirement Breaking, because it pulls go-core v0.6.0 into an importer's build.
- port: `scripts/seaweedfs.sh` takes the image from `$image` or reads it from `ci.yml`'s one `image:` line, and CI's start step calls the script, so the readiness wait is written once.
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

## Pending edits

- coordinator · roadmap: consider a backlog goal for local Azurite start/stop/acceptance tasks in go-storage.
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
