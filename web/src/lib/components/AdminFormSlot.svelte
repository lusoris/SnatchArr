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

	export interface FormEditorLike {
		readonly key: unknown;
		close: () => void;
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
		editor: FormEditorLike;
		save: MutationLike<T>;
		probe?: MutationLike<T>;
		form: Snippet<[formProps: FormSlotProps<T>]>;
	}

	const { editor, save, probe, form }: Props<T> = $props();
</script>

{#if session.isAdmin}
	{#key editor.key}
		{@render form({
			pending: save.isPending,
			error: save.error,
			onsave: (body: T) => save.mutate(body),
			oncancel: editor.close,
			ontest: probe ? (body: T) => probe.mutate(body) : undefined,
			testing: probe?.isPending
		})}
	{/key}
{/if}
