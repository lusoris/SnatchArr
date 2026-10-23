<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts" generics="T">
	import type { Snippet } from 'svelte';
	import { session } from '#lib/session.svelte.js';

	export interface MutationLike<T> {
		isPending: boolean;
		error: unknown;
		mutate: (body: T) => void;
	}

	export interface FormSlotProps<T> {
		pending: boolean;
		error: unknown;
		onsave: (body: T) => void;
		oncancel: () => void;
		ontest?: (body: T) => void;
		testing?: boolean;
	}

	interface Props<T> {
		key: unknown;
		save: MutationLike<T>;
		probe?: MutationLike<T>;
		oncancel: () => void;
		form: Snippet<[formProps: FormSlotProps<T>]>;
	}

	const { key, save, probe, oncancel, form }: Props<T> = $props();
</script>

{#if session.isAdmin}
	{#key key}
		{@render form({
			pending: save.isPending,
			error: save.error,
			onsave: (body: T) => save.mutate(body),
			oncancel,
			ontest: probe ? (body: T) => probe.mutate(body) : undefined,
			testing: probe?.isPending
		})}
	{/key}
{/if}
