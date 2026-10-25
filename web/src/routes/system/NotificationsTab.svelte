<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import NotificationChannels from './NotificationChannels.svelte';
	import NotificationRules from './NotificationRules.svelte';
</script>

<section class="w-full" aria-labelledby="notifications-tab-title">
	<h2 id="notifications-tab-title" class="sr-only">
		{m.notifications_title()}
	</h2>

	<div class="grid w-full grid-cols-1 items-start gap-6 xl:grid-cols-12">
		<div class="flex flex-col gap-6 xl:col-span-7">
			<NotificationChannels />
		</div>

		<div class="flex flex-col gap-6 xl:col-span-5">
			<NotificationRules />
		</div>
	</div>
</section>
