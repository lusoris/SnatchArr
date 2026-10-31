// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import fs from 'node:fs';
import path from 'node:path';
import { expect, test, type APIRequestContext } from '@playwright/test';

const e2eApi = process.env.SNATCHARR_E2E_API ?? '';
const sweepEnabled = process.env.SNATCHARR_E2E_SWEEP === '1';

const ALL_SCOPES = [
	'core:read',
	'core:write',
	'requests:read',
	'requests:write',
	'download_clients:read',
	'download_clients:write',
	'prowlarr:read',
	'prowlarr:write',
	'notifications:read',
	'notifications:write',
	'admin:read',
	'admin:write',
	'adult:read',
	'adult:write'
];

// Every write tool the API knows: the LlmWriteTool enum in the OpenAPI contract.
function readWriteToolEnum(): string[] {
	const spec = fs.readFileSync(
		path.resolve(import.meta.dirname, '../../api/openapi/openapi.yaml'),
		'utf-8'
	);
	const lines = spec.split('\n');
	const start = lines.indexOf('    LlmWriteTool:');
	const names: string[] = [];
	for (let i = start + 3; start >= 0 && i < lines.length && i < start + 500; i++) {
		const m = /^ {8}- (snatcharr_[a-z_]+)$/.exec(lines[i]);
		if (!m) break;
		names.push(m[1]);
	}
	if (names.length === 0) throw new Error('LlmWriteTool enum not found in openapi.yaml');
	return names;
}

const ALL_WRITE_TOOLS = readWriteToolEnum();

// Destructive tools run last, each against a record this sweep created itself.
// Value: [argument name, created-record key, order within the destructive tail].
const DELETE_TOOLS = new Map<string, [string, CreatedKey | 'run', number]>([
	['snatcharr_cancel_run', ['runId', 'run', 0]],
	['snatcharr_reset_state', ['', 'instance', 1]],
	['snatcharr_delete_instance_events', ['instanceId', 'instance', 2]],
	['snatcharr_delete_notification_rule', ['ruleId', 'rule', 3]],
	['snatcharr_delete_notification_channel', ['channelId', 'channel', 4]],
	['snatcharr_delete_schedule', ['scheduleId', 'schedule', 4]],
	['snatcharr_delete_download_client', ['clientId', 'client', 4]],
	['snatcharr_delete_prowlarr_link', ['linkId', 'prowlarr', 4]],
	['snatcharr_delete_seerr_link', ['linkId', 'seerr', 4]],
	['snatcharr_delete_instance', ['instanceId', 'instance', 5]]
]);

type CreatedKey = 'instance' | 'client' | 'schedule' | 'channel' | 'rule' | 'prowlarr' | 'seerr';

const CREATE_TOOLS: Record<string, CreatedKey> = {
	snatcharr_create_instance: 'instance',
	snatcharr_create_download_client: 'client',
	snatcharr_create_schedule: 'schedule',
	snatcharr_create_notification_channel: 'channel',
	snatcharr_create_notification_rule: 'rule',
	snatcharr_create_prowlarr_link: 'prowlarr',
	snatcharr_create_seerr_link: 'seerr'
};

const NO_PARAM_TOOLS = new Set([
	'snatcharr_get_system_status',
	'snatcharr_get_setup_status',
	'snatcharr_get_settings',
	'snatcharr_list_logs',
	'snatcharr_get_hourly_caps',
	'snatcharr_get_configarr_status',
	'snatcharr_list_instances',
	'snatcharr_list_schedules',
	'snatcharr_list_download_clients',
	'snatcharr_get_download_client_status',
	'snatcharr_list_notification_channels',
	'snatcharr_list_notification_rules',
	'snatcharr_list_prowlarr_links',
	'snatcharr_get_prowlarr_status',
	'snatcharr_list_seerr_links',
	'snatcharr_list_seerr_requests',
	'snatcharr_list_runs',
	'snatcharr_list_events',
	'snatcharr_import_configarr'
]);

const DEFAULT_POLICY: Record<string, unknown> = {
	missing_per_cycle: 10,
	upgrade_per_cycle: 10,
	cycle_interval_s: 300,
	hourly_cap: 50,
	selection: 'all',
	monitored_only: true,
	skip_future_releases: true,
	radarr_release_type: 'all',
	sonarr_missing_mode: 'all',
	sonarr_upgrade_mode: 'all',
	lidarr_missing_mode: 'all',
	processed_ttl_h: 24,
	max_queue_size: 50,
	await_command: true,
	page_size: 50,
	recent_grab_window_h: 24,
	afterglow_max_h: 24
};

const DEFAULT_QUEUE_POLICY: Record<string, unknown> = {
	enabled: true,
	rules: {
		stalled: { enabled: true, grace_seconds: 300, max_strikes: 3 },
		slow: { enabled: true, grace_seconds: 300, max_strikes: 3 },
		failed: { enabled: true, grace_seconds: 300, max_strikes: 3 },
		import_blocked: { enabled: true, grace_seconds: 300, max_strikes: 3 },
		unwanted_score: { enabled: true, grace_seconds: 300, max_strikes: 3 },
		unwanted_unmonitored: { enabled: true, grace_seconds: 300, max_strikes: 3 }
	},
	grace_seconds: 300,
	min_bytes_per_second: 1024,
	max_removals_per_sweep: 5
};

type SweepVerdict = 'pass' | 'warn' | 'fail' | 'skip';

interface SweepRow {
	tool: string;
	kind: 'read' | 'write';
	argsSummary: string;
	verdict: SweepVerdict;
	detail: string;
}

interface FixtureState {
	instanceId?: string;
	clientId?: string;
	scheduleId?: string;
	channelId?: string;
	ruleId?: string;
	prowlarrLinkId?: string;
	seerrLinkId?: string;
	runId?: string;
	policy?: Record<string, unknown>;
	queuePolicy?: Record<string, unknown>;
	settings?: Record<string, unknown>;
	created: Partial<Record<CreatedKey, string>>;
}

interface SweepEnv {
	fakearrUrl: string;
	fakearrFlowUrl: string;
}

interface McpTool {
	name: string;
	description?: string;
	inputSchema?: unknown;
	annotations?: {
		readOnlyHint?: boolean;
		destructiveHint?: boolean;
	};
}

type ToolArgResult = { skip?: string; args?: Record<string, unknown> };

function isSafeUrlString(val: string): boolean {
	if (!val.includes('://')) return true;
	try {
		const parsed = new URL(val);
		return parsed.hostname === '127.0.0.1' || parsed.hostname === 'localhost';
	} catch {
		return false;
	}
}

// Keys whose values are never a request target: a User-Agent string may name a project URL.
const NON_TARGET_KEYS = new Set(['user_agent']);

// Walks the argument tree breadth first; refuses any URL off the loopback host.
function checkSafetyGuard(root: unknown): boolean {
	const queue: unknown[] = [root];
	for (let i = 0; i < queue.length; i++) {
		if (i > 10_000) return false;
		const node = queue[i];
		if (typeof node === 'string' && !isSafeUrlString(node)) return false;
		if (node === null || typeof node !== 'object') continue;
		for (const [key, value] of Object.entries(node)) {
			if (!NON_TARGET_KEYS.has(key)) queue.push(value);
		}
	}
	return true;
}

function assertSafeApiBase(apiBase: string): void {
	const parsed = new URL(apiBase);
	if (parsed.hostname !== '127.0.0.1' && parsed.hostname !== 'localhost') {
		throw new Error(`guard: external url in apiBase: ${parsed.hostname}`);
	}
}

function escapeTsv(str: string): string {
	return str.replace(/[\t\r\n]+/g, ' ').trim();
}

function summarizeArgs(args: Record<string, unknown>): string {
	if (Object.keys(args).length === 0) return '{}';
	const raw = JSON.stringify(args);
	return raw.length > 80 ? `${raw.slice(0, 77)}...` : raw;
}

function classifyRpcResult(
	status: number,
	data: unknown,
	rawText: string
): { verdict: SweepVerdict; detail: string } {
	if (status >= 500 || status !== 200) {
		return { verdict: 'fail', detail: `HTTP ${status}: ${rawText.slice(0, 150)}` };
	}
	const json = data as {
		error?: { code?: number; message?: string };
		result?: { isError?: boolean; content?: Array<{ text?: string }> };
	} | null;
	if (!json) {
		return { verdict: 'fail', detail: `invalid json: ${rawText.slice(0, 140)}` };
	}
	if (json.error) {
		const msg = `RPC ${json.error.code ?? 'err'}: ${json.error.message ?? ''}`.slice(0, 160);
		return { verdict: 'fail', detail: msg };
	}
	return classifyToolResult(json.result);
}

function classifyToolResult(res?: { isError?: boolean; content?: Array<{ text?: string }> }): {
	verdict: SweepVerdict;
	detail: string;
} {
	if (!res?.isError) {
		const text = res?.content?.[0]?.text ?? 'ok';
		return { verdict: 'pass', detail: text.slice(0, 160) };
	}
	const errText = res.content?.map((c) => c.text ?? '').join(' ') ?? 'error';
	const detail = errText
		.replace(/[\t\r\n]+/g, ' ')
		.trim()
		.slice(0, 160);
	if (/internal|panic|500/i.test(detail)) {
		return { verdict: 'fail', detail };
	}
	return { verdict: 'warn', detail };
}

async function getSessionCsrf(request: APIRequestContext, apiBase: string): Promise<string> {
	let res = await request.get('/api/v1/auth/session', { timeout: 20_000 });
	if (!res.ok()) {
		res = await request.get(`${apiBase}/api/v1/auth/session`, { timeout: 20_000 });
	}
	expect(res.ok(), `get /auth/session status: ${res.status()}`).toBeTruthy();
	const data = (await res.json()) as { csrf_token: string };
	return data.csrf_token;
}

async function enableMcpInSettings(
	request: APIRequestContext,
	apiBase: string,
	csrf: string
): Promise<Record<string, unknown>> {
	let res = await request.get('/api/v1/settings', { timeout: 20_000 });
	if (!res.ok()) {
		res = await request.get(`${apiBase}/api/v1/settings`, { timeout: 20_000 });
	}
	expect(res.ok(), `get /settings status: ${res.status()}`).toBeTruthy();
	const current = (await res.json()) as Record<string, unknown>;
	current.llm_enabled = true;
	current.llm_write_tools = ALL_WRITE_TOOLS;

	let putRes = await request.put('/api/v1/settings', {
		data: current,
		headers: { 'X-CSRF-Token': csrf },
		timeout: 20_000
	});
	if (!putRes.ok()) {
		putRes = await request.put(`${apiBase}/api/v1/settings`, {
			data: current,
			headers: { 'X-CSRF-Token': csrf },
			timeout: 20_000
		});
	}
	expect(putRes.ok(), `put /settings status: ${putRes.status()}`).toBeTruthy();
	return current;
}

async function createSweepApiKey(
	request: APIRequestContext,
	apiBase: string,
	csrf: string
): Promise<string> {
	let res = await request.post('/api/v1/auth/api-keys', {
		data: { name: 'mcp-sweep-key', scopes: ALL_SCOPES },
		headers: { 'X-CSRF-Token': csrf },
		timeout: 20_000
	});
	if (!res.ok()) {
		res = await request.post(`${apiBase}/api/v1/auth/api-keys`, {
			data: { name: 'mcp-sweep-key', scopes: ALL_SCOPES },
			headers: { 'X-CSRF-Token': csrf },
			timeout: 20_000
		});
	}
	expect(res.ok(), `create api key status: ${res.status()}`).toBeTruthy();
	const created = (await res.json()) as { secret: string };
	return created.secret;
}

function safeJsonParse(text: string): unknown {
	try {
		return JSON.parse(text);
	} catch {
		return null;
	}
}

async function postJsonRpc(
	apiBase: string,
	apiKey: string,
	sessionId: string | null,
	payload: unknown
): Promise<{ status: number; data: unknown; rawText: string; sessionId: string | null }> {
	const headers: Record<string, string> = {
		Authorization: `Bearer ${apiKey}`,
		'Content-Type': 'application/json',
		Accept: 'application/json, text/event-stream'
	};
	if (sessionId) {
		headers['Mcp-Session-Id'] = sessionId;
	}
	const res = await fetch(`${apiBase}/api/v1/mcp`, {
		method: 'POST',
		headers,
		body: JSON.stringify(payload),
		signal: AbortSignal.timeout(20_000)
	});
	const rawText = await res.text();
	const nextSessionId = res.headers.get('mcp-session-id') ?? sessionId;
	const data = safeJsonParse(rawText);
	return { status: res.status, data, rawText, sessionId: nextSessionId };
}

async function initMcpSession(apiBase: string, apiKey: string): Promise<string | null> {
	const res = await postJsonRpc(apiBase, apiKey, null, {
		jsonrpc: '2.0',
		id: 1,
		method: 'initialize',
		params: {
			protocolVersion: '2025-03-26',
			capabilities: {},
			clientInfo: { name: 'snatcharr-mcp-sweep', version: '1.0.0' }
		}
	});
	expect(res.status, `initialize failed status ${res.status}: ${res.rawText}`).toBe(200);
	return res.sessionId;
}

async function listMcpTools(
	apiBase: string,
	apiKey: string,
	sessionId: string | null
): Promise<McpTool[]> {
	const res = await postJsonRpc(apiBase, apiKey, sessionId, {
		jsonrpc: '2.0',
		id: 2,
		method: 'tools/list',
		params: {}
	});
	expect(res.status, `tools/list failed status ${res.status}`).toBe(200);
	const body = res.data as { result?: { tools?: McpTool[] } };
	const tools = body?.result?.tools;
	expect(Array.isArray(tools), 'tools/list result.tools must be array').toBeTruthy();
	return tools ?? [];
}

function buildInstanceToolArgs(name: string, state: FixtureState, env: SweepEnv): ToolArgResult {
	if (!state.instanceId) return { skip: 'no instance fixture' };
	const id = state.instanceId;
	if (name === 'snatcharr_update_policy') {
		return { args: { instanceId: id, body: state.policy ?? DEFAULT_POLICY } };
	}
	if (name === 'snatcharr_update_queue_policy') {
		return { args: { instanceId: id, body: state.queuePolicy ?? DEFAULT_QUEUE_POLICY } };
	}
	if (name === 'snatcharr_trigger_run') {
		return { args: { instanceId: id, body: { kind: 'missing' } } };
	}
	if (name === 'snatcharr_preview_run') {
		return { args: { instanceId: id, kind: 'missing' } };
	}
	if (name === 'snatcharr_update_instance') {
		return {
			args: {
				instanceId: id,
				body: { kind: 'sonarr', name: 'seeded-sonarr', base_url: env.fakearrUrl, enabled: true }
			}
		};
	}
	return { args: { instanceId: id } };
}

function buildClientToolArgs(name: string, state: FixtureState): ToolArgResult {
	if (name === 'snatcharr_create_download_client') {
		return {
			args: {
				body: {
					kind: 'transmission',
					name: 'mcp-sweep-client',
					base_url: 'http://127.0.0.1:9091',
					enabled: true
				}
			}
		};
	}
	if (name === 'snatcharr_test_download_client_input') {
		return {
			args: {
				body: { kind: 'transmission', name: 'test-dl-in', base_url: 'http://127.0.0.1:9091' }
			}
		};
	}
	if (name === 'snatcharr_discover_download_clients') {
		return state.instanceId
			? { args: { instanceId: state.instanceId } }
			: { skip: 'no instance fixture' };
	}
	if (!state.clientId) return { skip: 'no download client fixture' };
	if (name === 'snatcharr_update_download_client') {
		return {
			args: {
				clientId: state.clientId,
				body: {
					kind: 'transmission',
					name: 'seeded-transmission',
					base_url: 'http://127.0.0.1:9091',
					enabled: true
				}
			}
		};
	}
	return { args: { clientId: state.clientId } };
}

function buildScheduleToolArgs(name: string, state: FixtureState): ToolArgResult {
	if (name === 'snatcharr_create_schedule') {
		return {
			args: {
				body: {
					name: 'mcp-sweep-sched',
					days: [1, 2, 3],
					start: '09:00',
					end: '18:00',
					tz: 'UTC',
					action: 'pause',
					enabled: true
				}
			}
		};
	}
	if (!state.scheduleId) return { skip: 'no schedule fixture' };
	return {
		args: {
			scheduleId: state.scheduleId,
			body: {
				name: 'seeded-schedule',
				days: [1, 2, 3, 4, 5],
				start: '08:00',
				end: '17:00',
				tz: 'UTC',
				action: 'pause',
				enabled: true
			}
		}
	};
}

const SWEEP_WEBHOOK_CONFIG = {
	url: 'http://127.0.0.1:9091/webhook',
	allow_private_networks: true,
	allow_insecure_http: true
};

function buildNotificationToolArgs(name: string, state: FixtureState): ToolArgResult {
	if (name === 'snatcharr_create_notification_channel') {
		return {
			args: {
				body: {
					name: 'mcp-sweep-chan',
					kind: 'webhook',
					config: SWEEP_WEBHOOK_CONFIG,
					enabled: true
				}
			}
		};
	}
	if (name === 'snatcharr_create_notification_rule') {
		if (!state.channelId) return { skip: 'no channel fixture for rule' };
		return {
			args: {
				body: {
					name: 'mcp-sweep-rule',
					event_kind: 'run_failure_streak',
					threshold: 2,
					channel_ids: [state.channelId]
				}
			}
		};
	}
	return buildNotificationItemArgs(name, state);
}

function buildNotificationItemArgs(name: string, state: FixtureState): ToolArgResult {
	if (name.includes('rule')) {
		if (!state.ruleId) return { skip: 'no notification rule fixture' };
		if (name === 'snatcharr_update_notification_rule') {
			return {
				args: {
					ruleId: state.ruleId,
					body: {
						name: 'seeded-rule',
						event_kind: 'run_failure_streak',
						threshold: 2,
						channel_ids: state.channelId ? [state.channelId] : []
					}
				}
			};
		}
		return { args: { ruleId: state.ruleId } };
	}
	if (!state.channelId) return { skip: 'no notification channel fixture' };
	if (name === 'snatcharr_update_notification_channel') {
		return {
			args: {
				channelId: state.channelId,
				body: {
					name: 'seeded-webhook-channel',
					kind: 'webhook',
					config: SWEEP_WEBHOOK_CONFIG,
					enabled: true
				}
			}
		};
	}
	return { args: { channelId: state.channelId } };
}

function buildProwlarrToolArgs(name: string, state: FixtureState): ToolArgResult {
	if (name === 'snatcharr_create_prowlarr_link') {
		return {
			args: {
				body: {
					name: 'mcp-sweep-prowlarr',
					base_url: 'http://127.0.0.1:9696',
					api_key: 'fake-key',
					enabled: true
				}
			}
		};
	}
	if (name === 'snatcharr_test_prowlarr_link_input') {
		return {
			args: {
				body: { name: 'test-prowlarr-in', base_url: 'http://127.0.0.1:9696', api_key: 'fake-key' }
			}
		};
	}
	if (!state.prowlarrLinkId) return { skip: 'no prowlarr link fixture' };
	if (name === 'snatcharr_update_prowlarr_link') {
		return {
			args: {
				linkId: state.prowlarrLinkId,
				body: { name: 'mcp-sweep-prowlarr', base_url: 'http://127.0.0.1:9696', enabled: true }
			}
		};
	}
	return { args: { linkId: state.prowlarrLinkId } };
}

function buildSeerrToolArgs(name: string, state: FixtureState): ToolArgResult {
	if (name === 'snatcharr_create_seerr_link') {
		return {
			args: {
				body: {
					name: 'mcp-sweep-seerr',
					base_url: 'http://127.0.0.1:5055',
					api_key: 'fake-key',
					enabled: true
				}
			}
		};
	}
	if (name === 'snatcharr_test_seerr_link_input') {
		return {
			args: {
				body: { name: 'test-seerr-in', base_url: 'http://127.0.0.1:5055', api_key: 'fake-key' }
			}
		};
	}
	if (!state.seerrLinkId) return { skip: 'no seerr link fixture' };
	if (name === 'snatcharr_snatch_seerr_request') {
		return { args: { linkId: state.seerrLinkId, requestId: 1 } };
	}
	if (name === 'snatcharr_update_seerr_link') {
		return {
			args: {
				linkId: state.seerrLinkId,
				body: { name: 'mcp-sweep-seerr', base_url: 'http://127.0.0.1:5055', enabled: true }
			}
		};
	}
	return { args: { linkId: state.seerrLinkId } };
}

function buildDeleteArgs(
	[param, key, order]: [string, CreatedKey | 'run', number],
	state: FixtureState
): ToolArgResult {
	const id = key === 'run' ? state.runId : state.created[key];
	if (!id) return { skip: `no ${key} created by this sweep` };
	if (order === 1) return { args: { confirm: true, body: { instance_id: id } } };
	return { args: { confirm: true, [param]: id } };
}

function buildToolArgs(name: string, state: FixtureState, env: SweepEnv): ToolArgResult {
	const destructive = DELETE_TOOLS.get(name);
	if (destructive) return buildDeleteArgs(destructive, state);
	if (NO_PARAM_TOOLS.has(name)) return { args: {} };
	if (name === 'snatcharr_create_instance') {
		return {
			args: {
				body: {
					kind: 'sonarr',
					name: 'mcp-sweep-inst',
					base_url: env.fakearrFlowUrl,
					api_key: 'fake-api-key-0123456789',
					enabled: true
				}
			}
		};
	}
	if (name === 'snatcharr_test_instance_input') {
		return {
			args: {
				body: {
					kind: 'sonarr',
					name: 'test-inst-in',
					base_url: env.fakearrUrl,
					api_key: 'fake-api-key-0123456789'
				}
			}
		};
	}
	if (name === 'snatcharr_get_run') {
		return state.runId ? { args: { runId: state.runId } } : { skip: 'no run fixture' };
	}
	if (name === 'snatcharr_update_settings') {
		return {
			args: {
				body: state.settings ?? {
					history_retention_days: 90,
					user_agent: 'SnatchArr',
					global_hourly_cap: 0,
					search_priority_preset: {},
					download_priority_preset: {}
				}
			}
		};
	}
	return routeDomainArgs(name, state, env);
}

function routeDomainArgs(name: string, state: FixtureState, env: SweepEnv): ToolArgResult {
	if (name === 'snatcharr_import_seerr_instances') return buildSeerrToolArgs(name, state);
	if (
		name.includes('instance') ||
		name.includes('policy') ||
		name.includes('strikes') ||
		name.includes('run')
	) {
		return buildInstanceToolArgs(name, state, env);
	}
	if (name.includes('download_client')) return buildClientToolArgs(name, state);
	if (name.includes('schedule')) return buildScheduleToolArgs(name, state);
	if (name.includes('notification')) return buildNotificationToolArgs(name, state);
	if (name.includes('prowlarr')) return buildProwlarrToolArgs(name, state);
	if (name.includes('seerr')) return buildSeerrToolArgs(name, state);
	return { skip: `no argument builder defined for ${name}` };
}

function getToolPriority(name: string): number {
	const destructive = DELETE_TOOLS.get(name);
	if (destructive) return 700 + destructive[2];
	if (name === 'snatcharr_get_run') return 650;
	if (name === 'snatcharr_get_prowlarr_link' || name === 'snatcharr_get_seerr_link') return 350;
	if (
		name.includes('trigger') ||
		name.includes('preview') ||
		name.includes('sync') ||
		name.includes('import') ||
		name.includes('discover') ||
		name.includes('snatch_')
	) {
		return 600;
	}
	if (name.includes('_update_')) return 500;
	if (name.includes('_test_')) return 400;
	if (name.includes('_create_')) return 300;
	if (
		name.startsWith('snatcharr_get_') &&
		![
			'snatcharr_get_system_status',
			'snatcharr_get_setup_status',
			'snatcharr_get_settings',
			'snatcharr_get_hourly_caps',
			'snatcharr_get_configarr_status',
			'snatcharr_get_download_client_status',
			'snatcharr_get_prowlarr_status'
		].includes(name)
	) {
		return 200;
	}
	return 100;
}

function extractStructuredData(data: unknown): unknown {
	const res = (
		data as {
			result?: { structuredContent?: unknown; content?: Array<{ text?: string }> };
		}
	)?.result;
	if (res?.structuredContent) return res.structuredContent;
	if (res?.content?.[0]?.text) {
		try {
			return JSON.parse(res.content[0].text);
		} catch {
			return null;
		}
	}
	return null;
}

function recordCreated(toolName: string, data: unknown, state: FixtureState): void {
	const key = CREATE_TOOLS[toolName];
	const id = (data as { id?: string } | null)?.id;
	if (!key || !id) return;
	state.created[key] = id;
	const ids: Record<CreatedKey, keyof FixtureState> = {
		instance: 'instanceId',
		client: 'clientId',
		schedule: 'scheduleId',
		channel: 'channelId',
		rule: 'ruleId',
		prowlarr: 'prowlarrLinkId',
		seerr: 'seerrLinkId'
	};
	const field = ids[key];
	if (key !== 'instance' && !state[field]) Object.assign(state, { [field]: id });
}

// List tool -> state field that takes the first listed id.
const LIST_FIRST_ID: Record<string, keyof FixtureState> = {
	snatcharr_list_schedules: 'scheduleId',
	snatcharr_list_download_clients: 'clientId',
	snatcharr_list_notification_channels: 'channelId',
	snatcharr_list_notification_rules: 'ruleId',
	snatcharr_list_prowlarr_links: 'prowlarrLinkId',
	snatcharr_list_seerr_links: 'seerrLinkId',
	snatcharr_list_runs: 'runId'
};

function recordListed(toolName: string, data: unknown, state: FixtureState): void {
	if (!Array.isArray(data) || data.length === 0) return;
	const id = (data[0] as { id?: string } | null)?.id;
	if (!id) return;
	if (toolName === 'snatcharr_list_instances') {
		state.instanceId ??= id;
		return;
	}
	const field = LIST_FIRST_ID[toolName];
	if (field) Object.assign(state, { [field]: id });
}

function updateFixtureState(toolName: string, data: unknown, state: FixtureState): void {
	recordCreated(toolName, data, state);
	recordListed(toolName, data, state);
	// cancel_run and get_run act on the run this sweep queued, not on a seeded one.
	const runId = (data as { id?: string } | null)?.id;
	if (toolName === 'snatcharr_trigger_run' && runId) state.runId = runId;
	const isObject = data !== null && typeof data === 'object';
	if (toolName === 'snatcharr_get_policy' && isObject) {
		state.policy = data as Record<string, unknown>;
	} else if (toolName === 'snatcharr_get_queue_policy' && isObject) {
		state.queuePolicy = data as Record<string, unknown>;
	}
}

async function loadSeedInstanceId(): Promise<string | undefined> {
	try {
		const seedPath = path.resolve(import.meta.dirname, '.auth/seed.json');
		const raw = await fs.promises.readFile(seedPath, 'utf-8');
		const parsed = JSON.parse(raw) as { instanceId?: string };
		return parsed.instanceId;
	} catch {
		return undefined;
	}
}

async function writeTsvReport(allTools: McpTool[], rows: SweepRow[]): Promise<void> {
	const outPath = process.env.SNATCHARR_MCP_SWEEP_OUT ?? path.resolve('test-results/mcp-sweep.tsv');
	await fs.promises.mkdir(path.dirname(outPath), { recursive: true });

	const lines = ['tool\tkind(read|write)\targs_summary\tverdict\tdetail'];
	let passCount = 0;
	let warnCount = 0;
	let skipCount = 0;
	let failCount = 0;

	for (let i = 0; i < rows.length && i < 200; i++) {
		const r = rows[i];
		if (r.verdict === 'pass') passCount++;
		else if (r.verdict === 'warn') warnCount++;
		else if (r.verdict === 'skip') skipCount++;
		else if (r.verdict === 'fail') failCount++;

		lines.push(
			`${r.tool}\t${r.kind}\t${escapeTsv(r.argsSummary)}\t${r.verdict}\t${escapeTsv(r.detail)}`
		);
	}

	const callsCount = passCount + warnCount + failCount;
	lines.push(
		`mcp tools ${allTools.length} calls ${callsCount} pass ${passCount} warn ${warnCount} skip ${skipCount} fail ${failCount}`
	);
	await fs.promises.writeFile(outPath, `${lines.join('\n')}\n`, 'utf-8');

	expect(failCount, `sweep encountered ${failCount} failures`).toBe(0);
	const recordedNames = new Set(rows.map((r) => r.tool));
	for (const t of allTools) {
		expect(recordedNames.has(t.name), `tool ${t.name} was never called nor skipped`).toBe(true);
	}
}

test.describe('mcp sweep', () => {
	test('mcp action sweep: call all tools from tools/list and write tsv', async ({ request }) => {
		test.skip(!e2eApi || !sweepEnabled, 'requires SNATCHARR_E2E_API and SNATCHARR_E2E_SWEEP=1');
		assertSafeApiBase(e2eApi);

		const csrf = await getSessionCsrf(request, e2eApi);
		const initialSettings = await enableMcpInSettings(request, e2eApi, csrf);
		const apiKey = await createSweepApiKey(request, e2eApi, csrf);

		let sessionId = await initMcpSession(e2eApi, apiKey);
		const tools = await listMcpTools(e2eApi, apiKey, sessionId);

		const sorted = [...tools].sort((a, b) => getToolPriority(a.name) - getToolPriority(b.name));

		const state: FixtureState = {
			instanceId: await loadSeedInstanceId(),
			settings: initialSettings,
			created: {}
		};
		const env: SweepEnv = {
			fakearrUrl: process.env.SNATCHARR_E2E_FAKEARR ?? 'http://127.0.0.1:18989',
			fakearrFlowUrl: process.env.SNATCHARR_E2E_FAKEARR_FLOW ?? 'http://127.0.0.1:18990'
		};

		const rows: SweepRow[] = [];
		const recorded = new Set<string>();

		for (let i = 0; i < sorted.length && i < 200; i++) {
			const tool = sorted[i];
			recorded.add(tool.name);
			const kind = tool.annotations?.readOnlyHint === false ? 'write' : 'read';
			const built = buildToolArgs(tool.name, state, env);
			if (built.skip) {
				rows.push({
					tool: tool.name,
					kind,
					argsSummary: '{}',
					verdict: 'skip',
					detail: built.skip
				});
				continue;
			}

			const args = built.args ?? {};
			if (!checkSafetyGuard(args)) {
				rows.push({
					tool: tool.name,
					kind,
					argsSummary: summarizeArgs(args),
					verdict: 'fail',
					detail: 'guard: external url'
				});
				continue;
			}

			const res = await postJsonRpc(e2eApi, apiKey, sessionId, {
				jsonrpc: '2.0',
				id: 100 + i,
				method: 'tools/call',
				params: { name: tool.name, arguments: args }
			});
			if (res.sessionId) sessionId = res.sessionId;

			const classified = classifyRpcResult(res.status, res.data, res.rawText);
			rows.push({
				tool: tool.name,
				kind,
				argsSummary: summarizeArgs(args),
				verdict: classified.verdict,
				detail: classified.detail
			});

			updateFixtureState(tool.name, extractStructuredData(res.data), state);
		}

		await writeTsvReport(tools, rows);
	});

	test('planted negative: guard refuses off-host and unparsable urls', () => {
		expect(checkSafetyGuard({ body: { base_url: 'http://127.0.0.1:18989' } })).toBe(true);
		expect(checkSafetyGuard({ body: { base_url: 'https://sonarr.example.org' } })).toBe(false);
		expect(checkSafetyGuard({ list: [{ deep: ['http://[broken'] }] })).toBe(false);
		expect(checkSafetyGuard({ body: { user_agent: 'SnatchArr/1.0 (https://github.com/x)' } })).toBe(
			true
		);
		expect(checkSafetyGuard({ body: { proxy_url: 'https://proxy.example.org' } })).toBe(false);
	});

	test('planted negative: bogus uuid for get tool yields isError and non-pass classification', async ({
		request
	}) => {
		test.skip(!e2eApi || !sweepEnabled, 'requires SNATCHARR_E2E_API and SNATCHARR_E2E_SWEEP=1');
		assertSafeApiBase(e2eApi);

		const csrf = await getSessionCsrf(request, e2eApi);
		await enableMcpInSettings(request, e2eApi, csrf);
		const apiKey = await createSweepApiKey(request, e2eApi, csrf);

		const res = await postJsonRpc(e2eApi, apiKey, null, {
			jsonrpc: '2.0',
			id: 9999,
			method: 'tools/call',
			params: {
				name: 'snatcharr_get_instance',
				arguments: { instanceId: '00000000-0000-0000-0000-000000000000' }
			}
		});
		expect(res.status).toBe(200);
		const classified = classifyRpcResult(res.status, res.data, res.rawText);
		expect(classified.verdict, 'bogus get tool call must not pass').not.toBe('pass');
		const result = (res.data as { result?: { isError?: boolean } })?.result;
		expect(result?.isError, 'bogus get tool call result must have isError: true').toBe(true);
	});
});
