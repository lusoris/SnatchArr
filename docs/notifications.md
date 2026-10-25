<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->

# Notifications

SnatchArr provides proactive notifications for operational events across *arr instances. The notification pipeline is built on `golusoris/notify` and `golusoris/webhooks/out`, operating asynchronously via a bounded background worker queue so that notification delivery never slows down or blocks snatch runs, event recording, or gRPC communications.

---

## Channels

SnatchArr supports both chat communication channels and signed outgoing webhooks. All channel secrets (bot tokens, API tokens, webhook URLs, and signing secrets) are encrypted at rest using AES-GCM and are write-only in the API.

### Supported Channel Types

| Type | Backend | Configuration Fields | Description |
| :--- | :--- | :--- | :--- |
| `discord` | Discord Webhook | `webhook_url`, optional `username`, `avatar_url` | Posts messages to Discord channel via incoming webhook. |
| `slack` | Slack Webhook | `webhook_url`, optional `channel`, `username`, `icon_emoji` | Posts formatted messages to a Slack incoming webhook. |
| `telegram` | Telegram Bot | `bot_token`, `chat_id`, optional `parse_mode` | Sends messages via the official Telegram Bot API. |
| `ntfy` | ntfy pub/sub | `server_url`, `topic`, optional `token`, `priority`, `tags` | Sends push notifications via ntfy.sh or self-hosted instances. |
| `gotify` | Gotify Server | `server_url`, `app_token`, optional `priority` | Sends push notifications via a self-hosted Gotify server. |
| `teams` | Microsoft Teams | `webhook_url`, optional `theme_color` | Sends connector cards to a Microsoft Teams channel webhook. |
| `webhook` | Outgoing Webhook | `url`, optional `secret`, `allow_private_network`, `allow_insecure_http` | Sends HTTP POST JSON payloads signed with HMAC-SHA256. |
| `apprise` | Apprise API | `webhook_url` (the notify URL), optional `username`, `password`, `tags` | Hands the message to an Apprise API server, which fans it out to every service its configuration lists. |

### Apprise API

[Apprise](https://github.com/caronc/apprise-api) is a notification gateway that speaks to more than a hundred services. SnatchArr posts to its stateful endpoint, so the destinations live in the Apprise configuration, not in SnatchArr:

- `webhook_url`: the full notify URL, `http(s)://<apprise-server>/notify/<config-key>`. The configuration key alone grants sending when the server runs without authentication, so the URL is stored as a secret, is never returned by the API, and is removed from every logged or recorded error.
- `username` and `password`: sent as HTTP Basic Auth when the server sets `APPRISE_AUTH_REQUIRED`.
- `tags`: limits delivery to the destinations of the configuration that carry one of the tags (sent comma-separated, which Apprise reads as "any of").

The event level picks the Apprise message type: `error` becomes `failure`, `warn` becomes `warning`, everything else `info`. A transport error, HTTP 429 or a 5xx response is retried twice with backoff. HTTP 424 (Apprise could not reach at least one destination) and any other 4xx is not retried, because a repeat would send the message again to the destinations that already received it. The sender code is `api/internal/notify/apprise.go`, its tests `api/internal/notify/apprise_test.go`.

The Apprise channel does not pass through the outgoing-webhook network policy below; like ntfy and Gotify it may point at a server on the local network.

### Private Network & Insecure HTTP Opt-in

By default, outgoing webhooks enforce strict network policies:
- Private IP addresses (e.g. RFC 1918 `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, loopback `127.0.0.0/8`) are blocked by default to prevent SSRF against internal infrastructure.
- Insecure plain HTTP URLs (`http://`) are blocked by default.

For homelab deployments where webhooks target internal services, administrators can explicitly opt in per webhook endpoint by enabling:
- `allow_private_network`: permits connections to private and loopback IP ranges.
- `allow_insecure_http`: permits plain HTTP endpoints without TLS.

---

## Rules & Triggers

Notifications are dispatched according to configurable rules. Each rule targets a specific event trigger and specifies a list of enabled channel IDs to receive the alert.

### Event Triggers

1. **Run failure streak (`run_failure_streak`)**
   - Fires when an instance experiences $N$ consecutive failed snatch runs across all snatch kinds (e.g. missing and cutoff-unmet).
   - The threshold $N$ is configurable (default: `1`).
   - Counter resets to zero upon the first successful run.
   - Notifications fire exactly when the streak reaches the threshold ($streak == N$), preventing alert spam on further consecutive failures while an instance remains broken.

2. **Circuit breaker opened (`circuit_opened`)**
   - Fires immediately when a worker trips its search circuit breaker after consecutive upstream search errors.

3. **Instance unreachable (`instance_unreachable`)**
   - Fires when an instance health check fails or an instance becomes unresponsive during a scheduled run. Enabled instances are checked every 5 minutes on the leader.

4. **Stamina exhausted (`stamina_exhausted`)**
   - Fires when an instance or global hourly stamina budget reaches zero.
   - **Transition Trigger:** An event is only recorded on the transition when a grant spent items and left zero remaining stamina (`Granted > 0 && Remaining == 0`). Subsequent zero-grant requests when already exhausted do not emit duplicate events.
   - **Global Cap Coverage:** The underlying `Grant` model tracks the tighter limit between instance capacity and global stamina cap, but only exposes the instance-scoped capacity (`Cap`), so notifications report the instance capacity limit.
   - **Deduplication:** At most one stamina exhausted notification is delivered per instance per 1-hour window. This deduplication is tracked in memory per replica.

5. **Seerr request snatched (`seerr_request_snatched`)**
   - Fires when a search is dispatched for an entity that resolves to a pending Overseerr or Jellyseerr request.

6. **Season gate decided (`watchaware_gate_decided`)**
   - Fires when the watch-aware season gate evaluates a newly added series on Sonarr and records decisions in observe mode.

---

## Outgoing Webhooks

### Payload Schema

Outgoing webhooks deliver an HTTP POST request with `Content-Type: application/json` containing the following JSON structure:

```json
{
  "event": "run_failure_streak",
  "timestamp": "2026-10-16T12:00:00Z",
  "data": {
    "instance_id": "018f2651-3472-749e-8c38-5188bf287c80",
    "run_id": "018f2651-3472-749e-8c38-5188bf287c81",
    "title": "Sonarr-Anime: 3 consecutive failed runs",
    "detail": "all indexers returned HTTP 503",
    "level": "error",
    "metadata": {
      "event": "run_failure_streak",
      "streak": "3"
    }
  }
}
```

### Signature Verification

When a webhook endpoint is configured with a secret, every outgoing request includes the header:
```http
X-Webhook-Signature: sha256=<hex_digest>
```
The digest is the hex-encoded HMAC-SHA256 signature calculated over the raw request body bytes using the configured secret key.

#### Verification Example (Python)

```python
import hmac
import hashlib
from flask import Flask, request, abort

app = Flask(__name__)
WEBHOOK_SECRET = b"your-configured-signing-secret"

@app.route("/webhook", methods=["POST"])
def handle_webhook():
    signature_header = request.headers.get("X-Webhook-Signature", "")
    if not signature_header.startswith("sha256="):
        abort(401, "Missing or invalid signature header")
    
    expected_sig = signature_header[len("sha256="):]
    computed_sig = hmac.new(
        WEBHOOK_SECRET,
        msg=request.get_data(),
        digestmod=hashlib.sha256
    ).hexdigest()

    if not hmac.compare_digest(expected_sig, computed_sig):
        abort(401, "Signature verification failed")

    payload = request.get_json()
    print(f"Received event: {payload.get('event')}")
    return ("", 204)
```

#### Verification Example (Go)

```go
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
)

func verifyWebhookSignature(secret []byte, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, false
	}
	sigHeader := r.Header.Get("X-Webhook-Signature")
	if !strings.HasPrefix(sigHeader, "sha256=") {
		return nil, false
	}
	gotHex := strings.TrimPrefix(sigHeader, "sha256=")
	gotSig, err := hex.DecodeString(gotHex)
	if err != nil {
		return nil, false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	expectedSig := mac.Sum(nil)
	return body, hmac.Equal(gotSig, expectedSig)
}
```

---

## Delivery Failures & Auditability

1. **Independent Fan-Out:** When a rule matches, alerts are dispatched to all assigned channels concurrently. A delivery error on one channel (such as a timeout or HTTP 500) never cancels or impedes delivery to other channels.
2. **Failure Logging:** If a channel fails to deliver, SnatchArr records an event of type `notification_failed` with level `warn`. This event includes the channel name and the sanitized error message.
3. **Secret Redaction:** All error messages are sanitized before being stored in database events or written to application logs: webhook secrets, bot tokens, passwords, and sensitive URLs are stripped.
