<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import QueryGuard from '#lib/components/QueryGuard.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { licencesQuery } from '#lib/queries/licences.js';
	import { systemStatusQuery } from '#lib/queries/system.js';
	import { formatWhen } from '#lib/stamina.js';
	import LicenceList from './LicenceList.svelte';

	const systemStatus = systemStatusQuery();
	const licences = licencesQuery();
</script>

<section class="w-full" aria-labelledby="about-tab-title">
	<h2 id="about-tab-title" class="sr-only">{m.system_tab_about()}</h2>
	<div class="grid w-full grid-cols-1 items-start gap-6 xl:grid-cols-12">
		<div class="flex flex-col gap-6 xl:col-span-5">
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

			<section class="card flex flex-col gap-4" aria-labelledby="about-brand-heading">
				<h2 id="about-brand-heading" class="text-lg font-semibold">
					{m.about_brand_heading()}
				</h2>
				<ul class="flex flex-col gap-2 text-sm text-fg">
					<li>
						{m.about_brand_vulva_text()}<a
							href="https://commons.wikimedia.org/wiki/File:Female_vulva.svg"
							target="_blank"
							rel="noopener noreferrer"
							aria-label={m.about_brand_vulva_aria()}
							class="text-brand underline hover:opacity-80">{m.about_brand_vulva_file()}</a
						>{m.about_brand_vulva_author()}
					</li>
					<li>
						{m.about_brand_hand_text()}<a
							href="https://commons.wikimedia.org/wiki/File:Hand_reaching_down_Pinhead_icon.svg"
							target="_blank"
							rel="noopener noreferrer"
							aria-label={m.about_brand_hand_aria()}
							class="text-brand underline hover:opacity-80">{m.about_brand_hand_file()}</a
						>{m.about_brand_hand_author()}
					</li>
				</ul>
			</section>
		</div>

		<div class="flex flex-col gap-6 xl:col-span-7">
			<section class="card flex flex-col gap-4" aria-labelledby="third-party-heading">
				<h2 id="third-party-heading" class="text-lg font-semibold">
					{m.about_third_party_title()}
				</h2>

				<QueryGuard query={licences}>
					{#snippet children(data)}
						{#if data.packages.length === 0}
							<p class="text-sm text-subtle">
								{m.about_third_party_empty()}
							</p>
						{:else}
							<LicenceList packages={data.packages} />
						{/if}
					{/snippet}
				</QueryGuard>
			</section>
		</div>
	</div>
</section>
