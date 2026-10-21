// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package llm

import (
	"log/slog"

	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	"github.com/lusoris/SnatchArr/api/internal/httpapi"
	"github.com/lusoris/SnatchArr/api/internal/settings"
)

// ToolSpecForTest exposes toolSpec for tests in package llm_test.
type ToolSpecForTest = toolSpec

// RegisteredToolsForTest returns the registered tools slice for tests.
func RegisteredToolsForTest() []toolSpec {
	return toolRegistry
}

// NewWithSpecsForTest constructs a Service using a custom toolSpec slice for testing error paths.
func NewWithSpecsForTest(build buildinfo.Info, settingsSvc *settings.Service, security *httpapi.Security, handlers *httpapi.Handlers, logger *slog.Logger, specs []toolSpec) (*Service, error) {
	return newWithSpecs(build, settingsSvc, security, handlers, logger, specs)
}
