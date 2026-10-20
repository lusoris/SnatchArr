<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import type { components } from '#lib/api/schema.js';
	import { m } from '#lib/paraglide/messages.js';

	type SnatchPolicy = components['schemas']['SnatchPolicy'];

	interface Props {
		policy: SnatchPolicy;
	}

	let { policy = $bindable() }: Props = $props();
</script>

<fieldset class="card flex flex-col gap-3">
	<legend class="px-1 text-sm font-semibold">{m.policy_group_safety()}</legend>
	<p class="text-xs text-subtle">{m.policy_group_safety_desc()}</p>

	<div class="flex flex-col gap-0.5 pt-1">
		<label class="flex items-center gap-2 text-sm font-medium">
			<input
				id="dry-run"
				data-field="dry_run"
				type="checkbox"
				aria-describedby="dry-run-hint"
				bind:checked={policy.dry_run}
			/>
			{m.policy_dry_run()}
		</label>
		<p id="dry-run-hint" class="text-xs text-subtle">
			{m.policy_dry_run_hint()}
		</p>
	</div>
</fieldset>
