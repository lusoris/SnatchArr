// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient

import (
	"fmt"
	"time"

	"github.com/golusoris/goenvoy/arr/v2"
)

// BaseFactory builds goenvoy base clients with one timeout and user agent.
type BaseFactory struct {
	Timeout   time.Duration
	UserAgent string
}

// Options converts the factory settings to goenvoy arr.Option values.
func (f BaseFactory) Options() []arr.Option {
	return []arr.Option{arr.WithTimeout(f.Timeout), arr.WithUserAgent(f.UserAgent)}
}

// Base builds a goenvoy BaseClient configured with the factory's timeout and user agent.
func (f BaseFactory) Base(baseURL, apiKey string) (*arr.BaseClient, error) {
	c, err := arr.NewBaseClient(baseURL, apiKey, f.Options()...)
	if err != nil {
		return nil, fmt.Errorf("base client: %w", err)
	}
	return c, nil
}
