<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { z } from 'zod';
	import { api, describeError } from '#lib/api/client.js';
	import AdminSaveButton from '#lib/components/AdminSaveButton.svelte';
	import BrandMarkSelect from '#lib/components/BrandMarkSelect.svelte';
	import Field from '#lib/components/Field.svelte';
	import NewPasswordField from '#lib/components/NewPasswordField.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import QueryGuard from '#lib/components/QueryGuard.svelte';
	import SfwModeSelect from '#lib/components/SfwModeSelect.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';
	import {
		createSettingsSave,
		generalSettingsSchema,
		mergeGeneralSettings,
		type GeneralSettingsDraft,
		type LlmWriteTool
	} from '#lib/settings.js';

	type WriteToolGroup = {
		field: string;
		legend: () => string;
		options: { value: LlmWriteTool; label: () => string }[];
	};

	const writeToolGroups: WriteToolGroup[] = [
		{
			field: 'llm_write_tools_core',
			legend: () => m.settings_llm_scope_core_write(),
			options: [
				{
					value: 'snatcharr_create_instance',
					label: () => m.settings_llm_tool_create_instance()
				},
				{
					value: 'snatcharr_update_instance',
					label: () => m.settings_llm_tool_update_instance()
				},
				{
					value: 'snatcharr_delete_instance',
					label: () => m.settings_llm_tool_delete_instance()
				},
				{ value: 'snatcharr_test_instance', label: () => m.settings_llm_tool_test_instance() },
				{
					value: 'snatcharr_test_instance_input',
					label: () => m.settings_llm_tool_test_instance_input()
				},
				{
					value: 'snatcharr_delete_instance_events',
					label: () => m.settings_llm_tool_delete_instance_events()
				},
				{ value: 'snatcharr_update_policy', label: () => m.settings_llm_tool_update_policy() },
				{
					value: 'snatcharr_update_queue_policy',
					label: () => m.settings_llm_tool_update_queue_policy()
				},
				{ value: 'snatcharr_trigger_run', label: () => m.settings_llm_tool_trigger_run() },
				{ value: 'snatcharr_preview_run', label: () => m.settings_llm_tool_preview_run() },
				{ value: 'snatcharr_cancel_run', label: () => m.settings_llm_tool_cancel_run() },
				{ value: 'snatcharr_reset_state', label: () => m.settings_llm_tool_reset_state() },
				{
					value: 'snatcharr_create_schedule',
					label: () => m.settings_llm_tool_create_schedule()
				},
				{
					value: 'snatcharr_update_schedule',
					label: () => m.settings_llm_tool_update_schedule()
				},
				{
					value: 'snatcharr_delete_schedule',
					label: () => m.settings_llm_tool_delete_schedule()
				},
				{
					value: 'snatcharr_import_configarr',
					label: () => m.settings_llm_tool_import_configarr()
				}
			]
		},
		{
			field: 'llm_write_tools_admin',
			legend: () => m.settings_llm_scope_admin_write(),
			options: [
				{
					value: 'snatcharr_update_settings',
					label: () => m.settings_llm_tool_update_settings()
				}
			]
		},
		{
			field: 'llm_write_tools_download_clients',
			legend: () => m.settings_llm_scope_download_clients_write(),
			options: [
				{
					value: 'snatcharr_create_download_client',
					label: () => m.settings_llm_tool_create_download_client()
				},
				{
					value: 'snatcharr_test_download_client_input',
					label: () => m.settings_llm_tool_test_download_client_input()
				},
				{
					value: 'snatcharr_update_download_client',
					label: () => m.settings_llm_tool_update_download_client()
				},
				{
					value: 'snatcharr_delete_download_client',
					label: () => m.settings_llm_tool_delete_download_client()
				},
				{
					value: 'snatcharr_test_download_client',
					label: () => m.settings_llm_tool_test_download_client()
				},
				{
					value: 'snatcharr_discover_download_clients',
					label: () => m.settings_llm_tool_discover_download_clients()
				}
			]
		},
		{
			field: 'llm_write_tools_requests',
			legend: () => m.settings_llm_scope_requests_write(),
			options: [
				{
					value: 'snatcharr_create_seerr_link',
					label: () => m.settings_llm_tool_create_seerr_link()
				},
				{
					value: 'snatcharr_test_seerr_link_input',
					label: () => m.settings_llm_tool_test_seerr_link_input()
				},
				{
					value: 'snatcharr_snatch_seerr_request',
					label: () => m.settings_llm_tool_snatch_seerr_request()
				},
				{
					value: 'snatcharr_update_seerr_link',
					label: () => m.settings_llm_tool_update_seerr_link()
				},
				{
					value: 'snatcharr_delete_seerr_link',
					label: () => m.settings_llm_tool_delete_seerr_link()
				},
				{
					value: 'snatcharr_test_seerr_link',
					label: () => m.settings_llm_tool_test_seerr_link()
				},
				{
					value: 'snatcharr_sync_seerr_link',
					label: () => m.settings_llm_tool_sync_seerr_link()
				},
				{
					value: 'snatcharr_import_seerr_instances',
					label: () => m.settings_llm_tool_import_seerr_instances()
				}
			]
		},
		{
			field: 'llm_write_tools_prowlarr',
			legend: () => m.settings_llm_scope_prowlarr_write(),
			options: [
				{
					value: 'snatcharr_create_prowlarr_link',
					label: () => m.settings_llm_tool_create_prowlarr_link()
				},
				{
					value: 'snatcharr_test_prowlarr_link_input',
					label: () => m.settings_llm_tool_test_prowlarr_link_input()
				},
				{
					value: 'snatcharr_update_prowlarr_link',
					label: () => m.settings_llm_tool_update_prowlarr_link()
				},
				{
					value: 'snatcharr_delete_prowlarr_link',
					label: () => m.settings_llm_tool_delete_prowlarr_link()
				},
				{
					value: 'snatcharr_test_prowlarr_link',
					label: () => m.settings_llm_tool_test_prowlarr_link()
				}
			]
		},
		{
			field: 'llm_write_tools_notifications',
			legend: () => m.settings_llm_scope_notifications_write(),
			options: [
				{
					value: 'snatcharr_create_notification_channel',
					label: () => m.settings_llm_tool_create_notification_channel()
				},
				{
					value: 'snatcharr_update_notification_channel',
					label: () => m.settings_llm_tool_update_notification_channel()
				},
				{
					value: 'snatcharr_delete_notification_channel',
					label: () => m.settings_llm_tool_delete_notification_channel()
				},
				{
					value: 'snatcharr_test_notification_channel',
					label: () => m.settings_llm_tool_test_notification_channel()
				},
				{
					value: 'snatcharr_create_notification_rule',
					label: () => m.settings_llm_tool_create_notification_rule()
				},
				{
					value: 'snatcharr_update_notification_rule',
					label: () => m.settings_llm_tool_update_notification_rule()
				},
				{
					value: 'snatcharr_delete_notification_rule',
					label: () => m.settings_llm_tool_delete_notification_rule()
				}
			]
		}
	];

	const knownWriteTools = new Set<string>(
		writeToolGroups.flatMap((g) => g.options.map((o) => o.value))
	);
	const schema = generalSettingsSchema.extend({
		llm_write_tools: z
			.array(z.custom<LlmWriteTool>((v) => typeof v === 'string' && knownWriteTools.has(v)))
			.optional()
	});

	const settings = createSentioQuery({
		queryKey: keys.settings,
		queryFn: async () => (await api.GET('/settings')).data
	});

	let draft = $state<GeneralSettingsDraft | null>(null);
	let currentPassword = $state('');
	let newPassword = $state('');
	let passwordError = $state('');
	let changingPassword = $state(false);

	$effect(() => {
		if (settings.data && !draft) {
			draft = {
				history_retention_days: settings.data.history_retention_days,
				user_agent: settings.data.user_agent,
				llm_enabled: settings.data.llm_enabled,
				llm_write_tools: [...(settings.data.llm_write_tools ?? [])],
				brand_mark: settings.data.brand_mark ?? 'vulva',
				sfw_mode: settings.data.sfw_mode ?? 'auto'
			};
		}
	});

	function toggleWriteTool(tool: LlmWriteTool, checked: boolean): void {
		if (!draft) return;
		const current = draft.llm_write_tools ?? [];
		draft.llm_write_tools = checked
			? current.includes(tool)
				? current
				: [...current, tool]
			: current.filter((t) => t !== tool);
	}

	function allowedCount(group: WriteToolGroup): number {
		if (!draft?.llm_write_tools) return 0;
		const tools = draft.llm_write_tools;
		return group.options.filter((opt) => tools.includes(opt.value)).length;
	}

	const save = createSettingsSave(mergeGeneralSettings);

	async function changePassword(e: SubmitEvent): Promise<void> {
		e.preventDefault();
		changingPassword = true;
		passwordError = '';
		try {
			await api.POST('/auth/password', {
				body: {
					current_password: currentPassword,
					new_password: newPassword
				}
			});
			toast.success(m.password_changed());
			currentPassword = '';
			newPassword = '';
		} catch (err) {
			passwordError = describeError(err);
		} finally {
			changingPassword = false;
		}
	}
</script>

<section class="w-full" aria-labelledby="settings-main-title">
	<h2 id="settings-main-title" class="mb-4 text-2xl font-semibold">{m.settings_title()}</h2>

	<QueryGuard query={settings}>
		{#if draft}
			<div class="flex w-full flex-col gap-6">
				{#if save.isError}
					<ProblemNote error={save.error} />
				{/if}

				<form
					class="card flex flex-col gap-4"
					onsubmit={(e) => {
						e.preventDefault();
						if (draft && schema.safeParse(draft).success) save.mutate(draft);
					}}
				>
					<div class="grid grid-cols-1 gap-4 md:grid-cols-2">
						<Field id="retention" label={m.settings_retention()} hint={m.settings_retention_hint()}>
							<input
								id="retention"
								data-field="history_retention_days"
								class="input"
								type="number"
								min="1"
								max="3650"
								aria-describedby="retention-hint"
								bind:value={draft.history_retention_days}
							/>
						</Field>
						<Field id="ua" label={m.settings_user_agent()} hint={m.settings_user_agent_hint()}>
							<input
								id="ua"
								data-field="user_agent"
								class="input"
								maxlength="200"
								aria-describedby="ua-hint"
								bind:value={draft.user_agent}
							/>
						</Field>
						<BrandMarkSelect bind:value={draft.brand_mark} />
						<SfwModeSelect bind:value={draft.sfw_mode} />
					</div>
					<label class="flex items-center gap-2">
						<input type="checkbox" data-field="llm_enabled" bind:checked={draft.llm_enabled} />
						{m.settings_llm_enabled()}
					</label>
					<div class="flex flex-col gap-3" data-field="llm_write_tools">
						<span class="text-sm font-medium">{m.settings_llm_write_tools()}</span>
						<div class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
							{#each writeToolGroups as group (group.field)}
								<details class="border-border rounded border p-3">
									<summary
										class="flex cursor-pointer items-center justify-between gap-2 text-sm font-medium select-none"
									>
										<span>{group.legend()}</span>
										<span class="text-xs font-normal text-subtle">
											{m.settings_llm_write_tools_allowed({
												count: allowedCount(group),
												total: group.options.length
											})}
										</span>
									</summary>
									<fieldset
										data-field={group.field}
										disabled={!draft.llm_enabled}
										class="mt-3 flex flex-col gap-2 border-0 p-0"
									>
										<legend class="sr-only">{group.legend()}</legend>
										{#each group.options as opt (opt.value)}
											<label class="flex items-center gap-2 text-sm">
												<input
													type="checkbox"
													value={opt.value}
													disabled={!draft.llm_enabled}
													checked={(draft.llm_write_tools ?? []).includes(opt.value)}
													onchange={(e) => toggleWriteTool(opt.value, e.currentTarget.checked)}
												/>
												{opt.label()}
											</label>
										{/each}
									</fieldset>
								</details>
							{/each}
						</div>
					</div>
					<div>
						<AdminSaveButton disabled={save.isPending} />
					</div>
				</form>

				<section class="card flex flex-col gap-4" aria-labelledby="change-password-heading">
					<h2 id="change-password-heading" class="text-xl font-semibold">
						{m.change_password_title()}
					</h2>
					<form class="flex flex-col gap-4" onsubmit={changePassword}>
						<div class="grid grid-cols-1 gap-4 md:grid-cols-2">
							<Field id="current-password" label={m.current_password()}>
								<input
									id="current-password"
									class="input"
									type="password"
									autocomplete="current-password"
									required
									bind:value={currentPassword}
								/>
							</Field>
							<NewPasswordField bind:value={newPassword} />
						</div>
						{#if passwordError}
							<p role="alert" class="text-sm text-danger">{passwordError}</p>
						{/if}
						<div>
							<button type="submit" class="btn btn-primary" disabled={changingPassword}>
								{m.change_password_submit()}
							</button>
						</div>
					</form>
				</section>
			</div>
		{/if}
	</QueryGuard>
</section>
