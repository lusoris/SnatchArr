// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package settings_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestPriorityPresetConcurrentCapacity(t *testing.T) {
	t.Parallel()
	svc := newService(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for i := range domain.MaxPriorityPresets - 1 {
		_, err := svc.CreatePriorityPreset(ctx, domain.PriorityPresetInput{
			Name: fmt.Sprintf("preset-%d", i), Search: domain.MediaFirstPreset(), Download: domain.MediaFirstPreset(),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	results := make(chan error, 2)
	for i := range 2 {
		go func() {
			_, err := svc.CreatePriorityPreset(ctx, domain.PriorityPresetInput{
				Name: fmt.Sprintf("racing-%d", i), Search: domain.MediaFirstPreset(), Download: domain.MediaFirstPreset(),
			})
			results <- err
		}()
	}
	var accepted, refused int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, domain.ErrInvalid):
			refused++
		default:
			t.Fatalf("concurrent create: %v", err)
		}
	}
	presets, err := svc.ListPriorityPresets(ctx)
	if err != nil || accepted != 1 || refused != 1 || len(presets) != domain.MaxPriorityPresets+1 {
		t.Fatalf("accepted=%d refused=%d listed=%d err=%v; want 1/1/21", accepted, refused, len(presets), err)
	}
}
