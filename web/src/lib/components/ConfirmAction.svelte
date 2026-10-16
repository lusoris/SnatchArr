<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';

	interface Props {
		/** Text of the button that asks for the action. */
		label: string;
		/** What the action does, shown before it runs. */
		message: string;
		onconfirm: () => void;
		pending?: boolean;
		/** Accessible name of the question; the trigger's label when not given. */
		groupLabel?: string;
		confirmLabel?: string;
		backLabel?: string;
		/** Lays the question out on one line, for a table row or a card's action bar. */
		inline?: boolean;
	}

	const {
		label,
		message,
		onconfirm,
		pending = false,
		groupLabel,
		confirmLabel,
		backLabel,
		inline = false
	}: Props = $props();
	let asking = $state(false);
	let trigger = $state<HTMLButtonElement>();
	let back = $state<HTMLButtonElement>();
	let opened = false;

	// Keep keyboard focus with the step: the safe choice when it opens, the trigger when it
	// closes, so the removed button does not drop focus to the page.
	$effect(() => {
		if (asking) back?.focus();
		else if (opened) trigger?.focus();
		opened = asking || opened;
	});
</script>

<!-- An in-page confirmation step: the action runs only after a second, explicit press. -->
{#if asking}
	<div
		role="group"
		aria-label={groupLabel ?? label}
		class={inline ? 'flex flex-wrap items-center gap-2' : 'flex flex-col gap-2'}
	>
		<p class={inline ? 'text-sm font-medium' : 'text-sm'}>{message}</p>
		<div class="flex gap-2">
			<button
				type="button"
				class="btn btn-primary"
				disabled={pending}
				onclick={() => {
					asking = false;
					onconfirm();
				}}>{confirmLabel ?? m.confirm_yes()}</button
			>
			<button type="button" class="btn" bind:this={back} onclick={() => (asking = false)}
				>{backLabel ?? m.confirm_back()}</button
			>
		</div>
	</div>
{:else}
	<button
		type="button"
		class="btn"
		bind:this={trigger}
		disabled={pending}
		onclick={() => (asking = true)}>{label}</button
	>
{/if}
