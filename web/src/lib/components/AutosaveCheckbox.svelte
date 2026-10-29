<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { describeError } from '#lib/api/client.js';
	import { m } from '#lib/paraglide/messages.js';

	// A checkbox that saves on change, with no separate save button. The status next to it says
	// whether the last change was saved; a failed save puts the box back and says why.

	interface Props {
		id: string;
		label: string;
		checked: boolean;
		disabled?: boolean;
		/**
		 * Persists the new value and resolves with the value the server stored (undefined: the
		 * value sent); a rejection marks the change as failed.
		 */
		save: (checked: boolean) => Promise<boolean | undefined>;
	}

	const { id, label, checked, disabled = false, save }: Props = $props();

	type Status = 'idle' | 'saving' | 'saved' | 'failed';

	// The value the user chose, shown until `checked` catches up after a save. Any newer
	// `checked` (a refetch, another tab) is authoritative and ends the override too.
	let chosen: boolean | null = $state(null);
	let checkedAtSave: boolean | null = null;
	let status: Status = $state('idle');
	let failure = $state('');
	const value = $derived(chosen ?? checked);

	$effect(() => {
		if (chosen === null || status === 'saving') return;
		if (checked === chosen || checked !== checkedAtSave) chosen = null;
	});

	async function onchange(event: Event & { currentTarget: HTMLInputElement }): Promise<void> {
		const next = event.currentTarget.checked;
		chosen = next;
		status = 'saving';
		try {
			const stored = await save(next);
			checkedAtSave = checked;
			chosen = stored ?? next;
			status = 'saved';
		} catch (err) {
			chosen = null;
			failure = describeError(err);
			status = 'failed';
		}
	}
</script>

<div class="flex flex-wrap items-center gap-2 text-sm">
	<label class="flex items-center gap-2" for={id}>
		<input
			{id}
			type="checkbox"
			checked={value}
			disabled={disabled || status === 'saving'}
			aria-describedby={`${id}-status`}
			{onchange}
		/>
		<span>{label}</span>
	</label>
	<span
		id={`${id}-status`}
		role="status"
		class={status === 'failed' ? 'text-xs text-danger' : 'text-xs text-subtle'}
		data-testid="autosave-status"
		data-status={status}
	>
		{#if status === 'saving'}
			{m.autosave_saving()}
		{:else if status === 'saved'}
			{m.autosave_saved()}
		{:else if status === 'failed'}
			{m.autosave_failed({ detail: failure })}
		{/if}
	</span>
</div>
