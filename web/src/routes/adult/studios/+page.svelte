<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { api } from '#lib/api/client.js';
	import AdultCatalogList from '#lib/components/adult/AdultCatalogList.svelte';
	import { HELP_MAP } from '#lib/help.js';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';
</script>

<AdultCatalogList
	title={m.adult_studios_title()}
	description={m.adult_studios_subtitle()}
	favoriteOnly={m.adult_studios_favorite_only()}
	none={m.adult_studios_none()}
	favoriteBadge={m.adult_studios_favorite_badge()}
	refCount={(count) => m.adult_studios_ref_count({ count })}
	help={HELP_MAP['/adult/studios']}
	filterInputId="studios-favorite-filter"
	queryKey={keys.adultStudios}
	fetch={async (favorite) => {
		const res = await api.GET('/adult/studios', {
			params: {
				query: {
					favorite,
					limit: 200
				}
			}
		});
		return res.data?.items ?? [];
	}}
/>
