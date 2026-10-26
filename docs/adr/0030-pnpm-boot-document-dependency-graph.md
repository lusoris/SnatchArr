<!-- markdownlint-disable MD013 -->
<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0030: pnpm boot document and GitHub dependency graph snapshot submission

- Status: Accepted
- Date: 2026-10-26

## Context

On 2026-10-26, audit of the GitHub dependency graph and SBOM exports for `SnatchArr` revealed a blind spot in frontend dependency tracking:

1. **Two YAML documents in `web/pnpm-lock.yaml`**: Because `web/package.json` pins `packageManager: pnpm@12.14.0`, pnpm 12 emits a multi-document lockfile (`https://pnpm.io/lockfile`). Document 1 (lines 1–157) is the self-hosting boot document containing only `packageManagerDependencies` (`pnpm@12.14.0`) and platform-specific runner packages (`@pnpm/exe.*`). Document 2 (lines 158–4512) contains the actual 442 project packages.
2. **GitHub Dependency Graph truncation**: GitHub's built-in dependency graph parser reads only the first YAML document of `pnpm-lock.yaml`. Consequently, the exported npm SBOM lists only 15 packages (the pnpm bootstrap shims), and Dependabot is blind to the 442 application dependencies in document 2.
3. **Local and CI scanner coverage**:
   - `tools/ci/trivy_scan.py` (Trivy 0.75.0) parses document 2 during CI PR checks.
   - `pnpm audit` in `.github/workflows/security.yml` (`web-audit` job) executes against document 2 in nightly security scans.
   - Trivy 0.75.0 `fs --format github --include-dev-deps` resolves document 2: 414 packages with `--include-dev-deps` (311 without), covering all 42 direct dependencies in `web/package.json`.

## Decision

1. **Keep two YAML documents**: Retain `packageManager: pnpm@12.14.0` in `web/package.json` and keep the multi-document `web/pnpm-lock.yaml` to ensure strict pnpm version enforcement across local workstations and CI environments.
2. **Submit Trivy dependency graph snapshot**: Implement automated dependency snapshot generation via `tools/ci/dependency_snapshot.py` and submit it to GitHub's Dependency Submission API (`POST repos/{owner}/{repo}/dependency-graph/snapshots`) from `.github/workflows/security.yml` in a dedicated `dependency-submission` job.
3. **Planted-case and direct-dependency verification**:
   - Before scanning `web/`, `tools/ci/dependency_snapshot.py` runs a planted test case against a synthetic lockfile containing only the boot document. The check must reject this planted case; if accepted, the script exits immediately with status 2 ("check not proven").
   - For the real scan, `tools/ci/dependency_snapshot.py` verifies that every direct dependency and devDependency declared in `web/package.json` appears in the snapshot's resolved package URLs (`pkg:npm/<urlencoded name>@ver`).
   - The script rewrites the snapshot manifest key and `file.source_location` to `web/pnpm-lock.yaml` so the graph reflects the repository path.

## Options Considered

1. **`pmOnFail: ignore` in pnpm configuration**:
   - *Result*: Rejected. While setting `pmOnFail: ignore` allows single-document lockfiles in certain setups, it disables automatic enforcement of the pinned pnpm version, permitting version drift between developers.
2. **pnpm lockfile suppression settings**:
   - *Result*: Rejected. The pnpm lockfile documentation (`https://pnpm.io/lockfile`, read 2026-10-26) names `pmOnFail: ignore` as the only way to drop document 1, and that removes the version pinning; no other setting was found.
3. **Accept the dependency graph blind spot**:
   - *Result*: Rejected. Relying solely on local linters leaves Dependabot alerts and GitHub security advisories inactive for frontend dependencies.

## Defense in Depth (Two-Barrier Rule)

Frontend supply-chain security is protected by three independent defenses:

1. **PR Gate**: `tools/ci/trivy_scan.py` scans all lockfiles (Go, Rust, and pnpm document 2) on pull requests and blocks on any HIGH/CRITICAL vulnerability with an available fix.
2. **Scheduled Audit**: `pnpm audit` runs nightly in `.github/workflows/security.yml` (`web-audit`) and opens issues on main failures.
3. **Dependabot Alerting**: Automated snapshot submission populates the GitHub dependency graph, enabling continuous Dependabot alerting against newly disclosed CVEs.

## Revisit Trigger

This decision will be revisited when:

- GitHub's native dependency graph parser adds support for multi-document YAML in `pnpm-lock.yaml`, or
- pnpm deprecates or modifies the multi-document boot document layout in future releases.
