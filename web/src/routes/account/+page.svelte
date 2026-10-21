<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import PageHeader from '#lib/components/PageHeader.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import ApiKeys from './ApiKeys.svelte';
	import ChangePassword from './ChangePassword.svelte';
</script>

<PageHeader title={m.account_title()} documentTitle />

<div class="flex flex-col gap-8">
	<section aria-labelledby="change-password-heading">
		<h2 id="change-password-heading" class="mb-4 text-xl font-semibold">
			{m.change_password_title()}
		</h2>
		<ChangePassword />
	</section>

	<section aria-labelledby="api-keys-heading">
		<h2 id="api-keys-heading" class="mb-4 text-xl font-semibold">
			{m.api_keys_title()}
		</h2>
		<ApiKeys />
	</section>
</div>
