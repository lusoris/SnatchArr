<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<th>Actions</th>
<button type="button" aria-label="Delete">✕</button>
<input placeholder="Enter text" />
<img src="/logo.png" alt="Company Logo" />
<p>{ok ? 'Saved' : 'Failed'}</p>
<button type="button" title={busy ? 'Working' : m.instance_save()}>✕</button>
<p>{`${count} items`}</p>
<p>{name || 'Hello' + ' there'}</p>
{#if shown}
	<p>{ready ? 'Nested' : m.instance_save()}</p>
{/if}
{#await load then value}
	<p>{value ?? 'Awaited'}</p>
{/await}
