<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts" generics="TType extends string = EntityType">
	import type { ResolvedPathname } from '$app/types';
	import { createSentioQuery } from '@sveltesentio/query';
	import { api } from '#lib/api/client.js';
	import { entities, type EntityType, type InstanceTab } from '#lib/entities.js';
	import { keys } from '#lib/query.js';

	interface EntityHrefGraph<K extends string> {
		entityHref(type: K, id: string, tab?: never, search?: string): ResolvedPathname;
	}

	interface Props {
		type: TType;
		id: string;
		label?: string;
		tab?: TType extends 'instance' ? InstanceTab : string;
		class?: string;
		graph?: EntityHrefGraph<TType>;
	}

	const {
		type,
		id,
		label,
		tab,
		class: className = 'break-all hover:underline',
		graph
	}: Props = $props();

	function loadInstances() {
		if (type !== 'instance' || label !== undefined) return null;
		try {
			return createSentioQuery({
				queryKey: keys.instances,
				queryFn: async () => (await api.GET('/instances')).data ?? []
			});
		} catch {
			return null;
		}
	}

	const instances = loadInstances();
	const activeGraph = $derived((graph ?? entities) as EntityHrefGraph<TType>);
	const href = $derived(activeGraph.entityHref(type, id, tab as never));
	const displayLabel = $derived(
		label ?? (type === 'instance' ? (instances?.data?.find((i) => i.id === id)?.name ?? id) : id)
	);
</script>

{#if href}
	<a class={className} {href}>{displayLabel}</a>
{/if}
