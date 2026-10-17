// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//go:build race

package workergrpc

// raceDetector reports whether the tests run with the race detector (see skipLatencyUnderRace).
const raceDetector = true
