<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import EntityLink from '#lib/components/EntityLink.svelte';

	interface Props {
		id: string;
	}
	const { id }: Props = $props();
</script>

<EntityLink type="instance" {id} />
