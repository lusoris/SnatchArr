<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import type { components } from '#lib/api/schema.js';
	import Field from '#lib/components/Field.svelte';
	import { m } from '#lib/paraglide/messages.js';

	type SnatchPolicy = components['schemas']['SnatchPolicy'];

	interface Props {
		policy: SnatchPolicy;
	}

	let { policy = $bindable() }: Props = $props();
</script>

<fieldset class="card flex flex-col gap-4">
	<legend class="px-1 text-sm font-semibold">{m.policy_group_limits()}</legend>
	<p class="text-xs text-subtle">{m.policy_group_limits_desc()}</p>

	<div class="grid gap-4 sm:grid-cols-2">
		<Field id="hourly-cap" label={m.policy_hourly_cap()} hint={m.policy_hourly_cap_hint()}>
			<input
				id="hourly-cap"
				data-field="hourly_cap"
				class="input"
				type="number"
				min="1"
				max="500"
				aria-describedby="hourly-cap-hint"
				bind:value={policy.hourly_cap}
			/>
		</Field>

		<Field
			id="max-queue-size"
			label={m.policy_max_queue_size()}
			hint={m.policy_max_queue_size_hint()}
		>
			<input
				id="max-queue-size"
				data-field="max_queue_size"
				class="input"
				type="number"
				min="-1"
				max="100000"
				aria-describedby="max-queue-size-hint"
				bind:value={policy.max_queue_size}
			/>
		</Field>
	</div>

	<div class="grid gap-4 sm:grid-cols-2">
		<Field id="page-size" label={m.policy_page_size()} hint={m.policy_page_size_hint()}>
			<input
				id="page-size"
				data-field="page_size"
				class="input"
				type="number"
				min="10"
				max="1000"
				aria-describedby="page-size-hint"
				bind:value={policy.page_size}
			/>
		</Field>

		<div class="flex flex-col justify-center gap-0.5 pt-2">
			<label class="flex items-center gap-2 text-sm font-medium">
				<input
					id="await-command"
					data-field="await_command"
					type="checkbox"
					aria-describedby="await-command-hint"
					bind:checked={policy.await_command}
				/>
				{m.policy_await_command()}
			</label>
			<p id="await-command-hint" class="text-xs text-subtle">
				{m.policy_await_command_hint()}
			</p>
		</div>
	</div>
</fieldset>
