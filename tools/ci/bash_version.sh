# shellcheck shell=bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Bash version predicate sourced by workflow_refs.sh, workflow-check.sh and
# workflow_refs_test.sh before bash-4.4-only syntax (such as mapfile -d) runs.
# Platform (HISS-21): written in bash-3.2-safe syntax so stock macOS bash 3.2 can source it cleanly;
# defines bash_version_supported only and runs nothing at source time.

# bash_version_supported MAJOR MINOR: return 0 when MAJOR and MINOR are integers for bash 4.4 or newer, else 1.
bash_version_supported() {
  case "${1:-}" in
    '' | *[!0-9]*) return 1 ;;
  esac
  case "${2:-}" in
    '' | *[!0-9]*) return 1 ;;
  esac
  [ "$1" -gt 4 ] || { [ "$1" -eq 4 ] && [ "$2" -ge 4 ]; }
}
