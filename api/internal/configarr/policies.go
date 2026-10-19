// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package configarr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/policyoas"
)

// PolicySection holds the raw YAML/JSON fields for snatch and queue policies.
type PolicySection struct {
	Snatch        map[string]any
	Queue         map[string]any
	SnatchInvalid bool
	QueueInvalid  bool
}

// ParsedPolicies holds the parsed declarative policy file.
type ParsedPolicies struct {
	Defaults  PolicySection
	Instances map[string]PolicySection
	Warnings  []string
}

// ForInstance returns the policy section for the given instance key, merging defaults first.
func (p ParsedPolicies) ForInstance(key string) PolicySection {
	if inst, ok := p.Instances[key]; ok {
		return inst
	}
	return p.Defaults
}

var errEmptyPolicies = errors.New("empty policies document")

func newPoliciesParser(path, secretsPath string, lookupEnv func(string) (string, bool)) (*Parser, error) {
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	p := &Parser{secrets: map[string]string{}, lookupEnv: lookupEnv, baseDir: filepath.Dir(path)}
	if secretsPath != "" {
		if err := p.loadSecrets(secretsPath); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func readPoliciesDoc(path, secretsPath string, lookupEnv func(string) (string, bool)) (*yaml.Node, error) {
	p, err := newPoliciesParser(path, secretsPath, lookupEnv)
	if err != nil {
		return nil, err
	}
	raw, err := readBounded(path, maxYAMLBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrParse, err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, errEmptyPolicies
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrParse, filepath.Base(path), err)
	}
	root := documentRoot(&doc)
	if root == nil || root.Kind == 0 {
		return nil, errEmptyPolicies
	}
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%w: %s: top level must be a mapping", ErrParse, filepath.Base(path))
	}
	if err := resolveCredentials(p, root, filepath.Base(path)); err != nil {
		return nil, err
	}
	return root, nil
}

// ParsePolicies reads the policies YAML file and resolves credentials.
func ParsePolicies(path, secretsPath string, lookupEnv func(string) (string, bool)) (ParsedPolicies, error) {
	root, err := readPoliciesDoc(path, secretsPath, lookupEnv)
	if err != nil {
		if errors.Is(err, errEmptyPolicies) {
			return ParsedPolicies{Instances: map[string]PolicySection{}}, nil
		}
		return ParsedPolicies{}, err
	}
	return parsePoliciesRoot(root, filepath.Base(path))
}

type nodeItem struct {
	path string
	node *yaml.Node
}

func pushMappingChildren(curr nodeItem, stack *[]nodeItem) {
	for _, kv := range mappingEntries(curr.node) {
		*stack = append(*stack, nodeItem{path: curr.path + "." + kv[0].Value, node: kv[1]})
	}
}

func pushSequenceChildren(curr nodeItem, stack *[]nodeItem) {
	for i, item := range curr.node.Content {
		*stack = append(*stack, nodeItem{path: fmt.Sprintf("%s[%d]", curr.path, i), node: item})
	}
}

func processOneNode(p *Parser, curr nodeItem, stack *[]nodeItem) error {
	n := curr.node
	if n == nil {
		return nil
	}
	if n.Kind == yaml.ScalarNode {
		return resolveScalarNode(p, curr.path, n)
	}
	if n.Kind == yaml.MappingNode {
		pushMappingChildren(curr, stack)
		return nil
	}
	if n.Kind == yaml.SequenceNode {
		pushSequenceChildren(curr, stack)
	}
	return nil
}

func resolveCredentials(p *Parser, root *yaml.Node, rootPath string) error {
	if root == nil {
		return nil
	}
	stack := []nodeItem{{path: rootPath, node: root}}
	const maxNodes = 100000
	for step := 0; step < maxNodes && len(stack) > 0; step++ {
		curr := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if err := processOneNode(p, curr, &stack); err != nil {
			return err
		}
	}
	return nil
}

func resolveScalarNode(p *Parser, path string, n *yaml.Node) error {
	if strings.HasPrefix(n.Tag, "!") && !strings.HasPrefix(n.Tag, "!!") {
		v, err := p.resolve(path, n)
		if err != nil {
			return err
		}
		n.Value = v
		setResolvedScalarTag(n, v)
	}
	return nil
}

func setResolvedScalarTag(n *yaml.Node, v string) {
	switch {
	case isInt(v):
		n.Tag = "!!int"
	case isBool(v):
		n.Tag = "!!bool"
	default:
		n.Tag = "!!str"
	}
}

func isInt(v string) bool {
	_, err := strconv.Atoi(strings.TrimSpace(v))
	return err == nil
}

func isBool(v string) bool {
	_, err := strconv.ParseBool(strings.TrimSpace(v))
	return err == nil
}

func parsePoliciesRoot(root *yaml.Node, fileBase string) (ParsedPolicies, error) {
	out := ParsedPolicies{Instances: map[string]PolicySection{}}
	for _, kv := range mappingEntries(root) {
		key := kv[0].Value
		switch key {
		case "defaults":
			if kv[1].Kind != yaml.MappingNode {
				return ParsedPolicies{}, fmt.Errorf("%w: %s: defaults must be a mapping", ErrParse, fileBase)
			}
			out.Defaults = parsePolicySection("defaults", kv[1], fileBase, &out.Warnings)
		case "instances":
			if kv[1].Kind != yaml.MappingNode {
				return ParsedPolicies{}, fmt.Errorf("%w: %s: instances must be a mapping", ErrParse, fileBase)
			}
			parseInstances(kv[1], out.Defaults, fileBase, &out)
		default:
			out.Warnings = append(out.Warnings, fmt.Sprintf("%s: unknown key %q", fileBase, key))
		}
	}
	return out, nil
}

func parseInstances(instancesNode *yaml.Node, defaults PolicySection, fileBase string, out *ParsedPolicies) {
	for _, kv := range mappingEntries(instancesNode) {
		instKey := kv[0].Value
		if kv[1].Kind != yaml.MappingNode {
			out.Warnings = append(out.Warnings, fmt.Sprintf("%s: %s: instance must be a mapping", fileBase, instKey))
			continue
		}
		rawSection := parsePolicySection(instKey, kv[1], fileBase, &out.Warnings)
		merged := PolicySection{
			Snatch:        deepMergeMap(defaults.Snatch, rawSection.Snatch),
			Queue:         deepMergeMap(defaults.Queue, rawSection.Queue),
			SnatchInvalid: rawSection.SnatchInvalid || defaults.SnatchInvalid,
			QueueInvalid:  rawSection.QueueInvalid || defaults.QueueInvalid,
		}
		out.Instances[instKey] = merged
	}
}

func parsePolicySection(key string, node *yaml.Node, fileBase string, warnings *[]string) PolicySection {
	var sec PolicySection
	for _, kv := range mappingEntries(node) {
		switch kv[0].Value {
		case "snatch":
			sec.Snatch, sec.SnatchInvalid = parseSnatchSection(key, kv[1], fileBase, warnings)
		case "queue":
			sec.Queue, sec.QueueInvalid = parseQueueSection(key, kv[1], fileBase, warnings)
		default:
			*warnings = append(*warnings, fmt.Sprintf("%s: %s: unknown field %q", fileBase, key, kv[0].Value))
		}
	}
	return sec
}

func parseSnatchSection(key string, node *yaml.Node, fileBase string, warnings *[]string) (map[string]any, bool) {
	if node.Kind != yaml.MappingNode {
		*warnings = append(*warnings, fmt.Sprintf("%s: %s: snatch must be a mapping", fileBase, key))
		return nil, true
	}
	var res map[string]any
	if err := node.Decode(&res); err != nil {
		*warnings = append(*warnings, fmt.Sprintf("%s: %s: snatch decode: %v", fileBase, key, err))
		return nil, true
	}
	return res, false
}

func parseQueueSection(key string, node *yaml.Node, fileBase string, warnings *[]string) (map[string]any, bool) {
	if node.Kind != yaml.MappingNode {
		*warnings = append(*warnings, fmt.Sprintf("%s: %s: queue must be a mapping", fileBase, key))
		return nil, true
	}
	var res map[string]any
	if err := node.Decode(&res); err != nil {
		*warnings = append(*warnings, fmt.Sprintf("%s: %s: queue decode: %v", fileBase, key, err))
		return nil, true
	}
	return res, false
}

type diffTask struct {
	pathPrefix  string
	merged      map[string]any
	remarshaled map[string]any
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func checkDiffKey(curr diffTask, k string, unknown *[]string, queue *[]diffTask) {
	fullPath := k
	if curr.pathPrefix != "" {
		fullPath = curr.pathPrefix + k
	}
	remarshaledVal, exists := curr.remarshaled[k]
	if !exists {
		*unknown = append(*unknown, fullPath)
		return
	}
	mergedSub, ok1 := curr.merged[k].(map[string]any)
	remarshaledSub, ok2 := remarshaledVal.(map[string]any)
	if ok1 && ok2 {
		*queue = append(*queue, diffTask{
			pathPrefix:  fullPath + ".",
			merged:      mergedSub,
			remarshaled: remarshaledSub,
		})
	}
}

func findUnknownFields(merged, remarshaled map[string]any) []string {
	var unknown []string
	queue := []diffTask{{pathPrefix: "", merged: merged, remarshaled: remarshaled}}
	const maxWalkSteps = 10000
	for step := 0; step < maxWalkSteps && len(queue) > 0; step++ {
		curr := queue[0]
		queue = queue[1:]

		for _, k := range sortedKeys(curr.merged) {
			checkDiffKey(curr, k, &unknown, &queue)
		}
	}
	return unknown
}

type mergeTask struct {
	dest map[string]any
	src  map[string]any
}

func deepMergeMap(base, overlay map[string]any) map[string]any {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	out := make(map[string]any, len(base)+len(overlay))
	maps.Copy(out, base)

	queue := []mergeTask{{dest: out, src: overlay}}
	const maxMergeSteps = 1000
	for step := 0; step < maxMergeSteps && len(queue) > 0; step++ {
		curr := queue[0]
		queue = queue[1:]

		for k, v := range curr.src {
			baseVal, exists := curr.dest[k]
			if exists {
				baseMap, baseIsMap := baseVal.(map[string]any)
				overlayMap, overlayIsMap := v.(map[string]any)
				if baseIsMap && overlayIsMap {
					mergedSub := make(map[string]any, len(baseMap)+len(overlayMap))
					maps.Copy(mergedSub, baseMap)
					curr.dest[k] = mergedSub
					queue = append(queue, mergeTask{dest: mergedSub, src: overlayMap})
					continue
				}
			}
			curr.dest[k] = v
		}
	}
	return out
}

func (s *Service) importPolicies(ctx context.Context, secretsPath string, res *Result) error {
	parsed, err := ParsePolicies(s.cfg.Policies, secretsPath, os.LookupEnv)
	if err != nil {
		if errors.Is(err, ErrParse) {
			return fmt.Errorf("%w: %w", domain.ErrInvalid, err)
		}
		return err
	}
	res.Warnings = append(res.Warnings, parsed.Warnings...)
	fileBase := filepath.Base(s.cfg.Policies)
	return s.applyPoliciesToInstances(ctx, fileBase, parsed, res)
}

func filterConfigarrInstances(list []domain.Instance) map[string]domain.Instance {
	res := make(map[string]domain.Instance)
	for _, inst := range list {
		if inst.Source == domain.SourceConfigarr {
			res[inst.ConfigarrKey] = inst
		}
	}
	return res
}

func (s *Service) applyOneInstancePolicies(ctx context.Context, fileBase string, inst domain.Instance, sec PolicySection, res *Result) {
	if len(sec.Snatch) > 0 && !sec.SnatchInvalid {
		s.applySnatchPolicy(ctx, fileBase, inst, sec.Snatch, res)
	}
	if len(sec.Queue) > 0 && !sec.QueueInvalid {
		s.applyQueuePolicy(ctx, fileBase, inst, sec.Queue, res)
	}
}

func (s *Service) applyPoliciesToInstances(ctx context.Context, fileBase string, parsed ParsedPolicies, res *Result) error {
	list, err := s.instances.List(ctx)
	if err != nil {
		return err
	}
	configarrInsts := filterConfigarrInstances(list)
	for instKey := range parsed.Instances {
		if _, ok := configarrInsts[instKey]; !ok {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s: unknown instance", fileBase, instKey))
		}
	}
	for instKey, inst := range configarrInsts {
		s.applyOneInstancePolicies(ctx, fileBase, inst, parsed.ForInstance(instKey), res)
	}
	return nil
}

func (s *Service) applySnatchPolicy(ctx context.Context, fileBase string, inst domain.Instance, patch map[string]any, res *Result) {
	cur, err := s.policies.Get(ctx, inst.ID)
	if err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s: snatch: %v", fileBase, inst.ConfigarrKey, err))
		return
	}
	curOAS := policyoas.PolicyToOAS(cur)
	curJSON, err := json.Marshal(curOAS)
	if err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s: snatch: %v", fileBase, inst.ConfigarrKey, err))
		return
	}
	merged := decodeAndMerge(curJSON, patch)
	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s: snatch: %v", fileBase, inst.ConfigarrKey, err))
		return
	}
	var oasPolicy oas.SnatchPolicy
	if err = oasPolicy.UnmarshalJSON(mergedJSON); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s: snatch: %v", fileBase, inst.ConfigarrKey, err))
		return
	}
	remarshaledJSON, mErr := oasPolicy.MarshalJSON()
	if mErr != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s: snatch: %v", fileBase, inst.ConfigarrKey, mErr))
		return
	}
	var remarshaledMap map[string]any
	if uErr := json.Unmarshal(remarshaledJSON, &remarshaledMap); uErr != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s: snatch: %v", fileBase, inst.ConfigarrKey, uErr))
		return
	}
	unknown := findUnknownFields(merged, remarshaledMap)
	if len(unknown) > 0 {
		for _, u := range unknown {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s: unknown field %q", fileBase, inst.ConfigarrKey, u))
		}
		return
	}
	domainPolicy := policyoas.PolicyFromOAS(inst.ID, &oasPolicy)
	if _, err = s.policies.Update(ctx, domainPolicy); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s: snatch: %v", fileBase, inst.ConfigarrKey, err))
	}
}

func (s *Service) applyQueuePolicy(ctx context.Context, fileBase string, inst domain.Instance, patch map[string]any, res *Result) {
	cur, err := s.queue.Settings(ctx, inst.ID)
	if err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s: queue: %v", fileBase, inst.ConfigarrKey, err))
		return
	}
	curOAS := policyoas.QueuePolicyToOAS(cur)
	curJSON, err := json.Marshal(curOAS)
	if err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s: queue: %v", fileBase, inst.ConfigarrKey, err))
		return
	}
	merged := decodeAndMerge(curJSON, patch)
	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s: queue: %v", fileBase, inst.ConfigarrKey, err))
		return
	}
	var oasPolicy oas.QueuePolicy
	if err = oasPolicy.UnmarshalJSON(mergedJSON); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s: queue: %v", fileBase, inst.ConfigarrKey, err))
		return
	}
	remarshaledJSON, mErr := oasPolicy.MarshalJSON()
	if mErr != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s: queue: %v", fileBase, inst.ConfigarrKey, mErr))
		return
	}
	var remarshaledMap map[string]any
	if uErr := json.Unmarshal(remarshaledJSON, &remarshaledMap); uErr != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s: queue: %v", fileBase, inst.ConfigarrKey, uErr))
		return
	}
	unknown := findUnknownFields(merged, remarshaledMap)
	if len(unknown) > 0 {
		for _, u := range unknown {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s: unknown field %q", fileBase, inst.ConfigarrKey, u))
		}
		return
	}
	domainSettings := policyoas.QueuePolicyFromOAS(&oasPolicy)
	if err = s.queue.SaveSettings(ctx, inst.ID, domainSettings); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s: queue: %v", fileBase, inst.ConfigarrKey, err))
	}
}

func decodeAndMerge(curJSON []byte, patch map[string]any) map[string]any {
	var curMap map[string]any
	d := json.NewDecoder(bytes.NewReader(curJSON))
	d.UseNumber()
	if err := d.Decode(&curMap); err != nil {
		return patch
	}
	return deepMergeMap(curMap, patch)
}
