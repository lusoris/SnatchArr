<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import PageHeader from '#lib/components/PageHeader.svelte';
	import QueryGuard from '#lib/components/QueryGuard.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { systemStatusQuery } from '#lib/queries/system.js';
	import { formatWhen } from '#lib/stamina.js';

	const systemStatus = systemStatusQuery();
</script>

<PageHeader title={m.about_title()} documentTitle />

<div class="flex max-w-2xl flex-col gap-6">
	<section class="card flex flex-col gap-4" aria-labelledby="about-app-heading">
		<h2 id="about-app-heading" class="text-lg font-semibold">
			{m.about_app_heading()}
		</h2>

		<QueryGuard query={systemStatus}>
			{#snippet children(st)}
				<dl class="grid grid-cols-1 gap-y-3 text-sm sm:grid-cols-2 sm:gap-x-4">
					<div>
						<dt class="text-xs font-medium tracking-wider text-subtle uppercase">
							{m.about_field_app_name()}
						</dt>
						<dd class="mt-0.5 font-medium text-fg">
							{st.name || m.app_name()}
						</dd>
					</div>

					<div>
						<dt class="text-xs font-medium tracking-wider text-subtle uppercase">
							{m.about_field_version()}
						</dt>
						<dd class="mt-0.5 font-mono text-fg">
							{st.version}
						</dd>
					</div>

					<div class="sm:col-span-2">
						<dt class="text-xs font-medium tracking-wider text-subtle uppercase">
							{m.about_field_commit()}
						</dt>
						<dd class="mt-0.5 font-mono break-all text-fg">
							{st.commit || '—'}
						</dd>
					</div>

					<div>
						<dt class="text-xs font-medium tracking-wider text-subtle uppercase">
							{m.about_field_build_date()}
						</dt>
						<dd class="mt-0.5 text-fg">
							{formatWhen(st.build_date, getLocale()) || '—'}
						</dd>
					</div>

					<div>
						<dt class="text-xs font-medium tracking-wider text-subtle uppercase">
							{m.about_field_license()}
						</dt>
						<dd class="mt-0.5 text-fg">
							<a
								href="https://interoperable-europe.ec.europa.eu/collection/eupl/eupl-text-eupl-12"
								target="_blank"
								rel="noopener noreferrer"
								aria-label={m.about_license_aria()}
								class="text-brand underline hover:opacity-80"
							>
								{m.about_license_name()}
							</a>
						</dd>
					</div>
				</dl>
			{/snippet}
		</QueryGuard>
	</section>

	<section class="card flex flex-col gap-2" aria-labelledby="third-party-heading">
		<h2 id="third-party-heading" class="text-lg font-semibold">
			{m.about_third_party_title()}
		</h2>
		<p class="text-sm text-subtle">
			{m.about_third_party_placeholder()}
		</p>
	</section>
</div>
