// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package config loads and validates settings under the "metadata_proxy" key tree.
package config

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.uber.org/fx"

	gconfig "github.com/golusoris/golusoris/core/config"
)

// Mode selects how hosts outside allow_hosts are handled after hard refusals.
type Mode string

// Operating modes.
const (
	ModeLog     Mode = "log"
	ModeEnforce Mode = "enforce"
)

// Default settings and environment paths.
const (
	DefaultConfigFile = "/etc/snatcharr/metadata-proxy.yaml"
	ConfigFileEnv     = "APP_METADATA_PROXY_CONFIG_FILE"

	DefaultPort              = 443
	DefaultConfigStatTimeout = 5 * time.Second
	DefaultDialTimeout       = 10 * time.Second
	DefaultIdleTimeout       = 2 * time.Minute
	DefaultMaxTunnel         = 30 * time.Minute
	DefaultMaxTunnels        = 256

	minPort              = 1
	maxPort              = 65535
	ipv4MappedPrefixBits = 96
)

// Validation sentinel errors.
var (
	ErrConfigFileChanged  = errors.New("config: explicit config file changed during load")
	ErrNoListeners        = errors.New("config: at least one listener is required")
	ErrBadAppName         = errors.New("config: invalid listener app name")
	ErrDuplicateApp       = errors.New("config: duplicate listener app")
	ErrDuplicatePort      = errors.New("config: duplicate listener port")
	ErrBadPort            = errors.New("config: port must be between 1 and 65535")
	ErrUnknownMode        = errors.New("config: mode must be log or enforce")
	ErrEmptyAllowHosts    = errors.New("config: enforce mode requires non-empty allow_hosts")
	ErrBadCIDR            = errors.New("config: invalid CIDR in denied_prefixes")
	ErrNonPositiveTimeout = errors.New("config: timeout must be greater than zero")
	ErrNonPositiveBound   = errors.New("config: max_tunnels must be greater than zero")
)

// Listener binds one CONNECT proxy port dedicated to one client application.
type Listener struct {
	App  string `koanf:"app"`
	Port int    `koanf:"port"`
}

// Options holds the unmarshalled metadata_proxy configuration.
type Options struct {
	Mode           Mode          `koanf:"mode"`
	Listeners      []Listener    `koanf:"listeners"`
	AllowHosts     []string      `koanf:"allow_hosts"`
	RefuseHosts    []string      `koanf:"refuse_hosts"`
	Ports          []int         `koanf:"ports"`
	DeniedPrefixes []string      `koanf:"denied_prefixes"`
	DialTimeout    time.Duration `koanf:"dial_timeout"`
	IdleTimeout    time.Duration `koanf:"idle_timeout"`
	MaxTunnel      time.Duration `koanf:"max_tunnel"`
	MaxTunnels     int           `koanf:"max_tunnels"`
}

// Compiled holds validated Options alongside pre-parsed matchers, ports, and prefixes.
type Compiled struct {
	Options       Options
	Apps          []string
	AllowMatcher  HostMatcher
	RefuseMatcher HostMatcher
	AllowedPorts  map[uint16]struct{}
	PortList      []uint16
	DeniedV4      []netip.Prefix
	DeniedV6      []netip.Prefix
}

// DefaultOptions returns Options populated with documented defaults.
func DefaultOptions() Options {
	return Options{
		Mode:        ModeLog,
		Ports:       []int{DefaultPort},
		DialTimeout: DefaultDialTimeout,
		IdleTimeout: DefaultIdleTimeout,
		MaxTunnel:   DefaultMaxTunnel,
		MaxTunnels:  DefaultMaxTunnels,
	}
}

// CompoundKeys lists koanf paths under metadata_proxy containing underscores.
func CompoundKeys() []string {
	return []string{
		"metadata_proxy.mode",
		"metadata_proxy.listeners",
		"metadata_proxy.allow_hosts",
		"metadata_proxy.refuse_hosts",
		"metadata_proxy.ports",
		"metadata_proxy.denied_prefixes",
		"metadata_proxy.dial_timeout",
		"metadata_proxy.idle_timeout",
		"metadata_proxy.max_tunnel",
		"metadata_proxy.max_tunnels",
	}
}

// CoreOptionsOverride allows callers to override golusoris core/config Options via fx.Supply.
type CoreOptionsOverride struct {
	Files        []string
	DisableEnv   bool
	DisableWatch bool
	StatTimeout  time.Duration
	StatFunc     func(string) (os.FileInfo, error)
}

type statFunc = func(string) (os.FileInfo, error)

// ExplicitFileCheck carries the decorate-time FileInfo of ConfigFileEnv through the fx graph
// so post-load verification can confirm the same unchanged file remained in place.
type ExplicitFileCheck struct {
	path    string
	before  os.FileInfo
	timeout time.Duration
	statFn  statFunc
}

// NewExplicitFileCheck constructs an empty ExplicitFileCheck for the fx graph.
func NewExplicitFileCheck() *ExplicitFileCheck {
	return &ExplicitFileCheck{}
}

// Verify re-stats any recorded explicit config file under its deadline and fails with
// ErrConfigFileChanged unless the same unchanged file is still in place.
func (c *ExplicitFileCheck) Verify() error {
	if c == nil || c.path == "" {
		return nil
	}
	timeout := c.timeout
	if timeout <= 0 {
		timeout = DefaultConfigStatTimeout
	}
	statFn := c.statFn
	if statFn == nil {
		statFn = defaultStatFile
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	after, err := statConfigFile(ctx, statFn, c.path)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrConfigFileChanged, err)
	}
	if !sameConfigFile(c.before, after) {
		return fmt.Errorf("%w: %s %q", ErrConfigFileChanged, ConfigFileEnv, c.path)
	}
	return nil
}

func sameConfigFile(before, after os.FileInfo) bool {
	return before != nil &&
		after != nil &&
		os.SameFile(before, after) &&
		before.ModTime().Equal(after.ModTime()) &&
		before.Size() == after.Size()
}

// CoreDecorateParams bundles inputs for fx.Decorate of golusoris core/config Options.
type CoreDecorateParams struct {
	fx.In

	Options  gconfig.Options
	Override *CoreOptionsOverride `optional:"true"`
	Check    *ExplicitFileCheck   `optional:"true"`
}

// DecorateCoreOptionsFX adapts DecorateCoreOptions for fx, applying any supplied CoreOptionsOverride.
func DecorateCoreOptionsFX(p CoreDecorateParams) (gconfig.Options, error) {
	o, timeout, statFn := applyCoreOverride(p.Options, p.Override)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	decorated, envPath, info, err := decorateCoreOptions(ctx, o, statFn)
	if err != nil {
		return gconfig.Options{}, err
	}
	if p.Check != nil {
		*p.Check = ExplicitFileCheck{
			path:    envPath,
			before:  info,
			timeout: timeout,
			statFn:  statFn,
		}
	}
	return decorated, nil
}

func applyCoreOverride(o gconfig.Options, ov *CoreOptionsOverride) (gconfig.Options, time.Duration, statFunc) {
	timeout := DefaultConfigStatTimeout
	statFn := defaultStatFile
	if ov == nil {
		return o, timeout, statFn
	}
	o.Files = ov.Files
	o.DisableEnv = ov.DisableEnv
	if ov.DisableWatch {
		o.Watch = false
	}
	if ov.StatTimeout > 0 {
		timeout = ov.StatTimeout
	}
	if ov.StatFunc != nil {
		statFn = ov.StatFunc
	}
	return o, timeout, statFn
}

// DecorateCoreOptions wires CompoundKeys and YAML file loading into golusoris core/config.
func DecorateCoreOptions(o gconfig.Options) (gconfig.Options, error) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultConfigStatTimeout)
	defer cancel()
	return DecorateCoreOptionsContext(ctx, o)
}

// DecorateCoreOptionsContext wires CompoundKeys and YAML file loading using a bounded stat context.
func DecorateCoreOptionsContext(ctx context.Context, o gconfig.Options) (gconfig.Options, error) {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultConfigStatTimeout)
		defer cancel()
	}
	decorated, _, _, err := decorateCoreOptions(ctx, o, defaultStatFile)
	return decorated, err
}

func decorateCoreOptions(
	ctx context.Context,
	o gconfig.Options,
	statFn statFunc,
) (gconfig.Options, string, os.FileInfo, error) {
	o.CompoundKeys = append(o.CompoundKeys, CompoundKeys()...)
	if len(o.Files) == 0 {
		o.Files = []string{DefaultConfigFile}
	}
	if o.DisableEnv {
		return o, "", nil, nil
	}
	envFile := strings.TrimSpace(os.Getenv(ConfigFileEnv))
	if envFile == "" {
		return o, "", nil, nil
	}
	info, err := statConfigFile(ctx, statFn, envFile)
	if err != nil {
		return gconfig.Options{}, "", nil, err
	}
	o.Files = append(o.Files, envFile)
	return o, envFile, info, nil
}

func defaultStatFile(path string) (os.FileInfo, error) {
	// #nosec G703 -- operator-supplied config path from APP_METADATA_PROXY_CONFIG_FILE
	return os.Stat(path)
}

type statResult struct {
	info os.FileInfo
	err  error
}

func statConfigFile(ctx context.Context, statFn statFunc, envFile string) (os.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("config: stat %s %q: %w", ConfigFileEnv, envFile, err)
	}
	resCh := make(chan statResult, 1)
	go func() {
		info, err := statFn(envFile)
		resCh <- statResult{info: info, err: err}
	}()
	select {
	case res := <-resCh:
		if res.err != nil {
			return nil, fmt.Errorf("config: stat %s %q: %w", ConfigFileEnv, envFile, res.err)
		}
		return res.info, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("config: stat %s %q: %w", ConfigFileEnv, envFile, ctx.Err())
	}
}

type scalarOptions struct {
	Mode           Mode          `koanf:"mode"`
	AllowHosts     []string      `koanf:"allow_hosts"`
	RefuseHosts    []string      `koanf:"refuse_hosts"`
	DeniedPrefixes []string      `koanf:"denied_prefixes"`
	DialTimeout    time.Duration `koanf:"dial_timeout"`
	IdleTimeout    time.Duration `koanf:"idle_timeout"`
	MaxTunnel      time.Duration `koanf:"max_tunnel"`
	MaxTunnels     int           `koanf:"max_tunnels"`
}

// Load reads metadata_proxy from cfg, applies defaults to unset fields, and compiles it.
func Load(cfg *gconfig.Config) (*Compiled, error) {
	defaults := DefaultOptions()
	scalars := scalarOptions{
		Mode:        defaults.Mode,
		DialTimeout: defaults.DialTimeout,
		IdleTimeout: defaults.IdleTimeout,
		MaxTunnel:   defaults.MaxTunnel,
		MaxTunnels:  defaults.MaxTunnels,
	}
	if err := cfg.Unmarshal("metadata_proxy", &scalars); err != nil {
		return nil, fmt.Errorf("config: load metadata_proxy: %w", err)
	}
	listeners, err := loadListeners(cfg)
	if err != nil {
		return nil, err
	}
	ports, err := loadPorts(cfg, defaults.Ports)
	if err != nil {
		return nil, err
	}
	opts := Options{
		Mode:           scalars.Mode,
		Listeners:      listeners,
		AllowHosts:     scalars.AllowHosts,
		RefuseHosts:    scalars.RefuseHosts,
		Ports:          ports,
		DeniedPrefixes: scalars.DeniedPrefixes,
		DialTimeout:    scalars.DialTimeout,
		IdleTimeout:    scalars.IdleTimeout,
		MaxTunnel:      scalars.MaxTunnel,
		MaxTunnels:     scalars.MaxTunnels,
	}
	return Compile(opts)
}

type rawListener struct {
	App  string `koanf:"app"`
	Port any    `koanf:"port"`
}

func loadListeners(cfg *gconfig.Config) ([]Listener, error) {
	raw, exists := cfg.All()["metadata_proxy.listeners"]
	if !exists {
		return nil, nil
	}
	if s, ok := raw.(string); ok {
		return parseEnvListeners(s)
	}
	var decoded []rawListener
	if err := cfg.Unmarshal("metadata_proxy.listeners", &decoded); err != nil {
		return nil, fmt.Errorf("config: decode metadata_proxy.listeners: %w", err)
	}
	out := make([]Listener, 0, len(decoded))
	for _, item := range decoded {
		port, err := parsePortValue(item.Port)
		if err != nil {
			return nil, fmt.Errorf("config: listener %q: %w", item.App, err)
		}
		out = append(out, Listener{App: item.App, Port: port})
	}
	return out, nil
}

func parseEnvListeners(raw string) ([]Listener, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	parts := strings.Split(trimmed, ",")
	out := make([]Listener, 0, len(parts))
	for _, item := range parts {
		appName, portStr, ok := strings.Cut(strings.TrimSpace(item), ":")
		if !ok {
			return nil, fmt.Errorf("%w: listener %q must have form app:port", ErrBadAppName, item)
		}
		port, err := portFromString(portStr)
		if err != nil {
			return nil, fmt.Errorf("listener %q: %w", item, err)
		}
		out = append(out, Listener{App: appName, Port: port})
	}
	return out, nil
}

func loadPorts(cfg *gconfig.Config, defaultPorts []int) ([]int, error) {
	raw, exists := cfg.All()["metadata_proxy.ports"]
	if !exists {
		return slices.Clone(defaultPorts), nil
	}
	switch v := raw.(type) {
	case string:
		return parseEnvPorts(v)
	case []int:
		return slices.Clone(v), nil
	case []any:
		return parsePortSlice(v)
	default:
		p, err := parsePortValue(raw)
		if err != nil {
			return nil, fmt.Errorf("config: decode metadata_proxy.ports: %w", err)
		}
		return []int{p}, nil
	}
}

func parseEnvPorts(raw string) ([]int, error) {
	if strings.TrimSpace(raw) == "" {
		return []int{}, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int, 0, len(parts))
	for _, item := range parts {
		port, err := portFromString(item)
		if err != nil {
			return nil, fmt.Errorf("config: decode metadata_proxy.ports: %w", err)
		}
		out = append(out, port)
	}
	return out, nil
}

func parsePortSlice(items []any) ([]int, error) {
	out := make([]int, 0, len(items))
	for _, item := range items {
		p, err := parsePortValue(item)
		if err != nil {
			return nil, fmt.Errorf("config: decode metadata_proxy.ports: %w", err)
		}
		out = append(out, p)
	}
	return out, nil
}

func parsePortValue(v any) (int, error) {
	switch x := v.(type) {
	case int:
		return x, nil
	case int64:
		return portFromInt64(x)
	case uint64:
		return portFromUint64(x)
	case float64:
		return portFromFloat64(x)
	case string:
		return portFromString(x)
	default:
		return parseNarrowPortValue(v)
	}
}

func parseNarrowPortValue(v any) (int, error) {
	switch x := v.(type) {
	case int8:
		return int(x), nil
	case int16:
		return int(x), nil
	case int32:
		return int(x), nil
	case uint8:
		return int(x), nil
	case uint16:
		return int(x), nil
	default:
		return parseWideUnsignedOrFloatPort(v)
	}
}

func parseWideUnsignedOrFloatPort(v any) (int, error) {
	switch x := v.(type) {
	case uint:
		return portFromUint64(uint64(x))
	case uint32:
		return portFromUint64(uint64(x))
	case float32:
		return portFromFloat64(float64(x))
	default:
		return 0, fmt.Errorf("%w: invalid port value %v", ErrBadPort, v)
	}
}

func portFromInt64(n int64) (int, error) {
	if n < int64(math.MinInt) || n > int64(math.MaxInt) {
		return 0, fmt.Errorf("%w: port %d", ErrBadPort, n)
	}
	return int(n), nil
}

func portFromUint64(n uint64) (int, error) {
	if n > uint64(math.MaxInt) {
		return 0, fmt.Errorf("%w: port %d", ErrBadPort, n)
	}
	return int(n), nil
}

func portFromFloat64(f float64) (int, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) || math.Trunc(f) != f {
		return 0, fmt.Errorf("%w: fractional port %v", ErrBadPort, f)
	}
	if f < float64(math.MinInt32) || f > float64(math.MaxInt32) {
		return 0, fmt.Errorf("%w: port %v", ErrBadPort, f)
	}
	return int(f), nil
}

func portFromString(raw string) (int, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("%w: empty port", ErrBadPort)
	}
	p, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%w: port %q: %w", ErrBadPort, raw, err)
	}
	return p, nil
}

// Validate checks every startup invariant of o.
func (o Options) Validate() error {
	_, err := Compile(o)
	return err
}

// Compile validates o and returns the compiled matchers, port set, and denied prefixes.
func Compile(o Options) (*Compiled, error) {
	if err := validateModeAndBounds(o); err != nil {
		return nil, err
	}
	apps, err := validateListeners(o.Listeners)
	if err != nil {
		return nil, err
	}
	allowMatcher, refuseMatcher, err := compileHostLists(o)
	if err != nil {
		return nil, err
	}
	allowedPorts, portList, err := compilePorts(o.Ports)
	if err != nil {
		return nil, err
	}
	v4, v6, err := compileDeniedPrefixes(o.DeniedPrefixes)
	if err != nil {
		return nil, err
	}
	return &Compiled{
		Options:       o,
		Apps:          apps,
		AllowMatcher:  allowMatcher,
		RefuseMatcher: refuseMatcher,
		AllowedPorts:  allowedPorts,
		PortList:      portList,
		DeniedV4:      v4,
		DeniedV6:      v6,
	}, nil
}

func validateModeAndBounds(o Options) error {
	switch o.Mode {
	case ModeLog, ModeEnforce:
	default:
		return fmt.Errorf("%w: got %q", ErrUnknownMode, o.Mode)
	}
	if o.DialTimeout <= 0 {
		return fmt.Errorf("%w: dial_timeout=%s", ErrNonPositiveTimeout, o.DialTimeout)
	}
	if o.IdleTimeout <= 0 {
		return fmt.Errorf("%w: idle_timeout=%s", ErrNonPositiveTimeout, o.IdleTimeout)
	}
	if o.MaxTunnel <= 0 {
		return fmt.Errorf("%w: max_tunnel=%s", ErrNonPositiveTimeout, o.MaxTunnel)
	}
	if o.MaxTunnels <= 0 {
		return fmt.Errorf("%w: max_tunnels=%d", ErrNonPositiveBound, o.MaxTunnels)
	}
	return nil
}

func validateListeners(listeners []Listener) ([]string, error) {
	if len(listeners) == 0 {
		return nil, ErrNoListeners
	}
	seenApps := make(map[string]struct{}, len(listeners))
	seenPorts := make(map[int]struct{}, len(listeners))
	apps := make([]string, 0, len(listeners))
	for _, l := range listeners {
		if !isValidAppName(l.App) {
			return nil, fmt.Errorf("%w: %q", ErrBadAppName, l.App)
		}
		if l.Port < minPort || l.Port > maxPort {
			return nil, fmt.Errorf("%w: listener %s port %d", ErrBadPort, l.App, l.Port)
		}
		if _, dup := seenApps[l.App]; dup {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateApp, l.App)
		}
		if _, dup := seenPorts[l.Port]; dup {
			return nil, fmt.Errorf("%w: %d", ErrDuplicatePort, l.Port)
		}
		seenApps[l.App] = struct{}{}
		seenPorts[l.Port] = struct{}{}
		apps = append(apps, l.App)
	}
	return apps, nil
}

func isValidAppName(name string) bool {
	if name == "" || len(name) > maxDNSLabelLen {
		return false
	}
	for i := range len(name) {
		if !isAppNameByte(name[i]) {
			return false
		}
	}
	return true
}

func isAppNameByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '-' || ch == '_'
}

func compileHostLists(o Options) (HostMatcher, HostMatcher, error) {
	allowMatcher, err := NewHostMatcher(o.AllowHosts)
	if err != nil {
		return HostMatcher{}, HostMatcher{}, fmt.Errorf("allow_hosts: %w", err)
	}
	if o.Mode == ModeEnforce && len(allowMatcher.Patterns()) == 0 {
		return HostMatcher{}, HostMatcher{}, ErrEmptyAllowHosts
	}
	refuseMatcher, err := NewHostMatcher(o.RefuseHosts)
	if err != nil {
		return HostMatcher{}, HostMatcher{}, fmt.Errorf("refuse_hosts: %w", err)
	}
	return allowMatcher, refuseMatcher, nil
}

func compilePorts(ports []int) (map[uint16]struct{}, []uint16, error) {
	if len(ports) == 0 {
		return nil, nil, fmt.Errorf("%w: ports must not be empty", ErrBadPort)
	}
	set := make(map[uint16]struct{}, len(ports))
	list := make([]uint16, 0, len(ports))
	for _, p := range ports {
		if p < minPort || p > maxPort {
			return nil, nil, fmt.Errorf("%w: target port %d", ErrBadPort, p)
		}
		u := uint16(p)
		if _, dup := set[u]; !dup {
			set[u] = struct{}{}
			list = append(list, u)
		}
	}
	return set, list, nil
}

func compileDeniedPrefixes(rawPrefixes []string) ([]netip.Prefix, []netip.Prefix, error) {
	v4 := make([]netip.Prefix, 0, len(rawPrefixes))
	v6 := make([]netip.Prefix, 0, len(rawPrefixes))
	for _, raw := range rawPrefixes {
		p, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return nil, nil, fmt.Errorf("%w %q: %w", ErrBadCIDR, raw, err)
		}
		if p.Addr().Is4In6() {
			if p.Bits() < ipv4MappedPrefixBits {
				return nil, nil, fmt.Errorf(
					"%w %q: IPv4-mapped IPv6 prefix length must be %d..128 (or use IPv4 CIDR notation)",
					ErrBadCIDR, raw, ipv4MappedPrefixBits,
				)
			}
			p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-ipv4MappedPrefixBits)
		}
		masked := p.Masked()
		if masked.Addr().Is4() {
			v4 = append(v4, masked)
		} else {
			v6 = append(v6, masked)
		}
	}
	return v4, v6, nil
}

// LoadChecked verifies that any explicit ConfigFileEnv file recorded in check remained unchanged
// across configuration loading, then compiles *Compiled from cfg.
func LoadChecked(cfg *gconfig.Config, check *ExplicitFileCheck) (*Compiled, error) {
	if err := check.Verify(); err != nil {
		return nil, err
	}
	return Load(cfg)
}

// Module provides *ExplicitFileCheck and *Compiled to the fx graph.
var Module = fx.Module("metadata_proxy.config", fx.Provide(NewExplicitFileCheck, LoadChecked))
