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

<div class="flex flex-col gap-8">
	<section aria-labelledby="preferences-heading">
		<h2 id="preferences-heading" class="mb-4 text-xl font-semibold">
			{m.preferences_title()}
		</h2>
		<Preferences />
	</section>

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

	<LinkedSignIns />
</div>
