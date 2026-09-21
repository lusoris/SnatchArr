<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { m } from '$lib/paraglide/messages.js';
	import { staminaLevel, staminaPercent } from '$lib/stamina.js';

	interface Props {
		label: string;
		used: number;
		cap: number;
	}

	const { label, used, cap }: Props = $props();
	const level = $derived(staminaLevel(used, cap));
	const percent = $derived(staminaPercent(used, cap));
	const levelText = $derived(
		level === 'fresh'
			? m.stamina_fresh()
			: level === 'tired'
				? m.stamina_tired()
				: m.stamina_spent()
	);
</script>

<div class="stamina" data-level={level}>
	<div class="flex items-baseline justify-between text-sm">
		<span class="font-medium">{label}</span>
		<span class="text-subtle">{m.stamina_used({ used, cap })} · {levelText}</span>
	</div>
	<div
		role="meter"
		aria-label={label}
		aria-valuemin={0}
		aria-valuemax={cap}
		aria-valuenow={Math.min(used, cap)}
		aria-valuetext={`${m.stamina_used({ used, cap })}, ${levelText}`}
		class="bar mt-1 h-2 w-full overflow-hidden rounded-full"
	>
		<div class="fill h-full rounded-full" style:width={`${percent}%`}></div>
	</div>
</div>

<style>
	.bar {
		background: var(--color-muted, oklch(0.9 0.01 260));
	}
	.fill {
		background: var(--stamina-color, oklch(0.62 0.16 160));
		transition: width 300ms ease;
	}
	.stamina[data-level='tired'] {
		--stamina-color: oklch(0.75 0.16 80);
	}
	.stamina[data-level='spent'] {
		--stamina-color: oklch(0.6 0.2 25);
	}
	@media (prefers-reduced-motion: reduce) {
		.fill {
			transition: none;
		}
	}
</style>
