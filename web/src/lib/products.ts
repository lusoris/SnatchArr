// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import type { components } from './api/schema.js';
import { m } from './paraglide/messages.js';

export type ArrProduct = components['schemas']['ArrProduct'];
type ProbeResult = components['schemas']['ProbeResult'];

// Every product the probe can name, and its name in the UI. `unknown` has none: the probe
// could not tell what answered. A product added to the OpenAPI enumeration fails the type
// check here until it is listed.
const names = {
	readarr: () => 'Readarr',
	bookshelf: () => 'Bookshelf',
	chaptarr: () => 'Chaptarr',
	readarr_faustvii: () => m.instance_product_readarr_faustvii(),
	unknown: undefined
} as const satisfies Record<ArrProduct, (() => string) | undefined>;

/** The name of a product the probe named, or undefined for `unknown` and for any other value. */
export function productName(product: string): string | undefined {
	return Object.hasOwn(names, product) ? names[product as ArrProduct]?.() : undefined;
}

/**
 * What the UI says after a probe answered. A kind that one product serves carries no product
 * and is named by the application itself. A named product replaces the application's name,
 * because most Readarr forks still report `Readarr`.
 */
export function probeMessage(result: ProbeResult | undefined): string {
	const app = result?.app_name ?? '';
	const version = result?.version ?? '';
	if (result?.product === undefined) return m.instance_probe_ok({ app, version });
	const name = productName(result.product);
	if (name === undefined) return m.instance_probe_unknown({ app, version });
	return m.instance_probe_ok({ app: name, version });
}
