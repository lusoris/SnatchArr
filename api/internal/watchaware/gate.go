// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware

import (
	"encoding/json"
	"slices"

	"github.com/google/uuid"
)

const (
	// GateStateGated marks a series whose later seasons are held back by the gate.
	GateStateGated = "gated"
	// GateStateExemptTag marks a series exempted by its tag in Sonarr.
	GateStateExemptTag = "exempt_tag"
	// GateStateBeforeGate marks a series present in Sonarr before the gate was activated.
	GateStateBeforeGate = "before_gate"
	// GateStateFilesBeyondGate marks a series with files beyond the gate season.
	GateStateFilesBeyondGate = "files_beyond_gate"

	// GateActionUnmonitor unmonitors a season past the gate.
	GateActionUnmonitor = "unmonitor"
	// GateActionPromote promotes the next season to monitored once watched.
	GateActionPromote = "promote"

	// GateModeObserve records decisions without writing to Sonarr.
	GateModeObserve = "observe"
	// GateModeAct writes decisions to Sonarr.
	GateModeAct = "act"

	// DefaultExemptTag is the Sonarr tag label that exempts a series from the gate.
	DefaultExemptTag = "snatcharr-no-gate"
	// DefaultGateSeasonsOnAdd is the default number of seasons kept monitored on add.
	DefaultGateSeasonsOnAdd = 1
	// DefaultGateLookAhead is the default look-ahead distance for promotion.
	DefaultGateLookAhead = 1
	// DefaultGatePromoteWhose is the default watcher filter for promotion.
	DefaultGatePromoteWhose = "any"

	maxGateInstanceIDs  = 32
	maxSeasonsPerSeries = 200
)

// GateSeason holds monitoring status and file count of a season.
type GateSeason struct {
	Number    int
	Monitored bool
	FileCount int
}

// GateSeries holds metadata and seasons of a series for gate evaluation.
type GateSeries struct {
	ID      int
	TvdbID  int
	Title   string
	TagIDs  []int
	Seasons []GateSeason
}

// GateDecision represents an action the gate would take or has taken.
type GateDecision struct {
	SeriesID int
	Season   int
	Action   string
	WouldAct bool
	Reason   string
}

// GateConfig specifies season gate settings for the watchaware module.
type GateConfig struct {
	Enabled      bool
	Mode         string
	SeasonsOnAdd int
	LookAhead    int
	PromoteWhose string
	ExemptTag    string
	InstanceIDs  []uuid.UUID
}

// DefaultGateConfig returns a GateConfig populated with defaults (gate disabled).
func DefaultGateConfig() GateConfig {
	return GateConfig{
		Enabled:      false,
		Mode:         GateModeObserve,
		SeasonsOnAdd: DefaultGateSeasonsOnAdd,
		LookAhead:    DefaultGateLookAhead,
		PromoteWhose: DefaultGatePromoteWhose,
		ExemptTag:    DefaultExemptTag,
		InstanceIDs:  nil,
	}
}

// AppliesTo reports whether the gate configuration applies to the given instance.
func (c GateConfig) AppliesTo(instanceID uuid.UUID) bool {
	if len(c.InstanceIDs) == 0 {
		return true
	}
	return slices.Contains(c.InstanceIDs, instanceID)
}

type rawGateConfig struct {
	Gate *struct {
		Enabled      *bool    `json:"enabled"`
		Mode         *string  `json:"mode"`
		SeasonsOnAdd *int     `json:"seasons_on_add"`
		LookAhead    *int     `json:"look_ahead"`
		PromoteWhose *string  `json:"promote_whose"`
		ExemptTag    *string  `json:"exempt_tag"`
		InstanceIDs  []string `json:"instance_ids"`
	} `json:"gate"`
}

func parseGateInstanceIDs(raw []string) []uuid.UUID {
	if len(raw) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(raw))
	for _, idStr := range raw {
		if u, err := uuid.Parse(idStr); err == nil {
			ids = append(ids, u)
			if len(ids) == maxGateInstanceIDs {
				break
			}
		}
	}
	return ids
}

func applyGateModeAndEnabled(cfg *GateConfig, enabled *bool, mode *string) {
	if enabled != nil {
		cfg.Enabled = *enabled
	}
	if mode != nil && *mode != "" {
		cfg.Mode = *mode
	}
}

func applyGateSeasons(cfg *GateConfig, seasonsOnAdd *int, lookAhead *int) {
	if seasonsOnAdd != nil && *seasonsOnAdd >= 1 && *seasonsOnAdd <= 20 {
		cfg.SeasonsOnAdd = *seasonsOnAdd
	}
	if lookAhead != nil && *lookAhead >= 0 && *lookAhead <= 5 {
		cfg.LookAhead = *lookAhead
	}
}

func applyGateStrings(cfg *GateConfig, promoteWhose *string, exemptTag *string) {
	if promoteWhose != nil && (*promoteWhose == "any" || *promoteWhose == "all") {
		cfg.PromoteWhose = *promoteWhose
	}
	if exemptTag != nil && len(*exemptTag) >= 1 && len(*exemptTag) <= 64 {
		cfg.ExemptTag = *exemptTag
	}
}

// GateConfigFrom resolves a GateConfig from module configuration JSON, applying defaults.
// If the raw JSON is invalid, missing, or omits gate settings, defaults are returned with the gate disabled.
func GateConfigFrom(raw json.RawMessage) GateConfig {
	cfg := DefaultGateConfig()
	if len(raw) == 0 {
		return cfg
	}
	var root rawGateConfig
	if err := json.Unmarshal(raw, &root); err != nil || root.Gate == nil {
		return cfg
	}
	g := root.Gate
	applyGateModeAndEnabled(&cfg, g.Enabled, g.Mode)
	applyGateSeasons(&cfg, g.SeasonsOnAdd, g.LookAhead)
	applyGateStrings(&cfg, g.PromoteWhose, g.ExemptTag)
	cfg.InstanceIDs = parseGateInstanceIDs(g.InstanceIDs)
	return cfg
}

func hasFilesBeyondGate(seasons []GateSeason, seasonsOnAdd int) bool {
	for _, s := range seasons {
		if s.Number > seasonsOnAdd && s.FileCount > 0 {
			return true
		}
	}
	return false
}

func buildUnmonitorDecisions(seriesID int, seasons []GateSeason, seasonsOnAdd int) []GateDecision {
	var decisions []GateDecision
	for _, s := range seasons {
		if s.Number > seasonsOnAdd && s.Monitored {
			decisions = append(decisions, GateDecision{
				SeriesID: seriesID,
				Season:   s.Number,
				Action:   GateActionUnmonitor,
				WouldAct: false,
				Reason:   "season after gate",
			})
		}
	}
	return decisions
}

// EvaluateOnAdd evaluates season gate rules for a series newly added to Sonarr.
// It returns the gate state, the gate season (if gated), and unmonitor decisions.
func EvaluateOnAdd(
	s GateSeries,
	cfg GateConfig,
	exemptTagID int,
	hasExemptTag bool,
) (state string, gateSeason int, decisions []GateDecision) {
	if hasExemptTag && slices.Contains(s.TagIDs, exemptTagID) {
		return GateStateExemptTag, 0, nil
	}

	seasons := s.Seasons
	if len(seasons) > maxSeasonsPerSeries {
		seasons = seasons[:maxSeasonsPerSeries]
	}

	if hasFilesBeyondGate(seasons, cfg.SeasonsOnAdd) {
		return GateStateFilesBeyondGate, 0, nil
	}

	decisions = buildUnmonitorDecisions(s.ID, seasons, cfg.SeasonsOnAdd)
	return GateStateGated, cfg.SeasonsOnAdd, decisions
}
