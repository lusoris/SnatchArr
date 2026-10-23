<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery, createSentioMutation } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { z } from 'zod';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import AdminSaveButton from '#lib/components/AdminSaveButton.svelte';
	import Field from '#lib/components/Field.svelte';
	import NewPasswordField from '#lib/components/NewPasswordField.svelte';
	import PriorityPresetFields from '#lib/components/PriorityPresetFields.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { APP_KINDS, type AppKind } from '#lib/kinds.js';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';
	import NotificationChannels from './NotificationChannels.svelte';
	import NotificationRules from './NotificationRules.svelte';

	type Settings = components['schemas']['Settings'];
	type PriorityPreset = components['schemas']['PriorityPreset'];
	type PriorityPresetMap = components['schemas']['PriorityPresetMap'];

	const rankSchema = z.number().int().min(1).max(100);
	const presetFields = Object.fromEntries(APP_KINDS.map((k) => [k, rankSchema])) as Record<
		AppKind,
		typeof rankSchema
	>;
	const presetMapSchema = z.object(presetFields) satisfies z.ZodType<PriorityPresetMap>;
	type LlmWriteTool = components['schemas']['LlmWriteTool'];

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

	const settingsSchema = z.object({
		history_retention_days: z.number().int().min(1).max(3650),
		user_agent: z.string().min(1).max(200),
		global_hourly_cap: z.number().int().min(0).max(5000),
		search_priority_preset: presetMapSchema,
		download_priority_preset: presetMapSchema,
		llm_enabled: z.boolean(),
		llm_write_tools: z
			.array(z.custom<LlmWriteTool>((v) => typeof v === 'string' && knownWriteTools.has(v)))
			.optional()
	}) satisfies z.ZodType<Omit<Settings, 'updated_at'>>;

	const settings = createSentioQuery({
		queryKey: keys.settings,
		queryFn: async () => (await api.GET('/settings')).data
	});
	const presets = createSentioQuery<PriorityPreset[] | undefined>({
		queryKey: keys.priorityPresets,
		queryFn: async () => (await api.GET('/priority-presets')).data
	});

	let draft = $state<Settings | null>(null);
	let currentPassword = $state('');
	let newPassword = $state('');
	let passwordError = $state('');
	let changingPassword = $state(false);

	const mediaFirst = $derived(presets.data?.find((p) => p.name === 'media_first'));

	$effect(() => {
		if (settings.data && !draft) {
			draft = {
				...settings.data,
				search_priority_preset: { ...(settings.data.search_priority_preset ?? {}) },
				download_priority_preset: { ...(settings.data.download_priority_preset ?? {}) },
				llm_write_tools: [...(settings.data.llm_write_tools ?? [])]
			};
		}
	});

	function resetToShippedPreset(): void {
		if (!draft || !mediaFirst) return;
		draft.search_priority_preset = { ...mediaFirst.search };
		draft.download_priority_preset = { ...mediaFirst.download };
	}

	function toggleWriteTool(tool: LlmWriteTool, checked: boolean): void {
		if (!draft) return;
		const current = draft.llm_write_tools ?? [];
		draft.llm_write_tools = checked
			? current.includes(tool)
				? current
				: [...current, tool]
			: current.filter((t) => t !== tool);
	}

	const save = createSentioMutation({
		mutationFn: async (s: Settings) => (await api.PUT('/settings', { body: s })).data,
		invalidates: [keys.settings, keys.caps, keys.instances],
		onSuccess: () => toast.success(m.settings_saved()),
		onError: (err) => toast.error(describeError(err))
	});

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

<div class="mx-auto flex max-w-xl flex-col gap-6">
	<h1 class="text-2xl font-semibold">{m.settings_title()}</h1>

	{#if settings.isPending}
		<p aria-live="polite">{m.loading()}</p>
	{:else if settings.isError}
		<ProblemNote error={settings.error} />
	{:else if draft}
		{#if save.isError}
			<ProblemNote error={save.error} />
		{/if}

		<form
			class="card flex flex-col gap-4"
			onsubmit={(e) => {
				e.preventDefault();
				if (draft && settingsSchema.safeParse(draft).success) save.mutate(draft);
			}}
		>
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
			<Field id="global_cap" label={m.settings_global_cap()} hint={m.settings_global_cap_hint()}>
				<input
					id="global_cap"
					data-field="global_hourly_cap"
					class="input"
					type="number"
					min="0"
					max="5000"
					aria-describedby="global_cap-hint"
					bind:value={draft.global_hourly_cap}
				/>
			</Field>
			<PriorityPresetFields
				bind:searchPreset={draft.search_priority_preset}
				bind:downloadPreset={draft.download_priority_preset}
				canReset={mediaFirst !== undefined}
				onReset={resetToShippedPreset}
			/>
			<label class="flex items-center gap-2">
				<input type="checkbox" data-field="llm_enabled" bind:checked={draft.llm_enabled} />
				{m.settings_llm_enabled()}
			</label>
			<div class="flex flex-col gap-3" data-field="llm_write_tools">
				<span class="text-sm font-medium">{m.settings_llm_write_tools()}</span>
				{#each writeToolGroups as group (group.field)}
					<fieldset
						data-field={group.field}
						disabled={!draft.llm_enabled}
						class="border-border flex flex-col gap-2 rounded border p-3"
					>
						<legend class="px-1 text-sm font-medium">{group.legend()}</legend>
						{#each group.options as opt (opt.value)}
							<label class="flex items-center gap-2">
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
				{/each}
			</div>
			<AdminSaveButton disabled={save.isPending} />
		</form>

		<h2 class="mt-4 text-xl font-semibold">{m.change_password_title()}</h2>
		<form class="card flex flex-col gap-4" onsubmit={changePassword}>
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
			{#if passwordError}
				<p role="alert" class="text-sm text-danger">{passwordError}</p>
			{/if}
			<div>
				<button type="submit" class="btn btn-primary" disabled={changingPassword}>
					{m.change_password_submit()}
				</button>
			</div>
		</form>

		<h2 class="mt-4 text-xl font-semibold">{m.notifications_title()}</h2>
		<NotificationChannels />
		<NotificationRules />
	{/if}
</div>
