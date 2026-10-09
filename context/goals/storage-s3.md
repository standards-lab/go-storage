# goal · storage-s3

- **State:** idle
- **Task:** none
- **Branch:** none

## Tasks

1. [x] port
2. [ ] hardening

## Task brief · port

```
## Task brief · storage-s3 · port
Problem       go-storage has one provider, so its standard tier is unvalidated.
              The spike proved an S3 provider on SeaweedFS, but it lives outside
              go-storage on a stale base (go-storage v0.4.0) and no CI runs it.
Behaviors     1. go-storage's currency exits 0: Go 1.27.2, and azureblob requires
                 go-storage v0.5.0, noted under azureblob's Unreleased; check passes.
              2. github.com/standards-lab/go-storage/s3 holds the spike's s3 module
                 at 2c53540, requiring go-storage v0.5.0 and the latest of each AWS
                 requirement; nothing names the spike's module path.
              3. The check covers s3 standalone (workspace off) and passes; the root
                 workspace resolves s3 with the base and azureblob.
              4. With S3_TEST_ENDPOINT unset, s3's unit tests pass and its acceptance
                 tests skip.
              5. seaweedfs:start brings up chrislusf/seaweedfs:4.48 (admin/secret,
                 no bucket auto-creation) and returns once a signed bucket listing
                 succeeds; a second start is a no-op; seaweedfs:stop removes it and
                 its data.
              6. acceptance runs s3's tests, conformance suite included, against the
                 running harness; they pass and none skip.
              7. CI's acceptance job starts SeaweedFS beside Azurite and runs s3 with
                 S3_TEST_ENDPOINT set; s3 and azureblob pass, s3's acceptance tests
                 run rather than skip.
              8. Currency reports the SeaweedFS pin from its single image: line, and
                 exits 0 at 4.48.
              9. s3 has its own changelog in azureblob's layout, with an Unreleased
                 section recording the provider's arrival.
Test seams    storagetest's conformance suite (Run, RunMissingContainer) over the
              s3 client, against SeaweedFS; mise run check and currency as gates
Slices        1. upgrade go-storage: Go 1.27.2, azureblob on go-storage v0.5.0;
                 done when currency exits 0 and check passes
              2. s3 in go-storage: module at the new path, current requirements, in
                 the workspace and the check, changelog; demo: check green, unit
                 tier passes, acceptance skips
              3. local harness: seaweedfs start/stop and acceptance; demo: start,
                 acceptance green, stop
              4. CI acceptance: SeaweedFS beside Azurite; demo: the pull request's
                 acceptance job green with s3's acceptance tests run
Out of scope  hardening's work (per-try deadline, resuming Get body, concurrency
              option, s3: error prefix, docs, s3/v0.1.0); the spike's app module,
              USAGE, evidence, compose stack; Azurite mise tasks; tagging azureblob;
              sync edits
Door          two-way: nothing is tagged; the module, tasks, and CI step revert
              with the merge
```

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

## Pending edits

- hardening plan: decide whether to tag azureblob/v0.5.0, since port moves azureblob onto go-storage v0.5.0 (release ripple, release-and-ci.md).
- coordinator · roadmap: consider a backlog goal for local Azurite start/stop/acceptance tasks in go-storage.
- hardening · go-storage README: state transfermanager's v0 exception beside the README's link to the standard (dependencies.md).
