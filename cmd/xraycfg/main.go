package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type options struct {
	APITag        string
	APIListen     string
	APIPort       int
	MetricsListen string
	AccessLog     string
	ErrorLog      string
	LogLevel      string
}

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "version", "--version", "-version":
		fmt.Println(version)
	case "patch":
		if err := runPatch(os.Args[2:]); err != nil {
			exitErr(err)
		}
	case "merge":
		if err := runMerge(os.Args[2:]); err != nil {
			exitErr(err)
		}
	case "validate":
		if err := runValidate(os.Args[2:]); err != nil {
			exitErr(err)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s <patch|merge|validate> [flags]\n", filepath.Base(os.Args[0]))
}

func exitErr(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func runPatch(args []string) error {
	fs, opts := newCommonFlagSet("patch")
	if err := fs.Parse(args); err != nil {
		return err
	}

	return writeJSON(os.Stdout, requiredPatch(opts))
}

func runMerge(args []string) error {
	fs, opts := newCommonFlagSet("merge")
	inPath := fs.String("in", "", "path to existing Xray config")
	outPath := fs.String("out", "", "path to write merged Xray config (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	root := map[string]any{}
	if *inPath != "" {
		data, err := os.ReadFile(*inPath)
		if err != nil {
			return fmt.Errorf("read input config: %w", err)
		}
		if err := json.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("parse input config: %w", err)
		}
	}

	mergeRequired(root, opts)

	if *outPath == "" {
		return writeJSON(os.Stdout, root)
	}

	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal merged config: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(*outPath, data, 0o644); err != nil {
		return fmt.Errorf("write merged config: %w", err)
	}
	return nil
}

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	filePath := fs.String("file", "", "path to Xray config")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *filePath == "" {
		return errors.New("validate requires --file")
	}

	data, err := os.ReadFile(*filePath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	root := map[string]any{}
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	var issues []string
	var warnings []string

	if _, ok := root["stats"].(map[string]any); !ok {
		issues = append(issues, "missing top-level stats object")
	}

	api := asMap(root["api"])
	if api == nil {
		issues = append(issues, "missing api block")
	} else {
		if strings.TrimSpace(asString(api["tag"])) == "" {
			issues = append(issues, "api.tag is empty")
		}
		if !stringSliceContains(asStringSlice(api["services"]), "StatsService") {
			issues = append(issues, "api.services does not include StatsService")
		}
	}

	apiTag := "api"
	if api != nil && strings.TrimSpace(asString(api["tag"])) != "" {
		apiTag = asString(api["tag"])
	}

	if !hasAPIInbound(root, apiTag) {
		issues = append(issues, "missing localhost dokodemo-door inbound for the Xray API")
	}

	if !hasAPIRoutingRule(root, apiTag) {
		issues = append(issues, "missing routing rule from inboundTag=api to outboundTag=api")
	}

	policy := asMap(root["policy"])
	if policy == nil {
		issues = append(issues, "missing policy block")
	} else {
		level0 := asMap(asMap(policy["levels"])["0"])
		if !asBool(level0["statsUserUplink"]) {
			issues = append(issues, "policy.levels.0.statsUserUplink must be true")
		}
		if !asBool(level0["statsUserDownlink"]) {
			issues = append(issues, "policy.levels.0.statsUserDownlink must be true")
		}

		system := asMap(policy["system"])
		requiredSystem := []string{
			"statsInboundUplink",
			"statsInboundDownlink",
			"statsOutboundUplink",
			"statsOutboundDownlink",
		}
		for _, key := range requiredSystem {
			if !asBool(system[key]) {
				issues = append(issues, fmt.Sprintf("policy.system.%s must be true", key))
			}
		}
	}

	metrics := asMap(root["metrics"])
	if metrics == nil || strings.TrimSpace(asString(metrics["listen"])) == "" {
		issues = append(issues, "metrics.listen is missing")
	} else if !isLoopbackAddress(asString(metrics["listen"])) {
		warnings = append(warnings, "metrics.listen is not bound to localhost")
	}

	logBlock := asMap(root["log"])
	if logBlock == nil {
		issues = append(issues, "missing log block")
	} else {
		if strings.TrimSpace(asString(logBlock["access"])) == "" {
			issues = append(issues, "log.access is empty")
		}
		if strings.TrimSpace(asString(logBlock["error"])) == "" {
			issues = append(issues, "log.error is empty")
		}
	}

	if len(warnings) > 0 {
		for _, warning := range warnings {
			fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
		}
	}

	if len(issues) > 0 {
		for _, issue := range issues {
			fmt.Fprintf(os.Stderr, "missing: %s\n", issue)
		}
		return errors.New("xray config validation failed")
	}

	fmt.Printf("config %s contains the required monitoring sections\n", *filePath)
	return nil
}

func newCommonFlagSet(name string) (*flag.FlagSet, options) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	opts := options{
		APITag:        "api",
		APIListen:     "127.0.0.1",
		APIPort:       10085,
		MetricsListen: "127.0.0.1:11111",
		AccessLog:     "/var/log/xray/access.log",
		ErrorLog:      "/var/log/xray/error.log",
		LogLevel:      "warning",
	}
	fs.StringVar(&opts.APITag, "api-tag", opts.APITag, "tag for the internal Xray API")
	fs.StringVar(&opts.APIListen, "api-listen", opts.APIListen, "listen address for the Xray API inbound")
	fs.IntVar(&opts.APIPort, "api-port", opts.APIPort, "port for the Xray API inbound")
	fs.StringVar(&opts.MetricsListen, "metrics-listen", opts.MetricsListen, "listen address for the Xray metrics endpoint")
	fs.StringVar(&opts.AccessLog, "access-log", opts.AccessLog, "path to access.log")
	fs.StringVar(&opts.ErrorLog, "error-log", opts.ErrorLog, "path to error.log")
	fs.StringVar(&opts.LogLevel, "log-level", opts.LogLevel, "Xray log level")
	return fs, opts
}

func requiredPatch(opts options) map[string]any {
	return map[string]any{
		"stats": map[string]any{},
		"api": map[string]any{
			"tag": opts.APITag,
			"services": []any{
				"StatsService",
			},
		},
		"inbounds": []any{
			map[string]any{
				"listen":   opts.APIListen,
				"port":     opts.APIPort,
				"protocol": "dokodemo-door",
				"settings": map[string]any{
					"address": "127.0.0.1",
				},
				"tag": opts.APITag,
			},
		},
		"routing": map[string]any{
			"rules": []any{
				map[string]any{
					"type":        "field",
					"inboundTag":  []any{opts.APITag},
					"outboundTag": opts.APITag,
				},
			},
		},
		"policy": map[string]any{
			"levels": map[string]any{
				"0": map[string]any{
					"statsUserUplink":   true,
					"statsUserDownlink": true,
				},
			},
			"system": map[string]any{
				"statsInboundUplink":    true,
				"statsInboundDownlink":  true,
				"statsOutboundUplink":   true,
				"statsOutboundDownlink": true,
			},
		},
		"metrics": map[string]any{
			"listen": opts.MetricsListen,
		},
		"log": map[string]any{
			"access":   opts.AccessLog,
			"error":    opts.ErrorLog,
			"loglevel": opts.LogLevel,
		},
	}
}

func mergeRequired(root map[string]any, opts options) {
	root["stats"] = map[string]any{}

	api := ensureMap(root, "api")
	api["tag"] = opts.APITag
	api["services"] = mergeStringArray(api["services"], "StatsService")

	inbounds := asSlice(root["inbounds"])
	if !hasInboundTag(inbounds, opts.APITag) {
		inbounds = append(inbounds, requiredPatch(opts)["inbounds"].([]any)...)
	}
	root["inbounds"] = inbounds

	routing := ensureMap(root, "routing")
	rules := asSlice(routing["rules"])
	if !hasRoutingRule(rules, opts.APITag) {
		rules = append(rules, requiredPatch(opts)["routing"].(map[string]any)["rules"].([]any)...)
	}
	routing["rules"] = rules

	policy := ensureMap(root, "policy")
	levels := ensureNestedMap(policy, "levels")
	level0 := ensureNestedMap(levels, "0")
	level0["statsUserUplink"] = true
	level0["statsUserDownlink"] = true

	system := ensureNestedMap(policy, "system")
	system["statsInboundUplink"] = true
	system["statsInboundDownlink"] = true
	system["statsOutboundUplink"] = true
	system["statsOutboundDownlink"] = true

	metrics := ensureMap(root, "metrics")
	metrics["listen"] = opts.MetricsListen

	logBlock := ensureMap(root, "log")
	logBlock["access"] = opts.AccessLog
	logBlock["error"] = opts.ErrorLog
	if strings.TrimSpace(asString(logBlock["loglevel"])) == "" {
		logBlock["loglevel"] = opts.LogLevel
	}
}

func writeJSON(file *os.File, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = file.Write(data)
	return err
}

func ensureMap(root map[string]any, key string) map[string]any {
	if m := asMap(root[key]); m != nil {
		return m
	}
	m := map[string]any{}
	root[key] = m
	return m
}

func ensureNestedMap(root map[string]any, key string) map[string]any {
	return ensureMap(root, key)
}

func asMap(value any) map[string]any {
	if value == nil {
		return nil
	}
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return nil
}

func asSlice(value any) []any {
	if value == nil {
		return []any{}
	}
	if items, ok := value.([]any); ok {
		return items
	}
	return []any{}
}

func asString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func asBool(value any) bool {
	if b, ok := value.(bool); ok {
		return b
	}
	return false
}

func asStringSlice(value any) []string {
	items := asSlice(value)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if s := asString(item); s != "" {
			result = append(result, s)
		}
	}
	return result
}

func mergeStringArray(value any, required ...string) []any {
	current := asStringSlice(value)
	for _, item := range required {
		if !slices.Contains(current, item) {
			current = append(current, item)
		}
	}
	out := make([]any, 0, len(current))
	for _, item := range current {
		out = append(out, item)
	}
	return out
}

func hasInboundTag(inbounds []any, tag string) bool {
	for _, raw := range inbounds {
		inbound := asMap(raw)
		if inbound == nil {
			continue
		}
		if asString(inbound["tag"]) == tag {
			return true
		}
	}
	return false
}

func hasAPIInbound(root map[string]any, tag string) bool {
	for _, raw := range asSlice(root["inbounds"]) {
		inbound := asMap(raw)
		if inbound == nil || asString(inbound["tag"]) != tag {
			continue
		}
		if asString(inbound["protocol"]) != "dokodemo-door" {
			continue
		}
		if !isLoopbackAddress(asString(inbound["listen"])) {
			continue
		}
		if !isPositiveNumber(inbound["port"]) {
			continue
		}
		return true
	}
	return false
}

func hasRoutingRule(rules []any, tag string) bool {
	for _, raw := range rules {
		rule := asMap(raw)
		if rule == nil {
			continue
		}
		if asString(rule["outboundTag"]) != tag {
			continue
		}
		if !stringSliceContains(asStringSlice(rule["inboundTag"]), tag) {
			continue
		}
		return true
	}
	return false
}

func hasAPIRoutingRule(root map[string]any, tag string) bool {
	routing := asMap(root["routing"])
	if routing == nil {
		return false
	}
	return hasRoutingRule(asSlice(routing["rules"]), tag)
}

func stringSliceContains(items []string, needle string) bool {
	return slices.Contains(items, needle)
}

func isLoopbackAddress(value string) bool {
	return strings.HasPrefix(value, "127.0.0.1") || strings.HasPrefix(strings.ToLower(value), "localhost")
}

func isPositiveNumber(value any) bool {
	switch n := value.(type) {
	case float64:
		return n > 0
	case int:
		return n > 0
	default:
		return false
	}
}
