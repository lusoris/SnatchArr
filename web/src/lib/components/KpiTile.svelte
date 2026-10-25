<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import StatusIcon, { type StatusKind } from '#lib/components/StatusIcon.svelte';

	interface Props {
		label: string;
		heroValue: string;
		subtitle?: string;
		status?: StatusKind;
		statusLabel?: string;
		accentClass?: string;
		loading?: boolean;
		error?: unknown;
	}

	const {
		label,
		heroValue,
		subtitle,
		status,
		statusLabel,
		accentClass = 'text-fg',
		loading = false,
		error = null
	}: Props = $props();
</script>

{#if loading}
	<div
		data-testid="skeleton"
		class="card flex min-w-0 flex-col justify-between gap-2 p-4"
		aria-busy="true"
	>
		<div class="bg-muted h-4 w-24 animate-pulse rounded"></div>
		<div class="bg-muted h-8 w-20 animate-pulse rounded"></div>
		<div class="bg-muted h-3 w-32 animate-pulse rounded"></div>
	</div>
{:else if error}
	<div class="card min-w-0 p-4">
		<ProblemNote {error} />
	</div>
{:else}
	<div class="card flex min-w-0 flex-col justify-between gap-2 p-4">
		<div class="flex min-w-0 items-center justify-between gap-2">
			<span class="truncate text-xs font-semibold tracking-wider text-subtle uppercase"
				>{label}</span
			>
			{#if status && statusLabel}
				<StatusIcon {status} label={statusLabel} size={18} />
			{/if}
		</div>
		<div class="truncate text-2xl font-bold {accentClass}">
			{heroValue}
		</div>
		{#if subtitle}
			<p class="truncate text-xs text-subtle" title={subtitle}>{subtitle}</p>
		{/if}
	</div>
{/if}
