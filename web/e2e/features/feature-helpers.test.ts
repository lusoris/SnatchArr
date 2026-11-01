// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #965

import http from 'node:http';
import { expect, test } from '@playwright/test';
import { listenLocal, listenStub } from './feature-helpers.js';

// Verify that listenStub binds an HTTP server, assigns an ephemeral port, and close resolves.
test('positive: real http server -> listenStub url matches pattern and close resolves', async () => {
	const server = http.createServer((_req, res) => {
		res.writeHead(200);
		res.end('ok');
	});
	const stub = await listenStub(server);
	try {
		expect(stub.url).toMatch(/^http:\/\/127\.0\.0\.1:\d+$/);
	} finally {
		await expect(stub.close()).resolves.toBeUndefined();
	}
});

// Verify that listenStub appends path without leading slash so that url ends with /graphql.
test('boundary: path "graphql" without leading slash produces url ending with "/graphql"', async () => {
	const server = http.createServer((_req, res) => {
		res.writeHead(200);
		res.end('ok');
	});
	const stub = await listenStub(server, 'graphql');
	try {
		expect(stub.url.endsWith('/graphql')).toBeTruthy();
	} finally {
		await stub.close();
	}
});

// Verify that listenLocal rejects when the server emits an error event on bind.
test('negative: replaced listen emitting error event causes listenLocal rejection', async () => {
	const server = http.createServer();
	const plantedError = new Error('planted bind failure');
	// Replace listen to emit error on next tick instead of binding socket.
	const fakeListen = function () {
		process.nextTick(() => {
			server.emit('error', plantedError);
		});
		return server;
	} as typeof server.listen;
	server.listen = fakeListen;

	try {
		await expect(listenLocal(server)).rejects.toThrow('planted bind failure');
	} finally {
		await new Promise<void>((resolve) => server.close(() => resolve()));
	}
});
