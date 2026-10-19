<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { page } from '$app/state';
	import InstanceActivityTab from '#lib/components/instance/InstanceActivityTab.svelte';
</script>

<InstanceActivityTab id={page.params.id ?? ''} />
