<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import Tabs, { type Tab } from '#lib/components/Tabs.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import type { Ecosystem, LicencePackage } from '#lib/queries/licences.js';

	interface Props {
		packages: LicencePackage[];
	}

	let { packages }: Props = $props();

	interface EcoDef {
		key: Ecosystem;
		label: () => string;
	}

	const ecosystems: EcoDef[] = [
		{ key: 'go', label: () => m.about_third_party_group_go() },
		{ key: 'rust', label: () => m.about_third_party_group_rust() },
		{ key: 'npm', label: () => m.about_third_party_group_npm() }
	];

	function getPackagesForEcosystem(eco: Ecosystem): LicencePackage[] {
		return packages.filter((p) => p.ecosystem === eco);
	}

	function findFirstNonEmptyEcosystem(): Ecosystem {
		return ecosystems.find((eco) => getPackagesForEcosystem(eco.key).length > 0)?.key ?? 'go';
	}

	// The tab the reader picked; a picked tab whose ecosystem has no packages falls back to the
	// first ecosystem that has some.
	let chosen = $state<Ecosystem | undefined>();
	const active = $derived(
		chosen !== undefined && getPackagesForEcosystem(chosen).length > 0
			? chosen
			: findFirstNonEmptyEcosystem()
	);
	let filterQuery = $state('');

	const tabs = $derived<Tab<Ecosystem>[]>(
		ecosystems.map((eco) => {
			const count = getPackagesForEcosystem(eco.key).length;
			return {
				id: eco.key,
				label: m.about_third_party_tab({ name: eco.label(), count }),
				hidden: count === 0
			};
		})
	);

	const activeEco = $derived(ecosystems.find((e) => e.key === active));
	const activeEcoLabel = $derived(activeEco ? activeEco.label() : '');

	const activePackages = $derived(getPackagesForEcosystem(active));

	const filteredPackages = $derived.by(() => {
		const q = filterQuery.trim().toLowerCase();
		if (!q) return activePackages;
		return activePackages.filter(
			(pkg) => pkg.name.toLowerCase().includes(q) || pkg.licence.toLowerCase().includes(q)
		);
	});
</script>

<div class="flex flex-col gap-4">
	<Tabs {tabs} {active} onchange={(id) => (chosen = id)} label={m.about_third_party_title()}>
		<div class="flex flex-col gap-3">
			<div class="flex flex-col gap-1.5">
				<label
					for="third-party-filter"
					class="text-xs font-medium tracking-wider text-subtle uppercase"
				>
					{m.about_third_party_filter_label()}
				</label>
				<input
					id="third-party-filter"
					type="search"
					bind:value={filterQuery}
					placeholder={m.about_third_party_filter_placeholder()}
					class="input w-full"
				/>
			</div>

			{#if filteredPackages.length === 0}
				<p class="text-sm text-subtle">
					{m.about_third_party_no_match()}
				</p>
			{:else}
				<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
				<div
					role="region"
					tabindex="0"
					aria-label={m.about_third_party_list_label({ name: activeEcoLabel })}
					class="max-h-[min(32rem,55vh)] overflow-y-auto"
				>
					<ul class="flex flex-col gap-3 divide-y divide-line">
						{#each filteredPackages as pkg (`${pkg.name}@${pkg.version}`)}
							<li class="flex flex-col gap-1 pt-2 first:pt-0">
								<div class="flex flex-wrap items-baseline justify-between gap-x-2 gap-y-1">
									<div class="flex min-w-0 flex-wrap items-baseline gap-2">
										<span class="min-w-0 font-medium break-all text-fg">{pkg.name}</span>
										<span class="font-mono text-xs text-subtle">{pkg.version}</span>
									</div>
									<div class="flex min-w-0 items-center gap-2">
										<span
											class="min-w-0 rounded bg-surface px-1.5 py-0.5 font-mono text-xs font-medium break-words text-fg"
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
											class="mt-2 max-h-60 overflow-auto rounded bg-surface p-2 font-mono text-[0.75rem] break-all whitespace-pre-wrap text-subtle">{pkg.text}</pre>
									</details>
								{/if}
							</li>
						{/each}
					</ul>
				</div>
			{/if}
		</div>
	</Tabs>
</div>
