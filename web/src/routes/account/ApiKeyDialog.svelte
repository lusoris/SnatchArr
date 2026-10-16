<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { m } from '#lib/paraglide/messages.js';

	interface Props {
		secret: string;
		name: string;
		onclose: () => void;
	}

	const { secret, name, onclose }: Props = $props();

	let copyStatus = $state<'idle' | 'copied' | 'failed'>('idle');
	let dialog = $state<HTMLDialogElement>();

	// showModal traps focus inside the dialog and closes it on Escape; every way of
	// closing ends in the close event, which drops the secret through onclose.
	$effect(() => {
		dialog?.showModal();
	});

	async function copyToClipboard(): Promise<void> {
		try {
			if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
				await navigator.clipboard.writeText(secret);
				copyStatus = 'copied';
				toast.success(m.api_key_copied());
			} else {
				copyStatus = 'failed';
			}
		} catch {
			copyStatus = 'failed';
		}
	}
</script>

<dialog
	bind:this={dialog}
	aria-labelledby="api-key-dialog-title"
	class="card m-auto w-full max-w-lg shadow-xl backdrop:bg-black/60"
	{onclose}
>
	<div class="flex flex-col gap-4">
		<h3 id="api-key-dialog-title" class="text-lg font-semibold">
			{m.api_key_secret_title()}
		</h3>
		<p class="text-sm font-medium">{name}</p>
		<p role="alert" class="rounded-md border border-line bg-surface p-3 text-sm text-subtle">
			{m.api_key_secret_warning()}
		</p>
		<div class="flex items-center gap-2">
			<input
				id="created-secret-input"
				readonly
				value={secret}
				aria-label={m.api_key_secret_title()}
				class="input font-mono text-sm"
			/>
			<button type="button" class="btn shrink-0" onclick={copyToClipboard}>
				{m.api_key_copy()}
			</button>
		</div>
		{#if copyStatus === 'copied'}
			<p role="status" class="text-xs text-subtle">{m.api_key_copied()}</p>
		{:else if copyStatus === 'failed'}
			<p role="alert" class="text-xs text-danger">{m.api_key_copy_fallback()}</p>
		{/if}
		<div class="flex justify-end">
			<button type="button" class="btn btn-primary" onclick={() => dialog?.close()}>
				{m.api_key_close_dialog()}
			</button>
		</div>
	</div>
</dialog>
