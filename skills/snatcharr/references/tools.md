<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->

# SnatchArr MCP Tool Catalog

Complete catalog of all 72 tools exposed by the SnatchArr Model Context Protocol (MCP) server at `<snatcharr-url>/api/v1/mcp`.
Tools are grouped by permission scope. Mutating tools (`Write`) require administrator opt-in in `llm_write_tools` (**Allowed MCP write tools** on `/settings`), and destructive tools (`Destructive: Yes`) require an explicit `confirm: true` argument.
Path identifier parameters use camelCase (`instanceId`, `runId`, `scheduleId`, `clientId`, `linkId`, `channelId`, `ruleId`), whereas list query filters use snake_case (`instance_id`, `before_id`, `page_size`).

## Scope `core:read`

| Tool | Purpose | Read/Write | Destructive |
| :--- | :--- | :--- | :--- |
| `snatcharr_list_instances` | List instances (API keys are never returned). | Read | No |
| `snatcharr_get_instance` | Get one instance. | Read | No |
| `snatcharr_get_policy` | Snatch policy of an instance. | Read | No |
| `snatcharr_get_policy_filter_options` | Tags and quality profiles the instance offers for the policy filters. | Read | No |
| `snatcharr_get_queue_policy` | Queue management policy of an instance. | Read | No |
| `snatcharr_list_queue_strikes` | Active queue strikes of an instance. | Read | No |
| `snatcharr_list_runs` | Recent snatch runs, newest first. | Read | No |
| `snatcharr_get_run` | Load a single snatch run by ID. | Read | No |
| `snatcharr_list_events` | Snatch history, newest first (cursor on before_id). | Read | No |
| `snatcharr_get_hourly_caps` | Per-instance and global stamina used versus cap in the last 60 minutes (rolling window). | Read | No |
| `snatcharr_list_schedules` | All schedule windows. | Read | No |
| `snatcharr_get_configarr_status` | Linked-mode state and the last import. | Read | No |

## Scope `core:write`

| Tool | Purpose | Read/Write | Destructive |
| :--- | :--- | :--- | :--- |
| `snatcharr_trigger_run` | Quickie - queue a snatch now (ignores the refractory period). | Write | No |
| `snatcharr_update_policy` | Replace the snatch policy of an instance. | Write | No |
| `snatcharr_cancel_run` | Cancel a queued or leased run. | Write | Yes |
| `snatcharr_delete_instance` | Delete an instance and everything it owns. | Write | Yes |
| `snatcharr_create_instance` | Add a manual instance. | Write | No |
| `snatcharr_test_instance_input` | Probe credentials that are not stored yet. | Write | No |
| `snatcharr_update_instance` | Update a manual instance (omit api_key to keep it). | Write | No |
| `snatcharr_test_instance` | Probe a stored instance and record the outcome. | Write | No |
| `snatcharr_update_queue_policy` | Replace the queue management policy of an instance. | Write | No |
| `snatcharr_preview_run` | Start a dry-run / preview snatch run. | Write | No |
| `snatcharr_delete_instance_events` | Clear the history of one instance. | Write | Yes |
| `snatcharr_reset_state` | Forget processed items (one instance or all). | Write | Yes |
| `snatcharr_create_schedule` | Add a schedule window. | Write | No |
| `snatcharr_update_schedule` | Replace a schedule window. | Write | No |
| `snatcharr_delete_schedule` | Delete a schedule window. | Write | Yes |
| `snatcharr_import_configarr` | Import instances from a Configarr config.yml (defaults to the linked files). | Write | No |

## Scope `requests:read`

| Tool | Purpose | Read/Write | Destructive |
| :--- | :--- | :--- | :--- |
| `snatcharr_list_seerr_links` | List Seerr links (API keys are never returned). | Read | No |
| `snatcharr_list_seerr_requests` | Cached approved-but-unavailable requests with their snatch status. | Read | No |
| `snatcharr_get_seerr_link` | Get one Seerr link. | Read | No |

## Scope `requests:write`

| Tool | Purpose | Read/Write | Destructive |
| :--- | :--- | :--- | :--- |
| `snatcharr_create_seerr_link` | Add a Seerr link. | Write | No |
| `snatcharr_test_seerr_link_input` | Probe Seerr credentials that are not stored yet. | Write | No |
| `snatcharr_snatch_seerr_request` | Quickie focused on one request (missing snatch of its instance, limited to that movie or series). | Write | No |
| `snatcharr_update_seerr_link` | Update a Seerr link (omit api_key to keep it). | Write | No |
| `snatcharr_delete_seerr_link` | Delete a Seerr link and its cached requests. | Write | Yes |
| `snatcharr_test_seerr_link` | Probe a stored Seerr link and record the outcome. | Write | No |
| `snatcharr_sync_seerr_link` | Refresh the request cache now (also runs every five minutes). | Write | No |
| `snatcharr_import_seerr_instances` | Create instances from the Sonarr/Radarr servers configured in Seerr. | Write | No |

## Scope `download_clients:read`

| Tool | Purpose | Read/Write | Destructive |
| :--- | :--- | :--- | :--- |
| `snatcharr_list_download_clients` | List download clients (secrets are never returned). | Read | No |
| `snatcharr_get_download_client_status` | Latest observation of every enabled download client (cached 20 s). | Read | No |
| `snatcharr_get_download_client` | Get one download client. | Read | No |

## Scope `download_clients:write`

| Tool | Purpose | Read/Write | Destructive |
| :--- | :--- | :--- | :--- |
| `snatcharr_create_download_client` | Add a download client by hand. | Write | No |
| `snatcharr_test_download_client_input` | Observe a download client whose credentials are not stored yet. | Write | No |
| `snatcharr_update_download_client` | Update a download client (omit secret to keep it). | Write | No |
| `snatcharr_delete_download_client` | Delete a download client. | Write | Yes |
| `snatcharr_test_download_client` | Observe a stored download client now and record the outcome. | Write | No |
| `snatcharr_discover_download_clients` | Import the download clients this instance has configured. | Write | No |

## Scope `prowlarr:read`

| Tool | Purpose | Read/Write | Destructive |
| :--- | :--- | :--- | :--- |
| `snatcharr_list_prowlarr_links` | List Prowlarr links (API keys are never returned). | Read | No |
| `snatcharr_get_prowlarr_status` | Live view of every enabled Prowlarr link (cached 30 s). | Read | No |
| `snatcharr_get_prowlarr_link` | Get one Prowlarr link. | Read | No |

## Scope `prowlarr:write`

| Tool | Purpose | Read/Write | Destructive |
| :--- | :--- | :--- | :--- |
| `snatcharr_create_prowlarr_link` | Add a Prowlarr link. | Write | No |
| `snatcharr_test_prowlarr_link_input` | Probe Prowlarr credentials that are not stored yet. | Write | No |
| `snatcharr_update_prowlarr_link` | Update a Prowlarr link (omit api_key to keep it). | Write | No |
| `snatcharr_delete_prowlarr_link` | Delete a Prowlarr link. | Write | Yes |
| `snatcharr_test_prowlarr_link` | Probe a stored Prowlarr link and record the outcome. | Write | No |

## Scope `notifications:read`

| Tool | Purpose | Read/Write | Destructive |
| :--- | :--- | :--- | :--- |
| `snatcharr_list_notification_channels` | List all configured notification channels. | Read | No |
| `snatcharr_get_notification_channel` | Get one notification channel. | Read | No |
| `snatcharr_list_notification_rules` | List all configured notification rules. | Read | No |
| `snatcharr_get_notification_rule` | Get one notification rule. | Read | No |

## Scope `notifications:write`

| Tool | Purpose | Read/Write | Destructive |
| :--- | :--- | :--- | :--- |
| `snatcharr_create_notification_channel` | Create a notification channel. | Write | No |
| `snatcharr_update_notification_channel` | Update a notification channel. | Write | No |
| `snatcharr_delete_notification_channel` | Delete a notification channel. | Write | Yes |
| `snatcharr_test_notification_channel` | Send a synchronous test notification to a channel. | Write | No |
| `snatcharr_create_notification_rule` | Create a notification rule. | Write | No |
| `snatcharr_update_notification_rule` | Update a notification rule. | Write | No |
| `snatcharr_delete_notification_rule` | Delete a notification rule. | Write | Yes |

## Scope `admin:read`

| Tool | Purpose | Read/Write | Destructive |
| :--- | :--- | :--- | :--- |
| `snatcharr_get_system_status` | Service status and build information. | Read | No |
| `snatcharr_get_setup_status` | Whether the first user still has to be created. | Read | No |
| `snatcharr_get_settings` | Runtime settings. | Read | No |
| `snatcharr_list_logs` | Recent log records of this api process and its workers, newest first (admin only). | Read | No |

## Scope `admin:write`

| Tool | Purpose | Read/Write | Destructive |
| :--- | :--- | :--- | :--- |
| `snatcharr_update_settings` | Replace the runtime settings. | Write | No |
