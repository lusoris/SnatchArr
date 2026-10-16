// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package policies

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/instances"
)

// Options reads what an instance offers for the policy filters.
type Options struct {
	instances *instances.Service
	labels    arrclient.Labeler
}

// NewOptions wires the filter options reader.
func NewOptions(inst *instances.Service, labels arrclient.Labeler) *Options {
	return &Options{instances: inst, labels: labels}
}

// FilterOptions returns the tags and quality profiles of a stored instance.
func (o *Options) FilterOptions(ctx context.Context, instanceID uuid.UUID) (arrclient.Labels, error) {
	inst, err := o.instances.Get(ctx, instanceID)
	if err != nil {
		return arrclient.Labels{}, err
	}
	creds, err := o.instances.Credentials(ctx, instanceID)
	if err != nil {
		return arrclient.Labels{}, err
	}
	labels, err := o.labels.Labels(ctx, inst.Kind, creds.BaseURL, creds.APIKey)
	if err != nil {
		return arrclient.Labels{}, fmt.Errorf("policies: filter options: %w", err)
	}
	return labels, nil
}
