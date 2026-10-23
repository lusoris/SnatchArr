<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { formatVersionLabel, systemStatusQuery } from '#lib/queries/system.js';

	const systemStatus = systemStatusQuery();
	const versionLabel = $derived(
		systemStatus.data ? formatVersionLabel(systemStatus.data.version, systemStatus.data.commit) : ''
	);
</script>

{#if versionLabel}
	<div class="px-2 text-[11px] text-subtle" data-testid="shell-version">
		{versionLabel}
	</div>
{/if}
