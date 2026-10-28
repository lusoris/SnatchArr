<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { api } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import AdultCatalogList from '#lib/components/adult/AdultCatalogList.svelte';
	import { HELP_MAP } from '#lib/help.js';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';

	type AdultPerformer = components['schemas']['AdultPerformer'];
</script>

{#snippet extraText(performer: AdultPerformer)}
	{#if performer.disambiguation}
		<span class="text-xs text-subtle">({performer.disambiguation})</span>
	{/if}
{/snippet}

<AdultCatalogList
	title={m.adult_performers_title()}
	description={m.adult_performers_subtitle()}
	favoriteOnly={m.adult_performers_favorite_only()}
	none={m.adult_performers_none()}
	favoriteBadge={m.adult_performers_favorite_badge()}
	refCount={(count) => m.adult_performers_ref_count({ count })}
	help={HELP_MAP['/adult/performers']}
	filterInputId="performers-favorite-filter"
	queryKey={keys.adultPerformers}
	fetch={async (favorite) => {
		const res = await api.GET('/adult/performers', {
			params: {
				query: {
					favorite,
					limit: 200
				}
			}
		});
		return res.data?.items ?? [];
	}}
	{extraText}
/>
