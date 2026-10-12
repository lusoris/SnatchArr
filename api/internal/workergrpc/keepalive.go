// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"errors"
	"fmt"
	"time"

	gconfig "github.com/golusoris/golusoris/core/config"
	grpcx "github.com/golusoris/golusoris/grpc"
)

// graceKey is the configuration path of the server's connection-age grace.
const graceKey = "grpc.keepalive.max_connection_age_grace"

// minConnectionAgeGrace is the shortest finite grace the server accepts: one full LeaseRun
// long-poll plus its last database poll and the response.
const minConnectionAgeGrace = maxLeaseWait + 5*time.Second

var errGraceTooShort = errors.New("workergrpc: connection-age grace shorter than the LeaseRun long-poll")

// LeaseKeepalive keeps connection rotation from cutting in-flight RPCs. golusoris ages every
// connection out after max_connection_age (default 2m) and closes it max_connection_age_grace
// later (default 5s); that cut LeaseRun (up to maxLeaseWait) and the run-long ReportEvents
// stream. Unset, the grace becomes unlimited: rotation still moves new RPCs to a fresh
// connection, and the old one closes once its RPCs end; shutdown keeps its own bound. A finite
// grace shorter than minConnectionAgeGrace is refused at startup. It is an fx decorator for
// the app root, so it reaches the golusoris grpc module.
func LeaseKeepalive(c grpcx.Config, cfg *gconfig.Config) (grpcx.Config, error) {
	ka := c.Keepalive
	switch {
	case ka.MaxConnectionAge < 0 || ka.MaxConnectionAgeGrace < 0:
		return c, nil
	case !cfg.Exists(graceKey):
		c.Keepalive.MaxConnectionAgeGrace = grpcx.Infinite
		return c, nil
	case ka.MaxConnectionAgeGrace < minConnectionAgeGrace:
		return grpcx.Config{}, fmt.Errorf("%w: %s is %s; set at least %s, or a negative value for unlimited",
			errGraceTooShort, graceKey, ka.MaxConnectionAgeGrace, minConnectionAgeGrace)
	default:
		return c, nil
	}
}
