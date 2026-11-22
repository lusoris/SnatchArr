# shellcheck shell=bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Bash major-version predicate sourced by workflow_refs.sh, workflow-check.sh and
# workflow_refs_test.sh before bash-4-only syntax runs.
# Platform (HISS-21): written in bash-3.2-safe syntax so stock macOS bash 3.2 can source it cleanly;
# defines bash_major_supported only and runs nothing at source time.

# bash_major_supported MAJOR: return 0 when MAJOR is an integer >= 4, else 1.
bash_major_supported() {
  case "${1:-}" in
    '' | *[!0-9]*) return 1 ;;
  esac
  [ "$1" -ge 4 ]
}
