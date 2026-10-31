<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts" generics="TType extends string = EntityType">
	import type { ResolvedPathname } from '$app/types';
	import { entities, type EntityType, type RelatedEntityLink } from '#lib/entities.js';
	import EntityLink from './EntityLink.svelte';

	interface RelatedGraph<K extends string> {
		related(type: K, row: object | null | undefined): RelatedEntityLink<string>[];
		entityHref(type: never, id: string, tab?: never, search?: string): ResolvedPathname;
	}

	interface Props {
		type: TType;
		row: object | null | undefined;
		class?: string;
		graph?: RelatedGraph<TType>;
	}

	const { type, row, class: className, graph }: Props = $props();
	const activeGraph = $derived((graph ?? entities) as RelatedGraph<TType>);
	const links = $derived(activeGraph.related(type, row));
</script>

{#each links as link (`${link.type}:${link.id}`)}
	<EntityLink type={link.type} id={link.id} class={className} graph={activeGraph as never} />
{/each}
