// @vitest-environment jsdom
// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { targetProblems } from './targets.js';

describe('targetProblems DOM validation', () => {
	it('positive: reports 0 problems for a target with exactly one a.target-link[href]', () => {
		const container = document.createElement('div');
		container.innerHTML = `
			<div class="card target">
				<a class="target-link" href="/instances/inst-1">Sonarr</a>
				<button type="button" class="btn">Delete</button>
			</div>
		`;
		expect(targetProblems(container)).toEqual([]);
	});

	it('positive: reports 0 problems when passed a target element directly as root', () => {
		const target = document.createElement('div');
		target.className = 'card target';
		target.innerHTML = '<a class="target-link" href="/instances/inst-1">Sonarr</a>';
		expect(targetProblems(target)).toEqual([]);
	});

	it('negative: reports problem for a target without target-link', () => {
		const container = document.createElement('tbody');
		container.innerHTML = `
			<tr class="target">
				<td>No link here</td>
			</tr>
		`;
		const problems = targetProblems(container);
		expect(problems.length).toBe(1);
		expect(problems[0]).toContain('found 0');
	});

	it('negative: reports problem for an anchor target-link missing href', () => {
		const container = document.createElement('div');
		container.innerHTML = `
			<div class="card target">
				<a class="target-link">Link without href</a>
			</div>
		`;
		const problems = targetProblems(container);
		expect(problems.length).toBe(1);
		expect(problems[0]).toContain('found 0');
	});

	it('boundary: reports problem for a target containing two target-links', () => {
		const container = document.createElement('div');
		container.innerHTML = `
			<div class="card target">
				<a class="target-link" href="/instances/1">Instance 1</a>
				<a class="target-link" href="/instances/2">Instance 2</a>
			</div>
		`;
		const problems = targetProblems(container);
		expect(problems.length).toBe(1);
		expect(problems[0]).toContain('found 2');
	});

	it('positive: reports 0 problems for a target with button.target-link[type=button]', () => {
		const container = document.createElement('div');
		container.innerHTML = `
			<div class="card target">
				<button type="button" class="btn target-link">Edit</button>
				<button type="button" class="btn">Delete</button>
			</div>
		`;
		expect(targetProblems(container)).toEqual([]);
	});

	it('negative: reports problem for a disabled button.target-link', () => {
		const container = document.createElement('div');
		container.innerHTML = `
			<div class="card target">
				<button type="button" class="btn target-link" disabled>Edit</button>
			</div>
		`;
		const problems = targetProblems(container);
		expect(problems.length).toBe(1);
		expect(problems[0]).toContain('found 0');
	});

	it('negative: reports problem for a button.target-link without type=button', () => {
		const container = document.createElement('div');
		container.innerHTML = `
			<div class="card target">
				<button class="btn target-link">Edit</button>
			</div>
		`;
		const problems = targetProblems(container);
		expect(problems.length).toBe(1);
		expect(problems[0]).toContain('found 0');
	});

	it('boundary: reports problem for a target containing both an anchor and a button target-link', () => {
		const container = document.createElement('div');
		container.innerHTML = `
			<div class="card target">
				<a class="target-link" href="/instances/1">Instance 1</a>
				<button type="button" class="btn target-link">Edit</button>
			</div>
		`;
		const problems = targetProblems(container);
		expect(problems.length).toBe(1);
		expect(problems[0]).toContain('found 2');
	});

	it('boundary: reports problem for nested .target elements', () => {
		const container = document.createElement('div');
		container.innerHTML = `
			<div class="card target">
				<a class="target-link" href="/outer">Outer</a>
				<div class="card target">
					<a class="target-link" href="/inner">Inner</a>
				</div>
			</div>
		`;
		const problems = targetProblems(container);
		expect(problems.length).toBeGreaterThanOrEqual(1);
		expect(problems.some((p) => p.includes('Nested .target elements are prohibited'))).toBe(true);
	});

	it('component markup: row markup matching instances page has zero problems', () => {
		const container = document.createElement('tbody');
		container.innerHTML = `
			<tr class="target">
				<td>
					<div class="flex items-center gap-2">
						<a class="target-link underline" href="/instances/inst-1">Sonarr</a>
					</div>
				</td>
				<td>Series</td>
				<td>http://sonarr.local:8989</td>
				<td>manual</td>
				<td>Enabled</td>
				<td>OK</td>
				<td>
					<button type="button" class="btn">Delete</button>
				</td>
			</tr>
		`;
		expect(targetProblems(container)).toEqual([]);
	});

	it('component markup: card markup matching instances phone card list has zero problems', () => {
		const container = document.createElement('ul');
		container.innerHTML = `
			<li class="card target flex flex-col gap-2">
				<div class="flex items-start justify-between gap-2">
					<div class="flex flex-col gap-0.5">
						<div class="flex items-center gap-2">
							<a class="target-link font-semibold underline" href="/instances/inst-1">Sonarr</a>
						</div>
					</div>
				</div>
				<button type="button" class="btn">Delete</button>
			</li>
		`;
		expect(targetProblems(container)).toEqual([]);
	});
});
