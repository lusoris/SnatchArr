<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import QueryGuard from '#lib/components/QueryGuard.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { licencesQuery, type Ecosystem, type LicencePackage } from '#lib/queries/licences.js';
	import { systemStatusQuery } from '#lib/queries/system.js';
	import { formatWhen } from '#lib/stamina.js';

	const systemStatus = systemStatusQuery();
	const licences = licencesQuery();

	const ecosystems: { key: Ecosystem; label: () => string }[] = [
		{ key: 'go', label: () => m.about_third_party_group_go() },
		{ key: 'rust', label: () => m.about_third_party_group_rust() },
		{ key: 'npm', label: () => m.about_third_party_group_npm() }
	];

	function getPackagesForEcosystem(packages: LicencePackage[], eco: Ecosystem): LicencePackage[] {
		return packages.filter((p) => p.ecosystem === eco);
	}
</script>

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
					<div class="flex flex-col gap-6">
						{#each ecosystems as eco (eco.key)}
							{@const pkgs = getPackagesForEcosystem(data.packages, eco.key)}
							{#if pkgs.length > 0}
								<div class="flex flex-col gap-3">
									<div class="border-border flex items-baseline justify-between border-b pb-1">
										<h3 class="text-base font-semibold text-fg">
											{eco.label()}
										</h3>
										<span class="text-xs text-subtle">
											{m.about_third_party_count({ count: pkgs.length })}
										</span>
									</div>

									<ul class="divide-border/40 flex flex-col gap-3 divide-y">
										{#each pkgs as pkg (`${pkg.name}@${pkg.version}`)}
											<li class="flex flex-col gap-1 pt-2 first:pt-0">
												<div class="flex flex-wrap items-baseline justify-between gap-x-2 gap-y-1">
													<div class="flex min-w-0 flex-wrap items-baseline gap-2">
														<span class="min-w-0 font-medium break-all text-fg">{pkg.name}</span>
														<span class="font-mono text-xs text-subtle">{pkg.version}</span>
													</div>
													<div class="flex min-w-0 items-center gap-2">
														<span
															class="bg-muted min-w-0 rounded px-1.5 py-0.5 font-mono text-xs font-medium break-words text-fg"
														>
															{pkg.licence}
														</span>
														{#if pkg.url}
															<a
																href={pkg.url}
																target="_blank"
																rel="noopener noreferrer external"
																aria-label={m.about_third_party_visit_aria({ name: pkg.name })}
																class="text-brand text-xs underline hover:opacity-80"
															>
																{m.about_third_party_visit_link()}
															</a>
														{/if}
													</div>
												</div>

												{#if pkg.text}
													<details class="mt-1 text-xs">
														<summary class="cursor-pointer text-subtle hover:text-fg">
															{m.about_third_party_toggle_text()}
														</summary>
														<pre
															class="bg-muted mt-2 max-h-60 overflow-auto rounded p-2 font-mono text-[0.75rem] break-all whitespace-pre-wrap text-subtle">{pkg.text}</pre>
													</details>
												{/if}
											</li>
										{/each}
									</ul>
								</div>
							{/if}
						{/each}
					</div>
				{/if}
			{/snippet}
		</QueryGuard>
	</section>
</div>
