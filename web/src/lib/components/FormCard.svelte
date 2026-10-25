<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { m } from '#lib/paraglide/messages.js';
	import ProblemNote from './ProblemNote.svelte';

	interface Props {
		/** Id of the heading; the section is labelled by it. */
		id: string;
		title: string;
		/** Editing a stored record: offers a way back to the create form. */
		editing: boolean;
		pending: boolean;
		/** The last save error; field-level reasons are shown by the fields themselves. */
		error: unknown;
		onsubmit: () => void;
		/** Back to the create form; only offered while editing. */
		oncancel?: () => void;
		/** Probes the values as typed, before they are stored; the form must be valid first. */
		ontest?: () => void;
		testing?: boolean;
		children: Snippet;
		/** Label of the submit button; defaults to Save. */
		submitLabel?: string;
	}

	const {
		id,
		title,
		editing,
		pending,
		error,
		onsubmit,
		oncancel,
		ontest,
		testing = false,
		children,
		submitLabel
	}: Props = $props();

	let form = $state<HTMLFormElement>();

	function test(): void {
		if (form?.reportValidity()) ontest?.();
	}
</script>

<section class="card w-full" aria-labelledby={id}>
	<h2 {id} class="mb-3 text-lg font-semibold">{title}</h2>
	<form
		bind:this={form}
		class="flex flex-col gap-3"
		onsubmit={(e) => {
			e.preventDefault();
			onsubmit();
		}}
	>
		<ProblemNote {error} />
		{@render children()}
		<div class="flex flex-wrap gap-2">
			{#if ontest}
				<button type="button" class="btn" disabled={testing} onclick={test}
					>{m.action_test()}</button
				>
			{/if}
			<button type="submit" class="btn btn-primary" disabled={pending}
				>{submitLabel ?? m.action_save()}</button
			>
			{#if editing && oncancel}
				<button type="button" class="btn" onclick={oncancel}>{m.action_cancel()}</button>
			{/if}
		</div>
	</form>
</section>
