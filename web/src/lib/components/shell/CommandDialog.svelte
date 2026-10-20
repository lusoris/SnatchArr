<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { createSentioQuery } from '@sveltesentio/query';
	import { CommandRegistry, type Command } from '@sveltesentio/ui/cmd';
	import CommandPalette from '@sveltesentio/ui/cmd/palette';
	import { api } from '#lib/api/client.js';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';
	import type { NavGroup } from './nav.js';

	interface Props {
		navGroups: NavGroup[];
		open?: boolean;
	}

	let { navGroups, open = $bindable(false) }: Props = $props();

	let openerEl: HTMLElement | null = null;
	let prevOpen = false;

	const instances = createSentioQuery({
		queryKey: keys.instances,
		queryFn: async () => (await api.GET('/instances')).data ?? []
	});

	const registry = $derived.by(() => {
		const cmds: Command[] = [];

		for (const group of navGroups) {
			for (const item of group.items) {
				cmds.push({
					id: `nav-${item.href}`,
					title: item.label,
					subtitle: group.label,
					group: group.label,
					run: async () => {
						await goto(resolve(item.href));
					}
				});
			}
		}

		cmds.push({
			id: 'nav-/account',
			title: m.nav_account(),
			subtitle: m.nav_account(),
			group: m.nav_account(),
			run: async () => {
				await goto(resolve('/account'));
			}
		});

		for (const inst of instances.data ?? []) {
			cmds.push({
				id: `instance-${inst.id}`,
				title: inst.name,
				subtitle: inst.kind,
				group: m.nav_instances(),
				keywords: [inst.kind, inst.name],
				run: async () => {
					await goto(resolve('/instances/[id]', { id: inst.id }));
				}
			});
		}

		return new CommandRegistry().register(...cmds);
	});

	$effect.pre(() => {
		if (open && !prevOpen) {
			openerEl = document.activeElement as HTMLElement | null;
		}
		prevOpen = open;
	});

	$effect(() => {
		if (open) {
			const input = document.getElementById('ssentio-cmd-input');
			input?.focus();
		} else if (openerEl) {
			const target = openerEl;
			openerEl = null;
			target.focus();
		}
	});
</script>

<CommandPalette {registry} bind:open placeholder={m.nav_search_placeholder()} />
