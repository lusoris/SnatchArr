<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import ConfirmDelete from './ConfirmDelete.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { session } from '#lib/session.svelte.js';
	import { formatWhen } from '#lib/stamina.js';

	export interface LinkItem {
		name: string;
		base_url: string;
		last_seen_version?: string | null;
		last_check_at?: string | null;
		last_error?: string | null;
	}

	interface Props {
		link: LinkItem;
		onedit: () => void;
		ontest: () => void;
		ondelete: () => void;
		testPending?: boolean;
		deletePending?: boolean;
		statusExtra?: Snippet;
		extraActions?: Snippet;
	}

	const {
		link,
		onedit,
		ontest,
		ondelete,
		testPending = false,
		deletePending = false,
		statusExtra,
		extraActions
	}: Props = $props();
</script>

<li class="card">
	<h3 class="font-semibold">
		{link.name} <span class="text-sm text-subtle">{link.last_seen_version ?? ''}</span>
	</h3>
	<p class="text-sm break-all text-subtle">{link.base_url}</p>
	<p class="text-sm">
		{link.last_check_at
			? m.link_last_check({ when: formatWhen(link.last_check_at, getLocale()) })
			: m.instance_never_tested()}{#if statusExtra}{@render statusExtra()}{/if}
	</p>
	{#if link.last_error}<p class="text-sm text-danger">{link.last_error}</p>{/if}
	{#if session.isAdmin}
		<div class="mt-3 flex flex-wrap gap-2">
			<button type="button" class="btn" onclick={onedit}>{m.action_edit()}</button>
			<button type="button" class="btn" disabled={testPending} onclick={ontest}
				>{m.action_test()}</button
			>
			{@render extraActions?.()}
			<ConfirmDelete name={link.name} disabled={deletePending} onconfirm={ondelete} />
		</div>
	{/if}
</li>
