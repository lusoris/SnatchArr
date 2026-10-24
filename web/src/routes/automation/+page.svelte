<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import type { ResolvedPathname } from '$app/types';
	import { page } from '$app/state';
	import { createSentioQuery } from '@sveltesentio/query';
	import { api } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import ScheduleForm from '#lib/components/ScheduleForm.svelte';
	import ScheduleTable from '#lib/components/ScheduleTable.svelte';
	import Tabs, { type Tab } from '#lib/components/Tabs.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';

	type Schedule = components['schemas']['Schedule'];

	const tabs: Tab[] = [{ id: 'schedules', label: m.automation_tab_schedules() }];

	const schedules = createSentioQuery({
		queryKey: keys.schedules,
		queryFn: async () => (await api.GET('/schedules')).data ?? []
	});

	const instances = createSentioQuery({
		queryKey: keys.instances,
		queryFn: async () => (await api.GET('/instances')).data ?? []
	});

	let editingSchedule = $state<Schedule | null>(null);

	const tabParam = $derived(page.url.searchParams.get('tab'));
	const activeTab = $derived(tabParam === 'schedules' ? 'schedules' : 'schedules');

	function handleTabChange(newTab: string): void {
		const newUrl = new URL(page.url.href);
		newUrl.searchParams.set('tab', newTab);
		const target = `${resolve('/automation')}${newUrl.search}` as ResolvedPathname;
		void goto(target, { replaceState: true });
	}
</script>

<PageHeader title={m.nav_automation()} documentTitle />

<Tabs {tabs} active={activeTab} onchange={handleTabChange} label={m.nav_automation()}>
	{#snippet children(current)}
		{#if current === 'schedules'}
			{#if schedules.isPending}
				<p aria-live="polite">{m.loading()}</p>
			{:else if schedules.isError}
				<ProblemNote error={schedules.error} />
			{:else}
				{#if (schedules.data ?? []).length === 0}
					<p class="card mb-6">{m.schedules_empty()}</p>
				{:else}
					<div class="mb-6">
						<ScheduleTable
							schedules={schedules.data ?? []}
							instances={instances.data ?? []}
							onedit={(s) => (editingSchedule = s)}
						/>
					</div>
				{/if}

				{#if session.isAdmin}
					<ScheduleForm
						instances={instances.data ?? []}
						editing={editingSchedule}
						oncancel={() => (editingSchedule = null)}
						onsuccess={() => (editingSchedule = null)}
					/>
				{/if}
			{/if}
		{/if}
	{/snippet}
</Tabs>
