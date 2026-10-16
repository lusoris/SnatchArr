// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient_test

import (
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
)

func TestProwlarrIndexerID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		baseURL     string
		prowlarrURL string
		wantID      int
		wantOK      bool
	}{
		{
			name:        "positive id 1 standard slash",
			baseURL:     "http://prowlarr:9696/1/",
			prowlarrURL: "http://prowlarr:9696",
			wantID:      1,
			wantOK:      true,
		},
		{
			name:        "positive id 1 without trailing slash",
			baseURL:     "http://prowlarr:9696/1",
			prowlarrURL: "http://prowlarr:9696",
			wantID:      1,
			wantOK:      true,
		},
		{
			name:        "positive id 1 trailing /api",
			baseURL:     "http://prowlarr:9696/1/api",
			prowlarrURL: "http://prowlarr:9696",
			wantID:      1,
			wantOK:      true,
		},
		{
			name:        "positive id 1 trailing /api/",
			baseURL:     "http://prowlarr:9696/1/api/",
			prowlarrURL: "http://prowlarr:9696",
			wantID:      1,
			wantOK:      true,
		},
		{
			name:        "positive prowlarrURL with trailing slash",
			baseURL:     "http://prowlarr:9696/42/",
			prowlarrURL: "http://prowlarr:9696/",
			wantID:      42,
			wantOK:      true,
		},
		{
			name:        "positive prowlarrURL with subpath",
			baseURL:     "http://prowlarr:9696/prowlarr/10/",
			prowlarrURL: "http://prowlarr:9696/prowlarr",
			wantID:      10,
			wantOK:      true,
		},
		{
			name:        "positive prowlarrURL with subpath and trailing /api/",
			baseURL:     "http://prowlarr:9696/prowlarr/10/api/",
			prowlarrURL: "http://prowlarr:9696/prowlarr/",
			wantID:      10,
			wantOK:      true,
		},
		{
			name:        "boundary id 999 max 3 digits allowed",
			baseURL:     "http://prowlarr:9696/999/",
			prowlarrURL: "http://prowlarr:9696",
			wantID:      999,
			wantOK:      true,
		},
		{
			name:        "boundary id 1000 refused 4 digits",
			baseURL:     "http://prowlarr:9696/1000/",
			prowlarrURL: "http://prowlarr:9696",
			wantID:      0,
			wantOK:      false,
		},
		{
			name:        "boundary id 0 refused non-positive",
			baseURL:     "http://prowlarr:9696/0/",
			prowlarrURL: "http://prowlarr:9696",
			wantID:      0,
			wantOK:      false,
		},
		{
			name:        "negative other host refused",
			baseURL:     "http://other-host:9696/1/",
			prowlarrURL: "http://prowlarr:9696",
			wantID:      0,
			wantOK:      false,
		},
		{
			name:        "negative other port refused",
			baseURL:     "http://prowlarr:8989/1/",
			prowlarrURL: "http://prowlarr:9696",
			wantID:      0,
			wantOK:      false,
		},
		{
			name:        "negative other scheme refused",
			baseURL:     "https://prowlarr:9696/1/",
			prowlarrURL: "http://prowlarr:9696",
			wantID:      0,
			wantOK:      false,
		},
		{
			name:        "negative non-numeric indexer path refused",
			baseURL:     "http://prowlarr:9696/torznab/",
			prowlarrURL: "http://prowlarr:9696",
			wantID:      0,
			wantOK:      false,
		},
		{
			name:        "negative subpath mismatch refused",
			baseURL:     "http://prowlarr:9696/other/1/",
			prowlarrURL: "http://prowlarr:9696/prowlarr",
			wantID:      0,
			wantOK:      false,
		},
		{
			name:        "boundary empty baseURL",
			baseURL:     "",
			prowlarrURL: "http://prowlarr:9696",
			wantID:      0,
			wantOK:      false,
		},
		{
			name:        "boundary empty prowlarrURL",
			baseURL:     "http://prowlarr:9696/1/",
			prowlarrURL: "",
			wantID:      0,
			wantOK:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotID, gotOK := arrclient.ProwlarrIndexerID(tc.baseURL, tc.prowlarrURL)
			if gotOK != tc.wantOK {
				t.Fatalf("ProwlarrIndexerID() ok = %v, wantOK %v", gotOK, tc.wantOK)
			}
			if gotID != tc.wantID {
				t.Errorf("ProwlarrIndexerID() id = %d, wantID %d", gotID, tc.wantID)
			}
		})
	}
}
