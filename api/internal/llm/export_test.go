// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package llm

// ToolSpecForTest exposes toolSpec for tests in package llm_test.
type ToolSpecForTest = toolSpec

// RegisteredToolsForTest returns the registered tools slice for tests.
func RegisteredToolsForTest() []toolSpec {
	return toolRegistry
}
