<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import type { components } from '#lib/api/schema.js';
	import StatusIcon from '#lib/components/StatusIcon.svelte';
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
		<StatusIcon status="danger" label={m.instance_check_failed()} size={16} />
		<span
			><span class="font-medium">{m.instance_check_failed()}:</span>
			{instance.last_error}</span
		>
	</p>
{:else if instance.last_check_at}
	<p class="flex items-center gap-1.5 text-xs text-subtle">
		<StatusIcon status="ok" label={m.dlclients_reachable()} size={16} />
		<span>
			{m.instance_last_check()}: {formatWhen(
				instance.last_check_at,
				getLocale()
			)}{instance.last_seen_version ? ` · ${instance.last_seen_version}` : ''}
		</span>
	</p>
{:else}
	<p class="flex items-center gap-1.5 text-xs text-subtle">
		<StatusIcon status="idle" label={m.instance_never_tested()} showLabel size={16} />
	</p>
{/if}
