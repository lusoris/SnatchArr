<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { page } from '$app/state';
	import InstanceQueueTab from '#lib/components/instance/InstanceQueueTab.svelte';
</script>

<InstanceQueueTab id={page.params.id ?? ''} />
