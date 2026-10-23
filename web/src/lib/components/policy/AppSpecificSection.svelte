<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import Field from '#lib/components/Field.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { isAppSpecificKind, type PolicySectionProps } from './section.js';

	let { policy = $bindable(), kind }: PolicySectionProps = $props();

	const isSupportedKind = $derived(isAppSpecificKind(kind));
</script>

{#if isSupportedKind}
	<fieldset class="card flex flex-col gap-4">
		<legend class="px-1 text-sm font-semibold">{m.policy_group_app_specific()}</legend>
		<p class="text-xs text-subtle">{m.policy_group_app_specific_desc()}</p>

		{#if kind === 'radarr'}
			<Field
				id="radarr-release-type"
				label={m.policy_radarr_release_type()}
				hint={m.policy_radarr_release_type_hint()}
			>
				<select
					id="radarr-release-type"
					data-field="radarr_release_type"
					class="select"
					aria-describedby="radarr-release-type-hint"
					bind:value={policy.radarr_release_type}
				>
					<option value="physical">{m.policy_radarr_release_physical()}</option>
					<option value="digital">{m.policy_radarr_release_digital()}</option>
					<option value="cinema">{m.policy_radarr_release_cinema()}</option>
				</select>
			</Field>
		{:else if kind === 'sonarr'}
			<div class="grid gap-4 sm:grid-cols-2">
				<Field
					id="sonarr-missing-mode"
					label={m.policy_sonarr_missing_mode()}
					hint={m.policy_sonarr_missing_mode_hint()}
				>
					<select
						id="sonarr-missing-mode"
						data-field="sonarr_missing_mode"
						class="select"
						aria-describedby="sonarr-missing-mode-hint"
						bind:value={policy.sonarr_missing_mode}
					>
						<option value="episodes">{m.policy_sonarr_mode_episodes()}</option>
						<option value="season_packs">{m.policy_sonarr_mode_season_packs()}</option>
						<option value="shows">{m.policy_sonarr_mode_shows()}</option>
					</select>
				</Field>

				<Field
					id="sonarr-upgrade-mode"
					label={m.policy_sonarr_upgrade_mode()}
					hint={m.policy_sonarr_upgrade_mode_hint()}
				>
					<select
						id="sonarr-upgrade-mode"
						data-field="sonarr_upgrade_mode"
						class="select"
						aria-describedby="sonarr-upgrade-mode-hint"
						bind:value={policy.sonarr_upgrade_mode}
					>
						<option value="episodes">{m.policy_sonarr_mode_episodes()}</option>
						<option value="season_packs">{m.policy_sonarr_mode_season_packs()}</option>
					</select>
				</Field>
			</div>
		{:else if kind === 'lidarr'}
			<Field
				id="lidarr-missing-mode"
				label={m.policy_lidarr_missing_mode()}
				hint={m.policy_lidarr_missing_mode_hint()}
			>
				<select
					id="lidarr-missing-mode"
					data-field="lidarr_missing_mode"
					class="select"
					aria-describedby="lidarr-missing-mode-hint"
					bind:value={policy.lidarr_missing_mode}
				>
					<option value="artist">{m.policy_lidarr_mode_artist()}</option>
					<option value="album">{m.policy_lidarr_mode_album()}</option>
				</select>
			</Field>
		{/if}
	</fieldset>
{/if}
