// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

/**
 * Validates that each `.target` in the given root contains exactly one
 * `a.target-link[href]`, has no nested `.target` elements, and has no invalid
 * `.target-link` elements.
 *
 * @param root DOM subtree to validate.
 * @returns Array of human-readable problem descriptions (empty if valid).
 */
export function targetProblems(root: ParentNode): string[] {
	const problems: string[] = [];
	const targets: Element[] = [];

	if (typeof Element !== 'undefined' && root instanceof Element && root.matches('.target')) {
		targets.push(root);
	}
	targets.push(...Array.from(root.querySelectorAll('.target')));

	for (const target of targets) {
		const nested = target.querySelectorAll('.target');
		if (nested.length > 0) {
			problems.push(
				`Nested .target elements are prohibited: found ${nested.length} nested target(s)`
			);
		}

		const allTargetLinks = Array.from(target.querySelectorAll('.target-link')).filter(
			(el) => el.closest('.target') === target
		);

		const validLinks = allTargetLinks.filter(
			(el): el is HTMLAnchorElement => el.tagName === 'A' && el.hasAttribute('href')
		);

		if (validLinks.length === 0) {
			problems.push('Target element must contain exactly one a.target-link[href], found 0');
		} else if (validLinks.length > 1) {
			problems.push(
				`Target element must contain exactly one a.target-link[href], found ${validLinks.length}`
			);
		}
	}

	return problems;
}
