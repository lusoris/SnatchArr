<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import PageHeader from '#lib/components/PageHeader.svelte';
	import { HELP_MAP } from '#lib/help.js';
	import { m } from '#lib/paraglide/messages.js';
	import ApiKeys from './ApiKeys.svelte';
	import ChangePassword from './ChangePassword.svelte';
	import LinkedSignIns from './LinkedSignIns.svelte';
	import Preferences from './Preferences.svelte';
</script>

<PageHeader title={m.account_title()} documentTitle help={HELP_MAP['/account']} />

<!-- Two columns from 1280 px (ADR-0029 density); the API key table spans the full width. -->
<div class="grid grid-cols-1 items-start gap-8 xl:grid-cols-12">
	<section aria-labelledby="preferences-heading" class="min-w-0 xl:col-span-7">
		<h2 id="preferences-heading" class="mb-4 text-xl font-semibold">
			{m.preferences_title()}
		</h2>
		<Preferences />
	</section>

	<div class="flex min-w-0 flex-col gap-8 xl:col-span-5">
		<LinkedSignIns />

		<section aria-labelledby="change-password-heading">
			<h2 id="change-password-heading" class="mb-4 text-xl font-semibold">
				{m.change_password_title()}
			</h2>
			<ChangePassword />
		</section>
	</div>

	<section aria-labelledby="api-keys-heading" class="min-w-0 xl:col-span-12">
		<h2 id="api-keys-heading" class="mb-4 text-xl font-semibold">
			{m.api_keys_title()}
		</h2>
		<ApiKeys />
	</section>
</div>
