<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import type { ResolvedPathname } from '$app/types';
	import { page } from '$app/state';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Tabs, { type Tab } from '#lib/components/Tabs.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { session } from '#lib/session.svelte.js';
	import HistoryTab from './HistoryTab.svelte';
	import LiveTab from './LiveTab.svelte';
	import LogTab from './LogTab.svelte';

	type ActivityTabKey = 'history' | 'live' | 'log';

	const tabs = $derived<Tab<ActivityTabKey>[]>(
		session.isAdmin
			? [
					{ id: 'history', label: m.history_title() },
					{ id: 'live', label: m.live_title() },
					{ id: 'log', label: m.logs_title() }
				]
			: [
					{ id: 'history', label: m.history_title() },
					{ id: 'live', label: m.live_title() }
				]
	);

	const rawSource = $derived(page.url.searchParams.get('source'));
	const activeTab = $derived<ActivityTabKey>(
		rawSource === 'live' ? 'live' : rawSource === 'log' && session.isAdmin ? 'log' : 'history'
	);

	function handleTabChange(newTab: ActivityTabKey): void {
		const newUrl = new URL(page.url.href);
		newUrl.searchParams.set('source', newTab);
		const target = `${resolve('/activity')}${newUrl.search}` as ResolvedPathname;
		void goto(target, { replaceState: true });
	}
</script>

<PageHeader title={m.nav_activity()} documentTitle />

<Tabs {tabs} active={activeTab} onchange={handleTabChange} label={m.nav_activity()}>
	{#snippet children(current)}
		{#if current === 'history'}
			<HistoryTab />
		{:else if current === 'live'}
			<LiveTab />
		{:else if current === 'log' && session.isAdmin}
			<LogTab />
		{/if}
	{/snippet}
</Tabs>
