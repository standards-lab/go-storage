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

slices 0/6 committed · standards — · spec — · editor —

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

## Pending edits

- coordinator · roadmap: consider a backlog goal for local Azurite start/stop/acceptance tasks in go-storage.
- hardening · go-storage README: state transfermanager's v0 exception beside the README's link to the standard (dependencies.md).
