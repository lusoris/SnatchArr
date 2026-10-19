<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { page } from '$app/state';
	import InstancePolicyTab from '#lib/components/instance/InstancePolicyTab.svelte';
</script>

<InstancePolicyTab id={page.params.id ?? ''} />
