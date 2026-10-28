<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { createSentioQuery } from '@sveltesentio/query';
	import { api } from '#lib/api/client.js';
	import { keys } from '#lib/query.js';

	interface Props {
		id: string;
	}
	const { id }: Props = $props();
	// Shares the cached instance list; the id stays the label until it loads or when it is unknown.
	const instances = createSentioQuery({
		queryKey: keys.instances,
		queryFn: async () => (await api.GET('/instances')).data ?? []
	});
	const name = $derived(instances.data?.find((i) => i.id === id)?.name ?? id);
</script>

<a class="break-all hover:underline" href={resolve('/instances/[id]', { id })}>{name}</a>
