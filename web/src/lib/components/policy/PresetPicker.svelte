<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import { PRESETS, estimateSearchesPerHour, type SnatchPolicy } from './presets.js';

	interface Props {
		draft: SnatchPolicy;
		onSelect: (values: Partial<SnatchPolicy>) => void;
	}

	const { draft, onSelect }: Props = $props();

	const estimate = $derived(estimateSearchesPerHour(draft));
	const cap = $derived(draft.hourly_cap ?? 0);
</script>

<div class="card flex flex-col gap-3">
	<div class="flex flex-col gap-1">
		<span class="text-sm font-semibold">{m.policy_presets_title()}</span>
		<p class="text-xs text-subtle">{m.policy_presets_hint()}</p>
	</div>

	<div class="grid grid-cols-2 gap-2 sm:grid-cols-4">
		{#each PRESETS as preset (preset.key)}
			<button
				type="button"
				class="btn flex flex-col items-start gap-1 p-3 text-left"
				onclick={() => onSelect(preset.values)}
				title={preset.description()}
			>
				<span class="text-xs font-semibold">{preset.label()}</span>
				<span class="line-clamp-2 text-xs text-subtle">{preset.description()}</span>
			</button>
		{/each}
	</div>

	<p class="text-brand text-xs font-medium" data-testid="estimate-text">
		{m.policy_searches_estimate({ count: estimate, cap })}
	</p>
</div>
