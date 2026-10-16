<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import type { components } from '#lib/api/schema.js';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { formatWhen } from '#lib/stamina.js';

	type Instance = components['schemas']['Instance'];

	interface Props {
		instance: Pick<Instance, 'last_check_at' | 'last_error' | 'last_seen_version'>;
	}

	const { instance }: Props = $props();
</script>

{#if instance.last_error}
	<p role="status" class="flex items-center gap-1.5 text-sm text-danger">
		<svg
			class="h-4 w-4 shrink-0"
			viewBox="0 0 16 16"
			fill="none"
			stroke="currentColor"
			stroke-width="2"
			stroke-linecap="round"
			stroke-linejoin="round"
			aria-hidden="true"
		>
			<circle cx="8" cy="8" r="7" />
			<line x1="8" y1="5" x2="8" y2="8" />
			<line x1="8" y1="11" x2="8.01" y2="11" />
		</svg><span
			><span class="font-medium">{m.instance_check_failed()}:</span>
			{instance.last_error}</span
		>
	</p>
{:else if instance.last_check_at}
	<p class="text-xs text-subtle">
		{m.instance_last_check()}: {formatWhen(
			instance.last_check_at,
			getLocale()
		)}{instance.last_seen_version ? ` · ${instance.last_seen_version}` : ''}
	</p>
{:else}
	<p class="text-xs text-subtle">
		{m.instance_never_tested()}
	</p>
{/if}
