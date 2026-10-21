// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

export class MockEventSource {
	static instances: MockEventSource[] = [];
	url: string;
	init?: { withCredentials?: boolean };
	private listeners: Record<string, ((event: unknown) => void)[]> = {};

	constructor(url: string, init?: { withCredentials?: boolean }) {
		this.url = url;
		this.init = init;
		MockEventSource.instances.push(this);
	}

	addEventListener(type: string, handler: (event: unknown) => void) {
		this.listeners[type] = this.listeners[type] ?? [];
		this.listeners[type].push(handler);
	}

	removeEventListener(type: string, handler: (event: unknown) => void) {
		this.listeners[type] = (this.listeners[type] ?? []).filter((h) => h !== handler);
	}

	close() {}

	emit(type: string, event: unknown) {
		for (const handler of [...(this.listeners[type] ?? [])]) {
			handler(event);
		}
	}
}

if (typeof globalThis !== 'undefined') {
	(globalThis as unknown as { EventSource: unknown }).EventSource = MockEventSource;
}
