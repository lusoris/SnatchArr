<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { page } from '$app/state';
	import InstanceMemoryTab from '#lib/components/instance/InstanceMemoryTab.svelte';
</script>

<InstanceMemoryTab id={page.params.id ?? ''} />
