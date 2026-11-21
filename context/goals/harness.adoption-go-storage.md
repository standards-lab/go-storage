# goal · harness.adoption-go-storage

- **State:** building
- **Task:** adopt
- **Branch:** adopt

## Tasks

1. [ ] adopt

## Task brief · adopt

**Problem**

go-storage trails its dependencies and doesn't run marathon's lints. Its capability map fails
lint-context. About 1,040 comment lines are outside the five kinds, and nothing makes a
suppression give its reason.

**Behaviors**
1. The currency command reports nothing trailing. The workspace checksum file is committed,
   including its three pending lines. Prose that names a pinned version (the SeaweedFS version in
   the provider assumptions, transfermanager's version in the s3 package docs) names the new one.
   The s3 module's CHANGELOG records its requirement changes under Unreleased. The acceptance
   tests pass on the new images.
2. The marathon 0.22.0 pin puts lint-context and lint-references on PATH. Currency reports a newer
   marathon tag.
3. The check also runs both lints and fails when either fails. A lint missing from PATH fails the
   check with a message naming `mise install`. The check's description names what it runs. CI
   installs the mise tools before the check.
4. lint-context passes: each map entry is marked `(built)` with a link to its home, or `(planned)`
   (managed identity); no prose sits inside the map; the provider-assumptions note moves to
   `docs/` unchanged, and every pointer names its new page; the `Delete` snapshot limit lives in
   azureblob's package docs.
5. Every comment in Go source and tests is one of the five kinds. Package docs and docs on exported
   identifiers stay; docs on unexported identifiers and test functions go. Reasoning with no other
   home moves into names, exported docs, or `docs/`.
6. Every comment in scripts and configuration is one of the five kinds. The currency script's
   sections become named functions.
7. golangci-lint enables nolintlint with require-explanation and require-specific.
8. STANDARDS.md gains a comment rule citing the comments principle for source, tests, scripts, and
   every configuration file.
9. The changes stay under Unreleased. CI passes.

**Test seams**
- `mise run check`, `mise run currency`, and `mise run acceptance`.

**Slices**
1. Upgrade go-storage. Done when currency exits 0, the check passes, and acceptance passes.
2. Context.
3. Pin, check, and CI.
4. Base module and storagetest comments, nolintlint, STANDARDS rule.
5. azureblob and s3 comments.
6. Script and configuration comments.

**Out of scope**

A Markdown link check, budgets for `docs/`, rewriting exported docs, releases, feature work.

**Door**

Two-way. Nothing is tagged.

## Progress

slices 6/6 committed · standards ✓ · spec ✓ · editor ✓

## Decisions

None.

## Pending edits

None.
