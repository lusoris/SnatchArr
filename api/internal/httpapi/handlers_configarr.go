// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/configarr"
)

// ImportConfigarr runs a one-shot import (or re-imports the linked configuration).
func (h *Handlers) ImportConfigarr(ctx context.Context, req oas.OptConfigarrImportRequest) (*oas.ConfigarrImportResult, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	var configPath, secretsPath string
	if body, ok := req.Get(); ok {
		configPath, secretsPath = body.ConfigPath.Or(""), body.SecretsPath.Or("")
	}
	res, err := h.configarr.Import(ctx, configPath, secretsPath)
	if err != nil {
		return nil, err
	}
	out := configarrResultToOAS(res)
	return &out, nil
}

// GetConfigarrStatus reports linked mode and the last import.
func (h *Handlers) GetConfigarrStatus(ctx context.Context) (*oas.ConfigarrStatus, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	st := h.configarr.Status()
	out := &oas.ConfigarrStatus{
		Linked: st.Linked, ConfigPath: optString(st.ConfigPath), SecretsPath: optString(st.SecretsPath), Watching: st.Watching,
		LastImportAt: optTime(st.LastImportAt), LastError: optString(st.LastError),
	}
	if st.LastResult != nil {
		out.LastResult = oas.NewOptConfigarrImportResult(configarrResultToOAS(*st.LastResult))
	}
	return out, nil
}

func configarrResultToOAS(r configarr.Result) oas.ConfigarrImportResult {
	return oas.ConfigarrImportResult{Imported: r.Imported, Updated: r.Updated, Disabled: r.Disabled, Warnings: emptyIfNil(r.Warnings), At: r.At}
}
