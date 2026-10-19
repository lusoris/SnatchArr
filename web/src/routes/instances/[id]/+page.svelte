<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { page } from '$app/state';
	import InstanceOverview from '#lib/components/instance/InstanceOverview.svelte';
</script>

<InstanceOverview id={page.params.id ?? ''} />
