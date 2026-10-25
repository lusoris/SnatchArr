<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery, createSentioMutation } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import CreateForm from '#lib/components/CreateForm.svelte';
	import Field from '#lib/components/Field.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';

	type ChannelKind = components['schemas']['NotificationChannelKind'];
	// Product names, the same in every locale.
	const channelKinds: readonly { value: ChannelKind; label: string }[] = [
		{ value: 'webhook', label: 'Webhook' },
		{ value: 'discord', label: 'Discord' },
		{ value: 'slack', label: 'Slack' },
		{ value: 'telegram', label: 'Telegram' },
		{ value: 'ntfy', label: 'ntfy' },
		{ value: 'gotify', label: 'Gotify' },
		{ value: 'teams', label: 'Microsoft Teams' },
		{ value: 'apprise', label: 'Apprise' }
	];

	const channels = createSentioQuery({
		queryKey: keys.channels,
		queryFn: async () => (await api.GET('/notifications/channels')).data ?? []
	});

	let name = $state('');
	let kind = $state<ChannelKind>('webhook');
	let webhookUrl = $state('');
	let secret = $state('');
	let serverUrl = $state('');
	let topic = $state('');
	let botToken = $state('');
	let chatId = $state('');
	let slackChannel = $state('');
	let allowPrivate = $state(false);
	let allowInsecure = $state(false);
	let username = $state('');
	let tags = $state('');

	// Apprise sends to {server}/notify/{key}; username and password are its optional Basic Auth.
	function appriseConfig() {
		if (kind !== 'apprise') return {};
		const tagList = tags
			.split(',')
			.map((t) => t.trim())
			.filter(Boolean);
		return {
			webhook_url: webhookUrl,
			username: username || undefined,
			password: secret || undefined,
			tags: tagList.length > 0 ? tagList : undefined
		};
	}

	function buildConfig() {
		return {
			url: kind === 'webhook' ? webhookUrl : undefined,
			webhook_url: ['discord', 'slack', 'teams'].includes(kind) ? webhookUrl : undefined,
			secret: kind === 'webhook' ? secret || undefined : undefined,
			server_url: ['ntfy', 'gotify'].includes(kind) ? serverUrl : undefined,
			topic: kind === 'ntfy' ? topic : undefined,
			token: kind === 'ntfy' ? secret || undefined : undefined,
			app_token: kind === 'gotify' ? secret : undefined,
			bot_token: kind === 'telegram' ? botToken : undefined,
			chat_id: kind === 'telegram' ? chatId : undefined,
			channel: kind === 'slack' ? slackChannel || undefined : undefined,
			allow_private_network: allowPrivate,
			allow_insecure_http: allowInsecure,
			...appriseConfig()
		};
	}

	function resetForm() {
		name = '';
		webhookUrl = '';
		secret = '';
		serverUrl = '';
		topic = '';
		botToken = '';
		chatId = '';
		slackChannel = '';
		allowPrivate = false;
		allowInsecure = false;
		username = '';
		tags = '';
	}

	const create = createSentioMutation({
		mutationFn: async () => {
			const res = await api.POST('/notifications/channels', {
				body: {
					name,
					kind,
					enabled: true,
					config: buildConfig()
				}
			});
			return res.data;
		},
		invalidates: [keys.channels],
		onSuccess: () => {
			toast.success(m.channel_saved());
			resetForm();
		},
		onError: (err) => toast.error(describeError(err))
	});

	const remove = createSentioMutation({
		mutationFn: async (id: string) => {
			await api.DELETE('/notifications/channels/{channelId}', {
				params: { path: { channelId: id } }
			});
		},
		invalidates: [keys.channels, keys.rules],
		onSuccess: () => toast.success(m.channel_deleted()),
		onError: (err) => toast.error(describeError(err))
	});

	const testChannel = createSentioMutation({
		mutationFn: async (id: string) => {
			await api.POST('/notifications/channels/{channelId}/test', {
				params: { path: { channelId: id } }
			});
		},
		onSuccess: () => toast.success(m.channel_test_success()),
		onError: (err) => toast.error(m.channel_test_failed({ detail: describeError(err) }))
	});
</script>

<h3 class="mt-6 mb-3 text-lg font-semibold">{m.notifications_channels_title()}</h3>

{#if channels.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if channels.isError}
	<ProblemNote error={channels.error} />
{:else if (channels.data ?? []).length === 0}
	<p class="card mb-4">{m.notifications_empty_channels()}</p>
{:else}
	<div class="relative overflow-x-auto">
		<table class="table-stack mb-6 table">
			<caption class="sr-only">{m.notifications_channels_title()}</caption>
			<thead>
				<tr>
					<th scope="col">{m.channel_name()}</th>
					<th scope="col">{m.channel_kind()}</th>
					<th scope="col">{m.channel_enabled()}</th>
					<th scope="col">{m.channel_has_secret()}</th>
					{#if session.isAdmin}
						<th scope="col"><span class="sr-only">{m.action_actions()}</span></th>
					{/if}
				</tr>
			</thead>
			<tbody>
				{#each channels.data ?? [] as ch (ch.id)}
					<tr>
						<td data-label={m.channel_name()}>{ch.name}</td>
						<td data-label={m.channel_kind()}>{ch.kind}</td>
						<td data-label={m.channel_enabled()}>{ch.enabled ? '✓' : '–'}</td>
						<td data-label={m.channel_has_secret()}>{ch.has_secret ? '✓' : '–'}</td>
						{#if session.isAdmin}
							<td class="flex flex-wrap gap-2" data-label="">
								<button
									type="button"
									class="btn"
									disabled={testChannel.isPending}
									onclick={() => testChannel.mutate(ch.id)}
								>
									{m.channel_test()}
								</button>
								<button
									type="button"
									class="btn"
									disabled={remove.isPending}
									onclick={() => remove.mutate(ch.id)}
								>
									{m.channel_delete()}
								</button>
							</td>
						{/if}
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}

<CreateForm heading={m.channel_add()} onsubmit={() => create.mutate()}>
	<Field id="channel-name" label={m.channel_name()}>
		<input id="channel-name" class="input" required bind:value={name} />
	</Field>
	<Field id="channel-kind" label={m.channel_kind()}>
		<select id="channel-kind" class="select" bind:value={kind}>
			{#each channelKinds as k (k.value)}
				<option value={k.value}>{k.label}</option>
			{/each}
		</select>
	</Field>

	{#if ['webhook', 'discord', 'slack', 'teams'].includes(kind)}
		<Field id="channel-url" label={m.channel_webhook_url()}>
			<input id="channel-url" class="input" type="url" required bind:value={webhookUrl} />
		</Field>
	{/if}

	{#if kind === 'apprise'}
		<Field
			id="channel-apprise-url"
			label={m.channel_apprise_url()}
			hint={m.channel_apprise_url_hint()}
		>
			<input
				id="channel-apprise-url"
				class="input"
				type="url"
				required
				autocomplete="off"
				aria-describedby="channel-apprise-url-hint"
				bind:value={webhookUrl}
			/>
		</Field>
		<Field id="channel-username" label={m.channel_username()}>
			<input id="channel-username" class="input" autocomplete="off" bind:value={username} />
		</Field>
		<Field id="channel-password" label={m.channel_password()}>
			<input
				id="channel-password"
				class="input"
				type="password"
				autocomplete="new-password"
				bind:value={secret}
			/>
		</Field>
		<Field id="channel-tags" label={m.channel_tags()} hint={m.channel_tags_hint()}>
			<input
				id="channel-tags"
				class="input"
				aria-describedby="channel-tags-hint"
				bind:value={tags}
			/>
		</Field>
	{/if}

	{#if ['ntfy', 'gotify'].includes(kind)}
		<Field id="channel-server-url" label={m.channel_server_url()}>
			<input id="channel-server-url" class="input" type="url" required bind:value={serverUrl} />
		</Field>
	{/if}

	{#if kind === 'ntfy'}
		<Field id="channel-topic" label={m.channel_topic()}>
			<input id="channel-topic" class="input" required bind:value={topic} />
		</Field>
	{/if}

	{#if kind === 'slack'}
		<Field id="channel-slack" label={m.channel_slack_channel()}>
			<input id="channel-slack" class="input" bind:value={slackChannel} />
		</Field>
	{/if}

	{#if kind === 'telegram'}
		<Field id="channel-bot-token" label={m.channel_bot_token()}>
			<input id="channel-bot-token" class="input" type="password" required bind:value={botToken} />
		</Field>
		<Field id="channel-chat-id" label={m.channel_chat_id()}>
			<input id="channel-chat-id" class="input" required bind:value={chatId} />
		</Field>
	{/if}

	{#if ['webhook', 'ntfy', 'gotify'].includes(kind)}
		<Field id="channel-secret" label={m.channel_secret()}>
			<input
				id="channel-secret"
				class="input"
				type="password"
				required={kind === 'gotify'}
				bind:value={secret}
			/>
		</Field>
	{/if}

	{#if kind === 'webhook'}
		<div class="flex flex-col gap-2">
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" bind:checked={allowPrivate} />
				<span>{m.channel_allow_private()}</span>
			</label>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" bind:checked={allowInsecure} />
				<span>{m.channel_allow_insecure()}</span>
			</label>
		</div>
	{/if}

	<div>
		<button type="submit" class="btn btn-primary" disabled={create.isPending}>
			{m.channel_add()}
		</button>
	</div>
</CreateForm>
