<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->

# Screen reader release check

Before each minor or major release, run one manual pass with Orca, the GNOME screen reader, against the served release candidate. Use keyboard navigation throughout and record the application version, browser version, Orca version, results, and any defects in the release evidence.

1. Navigate by landmarks. Confirm that the main content and navigation regions have useful names and that the skip link reaches the main content.
2. Open the heading list. Confirm one level-one heading per page and a meaningful heading order.
3. On `/account` and the instance creation form on `/instances`, move through every form control. Confirm that Orca announces each label, purpose, required state, and validation error. This build embeds the creation form in `/instances`; it has no `/instances/new` route.
4. On `/activity`, trigger a search in the test environment and confirm that the live region announces the new activity without moving keyboard focus.

Record pass or fail for each step in the release evidence. Resolve failures and repeat the affected steps before releasing. Automated layout and axe checks complement this pass; they cannot establish how a screen reader announces an interaction.
